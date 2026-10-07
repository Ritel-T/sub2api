"""Regression tests for account-wide Astra quality, with no provider requests."""
import copy
from datetime import datetime, timedelta, timezone
import fcntl
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).parent))
import oauth_candy_cycle as candy
import oauth_gateway_borrow as m


class AccountQualityTests(unittest.TestCase):
    def account(self, account_id=1):
        return {'id': account_id, 'platform': 'openai', 'type': 'oauth', 'status': 'active',
                'schedulable': True, 'proxy_id': 7,
                'credentials': {'access_token': 'test-token', 'chatgpt_account_id': 'test-account',
                    'model_mapping': {model: model for model in m.ROUTE_MODELS}},
                'extra': {'unrelated': 42}, 'group_ids': [2, 15, 16],
                'account_groups': [{'group_id': 2, 'allowed_models': ['gpt-6-luna']}]}

    def sample(self, text='21', **fields):
        return {'completed': True, 'http_status': '200', 'curl_exit': 0,
                'response_model': 'gpt-6-astra', 'text': text, **fields}

    def result(self, account, answers=None, initial=True, on_sample=None):
        answers = ['21'] * 4 if answers is None else answers
        probe = mock.Mock(side_effect=[self.sample(a) if isinstance(a, str) else a for a in answers])
        result = m.assess_model(account, 'gpt-6-astra', initial=initial, reasoning='medium',
            expected_answer='21', run_id='test-run', probe_fn=probe, on_sample=on_sample)
        self.assertEqual(probe.call_count, len(answers))
        return result

    def classified_account(self, account=None, state='healthy', new_mode=True):
        account = account or self.account()
        r = self.result(account, ['21' if state == 'healthy' else '29'] * 4)
        record = m.evidence(r) | {
            'latest_probe_at': r['checked_at'],
            'latest_probe_credential_sha256': m.identity(account)['credential_sha256'],
            'latest_probe_proxy_id': account['proxy_id']}
        if new_mode:
            account['extra']['openai_gateway_borrow_quality_mode'] = m.QUALITY_MODE
            account['extra']['quality_candy'] = record
        else:
            account['extra']['quality_candy_models'] = {'gpt-6-astra': record}
        return account

    def after_apply(self, account, result):
        after = copy.deepcopy(account)
        after['extra'].update(openai_gateway_borrow_models=m.desired_borrow(account, [result]),
            openai_gateway_borrow_quality_mode=m.QUALITY_MODE,
            openai_gateway_borrow_quality_pending=False)
        return after

    def run_fake(self, account=None, *, pending_only=False, initial=False, answers=None,
                 apply_error=False, routing_error=False, on_probe=None):
        account = account or self.account()
        answers = ['21'] * 4 if answers is None else answers
        samples = iter(self.sample(answer) if isinstance(answer, str) else answer for answer in answers)
        calls = []
        def probe(*args):
            calls.append(('probe', args[1]))
            if on_probe:
                on_probe(account)
            return next(samples)
        def apply_account(a, results, *args):
            calls.append(('apply', results[0]['total']))
            if apply_error:
                raise RuntimeError('conflict')
            return {'id': a['id'], 'applied': True}
        with tempfile.TemporaryDirectory() as folder:
            state_dir = Path(folder)
            (state_dir / 'admin-key').write_text('test-key')
            (state_dir / 'last-run.json').write_text('{"old":1}')
            (state_dir / 'candy-history.json').write_text('[{"old":1}]')
            with mock.patch.object(candy, 'inventory', return_value=[account]), \
                 mock.patch.object(candy, 'probe', side_effect=probe), \
                 mock.patch.object(m, 'apply_account', side_effect=apply_account), \
                 mock.patch.object(candy, 'persist_quota_one', return_value={'id': account['id'], 'updated': False}) as quota, \
                 mock.patch.object(candy, 'recover_one', return_value={'id': account['id'], 'recovered': False}) as recovery, \
                 mock.patch.object(m, 'update_routing', return_value={'updated': True},
                    side_effect=RuntimeError('routing failed') if routing_error else None) as routing:
                if apply_error or routing_error:
                    with self.assertRaisesRegex(RuntimeError, 'need reconciliation'):
                        m.run_borrow(state_dir=folder, apply=True, initial=initial, pending_only=pending_only, workers=1)
                else:
                    m.run_borrow(state_dir=folder, apply=True, initial=initial, pending_only=pending_only, workers=1)
            result_file = 'pending-last-run.json' if pending_only else 'last-run.json'
            summary = json.loads((state_dir / result_file).read_text())
            return summary, calls, quota.call_count, recovery.call_count, routing.call_args, \
                json.loads((state_dir / 'last-run.json').read_text()), \
                json.loads((state_dir / 'candy-history.json').read_text())

    def test_four_sample_threshold_and_all_three_required_names(self):
        a = self.account()
        for answers, state, borrowed in [(['21', '21', '29', '21'], 'healthy', []),
                (['21', '29', '29', '21'], 'degraded', list(m.BORROW_MODELS)),
                (['22'] * 4, 'degraded', list(m.BORROW_MODELS))]:
            r = self.result(a, answers)
            self.assertEqual(r['state'], state)
            self.assertEqual(m.desired_borrow(a, [r]), borrowed)

    def test_no_independent_sol_record_or_probe(self):
        a = self.classified_account()
        a['extra']['quality_candy_models'] = {'gpt-6.1-sol': {'state': 'degraded'}}
        self.assertIsNone(m.previous_record(a, 'gpt-6.1-sol'))
        fn = mock.Mock(side_effect=AssertionError('Sol must not be probed'))
        r = m.assess_model(a, 'gpt-6.1-sol', initial=True, reasoning='medium',
            expected_answer='21', run_id='test', probe_fn=fn)
        self.assertEqual(r['skipped'], 'unsupported_model')
        fn.assert_not_called()

    def test_new_mode_account_record_wins_over_stale_per_model_history(self):
        a = self.classified_account()
        a['extra']['quality_candy_models'] = {'gpt-6-astra': {'state': 'degraded'}}
        self.assertIs(m.previous_record(a, 'gpt-6-astra'), a['extra']['quality_candy'])
        self.assertEqual(self.result(a, ['21'], initial=False)['state'], 'healthy')

    def test_stable_one_then_changed_four_with_no_early_action(self):
        a = self.classified_account()
        a['extra']['openai_gateway_borrow_models'] = []
        self.assertEqual(self.result(a, ['21'], initial=False)['total'], 1)
        seen = []
        r = self.result(a, ['29'] * 4, initial=False,
            on_sample=lambda result: seen.append((len(result['samples']), list(a['extra']['openai_gateway_borrow_models']))))
        self.assertEqual(seen, [(1, []), (2, []), (3, []), (4, [])])
        self.assertEqual(m.desired_borrow(a, [r]), list(m.BORROW_MODELS))

    def test_changed_degraded_account_recovers_both_after_four(self):
        a = self.classified_account(state='degraded')
        a['extra']['openai_gateway_borrow_models'] = list(m.BORROW_MODELS)
        r = self.result(a, ['21', '21', '29', '21'], initial=False)
        self.assertEqual((r['total'], r['state']), (4, 'healthy'))
        self.assertEqual(m.desired_borrow(a, [r]), [])

    def test_incomplete_auth_quota_or_model_response_preserves_policy(self):
        a = self.classified_account(state='degraded')
        a['extra']['openai_gateway_borrow_models'] = list(m.BORROW_MODELS)
        for sample in [self.sample(http_status='401', error='auth'),
                       self.sample(http_status='429', error='rate'),
                       self.sample(completed=False),
                       self.sample(response_model='gpt-6.1-sol')]:
            r = self.result(a, [sample], initial=False)
            self.assertEqual(r['state'], 'inconclusive')
            self.assertEqual(m.desired_borrow(a, [r]), list(m.BORROW_MODELS))
        r = self.result(a, ['21', '21', self.sample(completed=False), '21'], initial=False)
        self.assertEqual(r['state'], 'inconclusive')
        self.assertEqual(m.desired_borrow(a, [r]), list(m.BORROW_MODELS))

    def test_changed_identity_proxy_or_stale_evidence_bootstraps(self):
        for field in ('token', 'proxy', 'stale'):
            a = self.classified_account()
            if field == 'token': a['credentials']['access_token'] = 'changed'
            if field == 'proxy': a['proxy_id'] = 9
            if field == 'stale': a['extra']['quality_candy']['latest_probe_at'] = (
                datetime.now(timezone.utc) - timedelta(hours=25)).isoformat()
            r = self.result(a, initial=False)
            self.assertEqual((r['total'], r['sampling_mode']), (4, 'bootstrap'))

    def test_pending_defaults_must_finish_before_initial_probes(self):
        a = self.account()
        a['extra']['openai_gateway_borrow_quality_pending'] = True
        r = self.result(a, [])
        self.assertEqual(r['skipped'], 'initial_defaults_pending')
        a['extra']['openai_gateway_borrow_initial_ready'] = True
        self.assertEqual(self.result(a)['total'], 4)

    def test_migration_reuses_actual_classification_without_new_evidence(self):
        a = self.classified_account(new_mode=False)
        prior = copy.deepcopy(m.previous_record(a, 'gpt-6-astra'))
        r = m.reusable_classification(a, 'medium', '21', 'run')
        self.assertEqual((r['total'], r['samples'], r['action']), (0, [], 'reuse_classification'))
        after = self.after_apply(a, r)
        api = mock.Mock(side_effect=[a, {'id': 1, 'applied': True}, after])
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(candy.legacy, 'admin', api):
            self.assertTrue(m.apply_account(a, [r], 'test-key', Path(folder))['applied'])
        payload = api.call_args_list[1].args[3]
        self.assertEqual(payload['model_results'], {})
        self.assertEqual(payload['borrow_models'], [])
        self.assertEqual(m.previous_record(a, 'gpt-6-astra'), prior)

    def test_migration_rejects_stale_pending_invalid_or_already_converted(self):
        for field in ('stale', 'pending', 'wrong_proxy', 'incomplete', 'new_mode'):
            a = self.classified_account(new_mode=False)
            prior = m.previous_record(a, 'gpt-6-astra')
            if field == 'stale': prior['latest_probe_at'] = (datetime.now(timezone.utc) - timedelta(hours=25)).isoformat()
            if field == 'pending': a['extra']['openai_gateway_borrow_quality_pending'] = True
            if field == 'wrong_proxy': prior['latest_probe_proxy_id'] = 99
            if field == 'incomplete': prior['total'] = 1
            if field == 'new_mode': a['extra']['openai_gateway_borrow_quality_mode'] = m.QUALITY_MODE
            self.assertIsNone(m.reusable_classification(a, 'medium', '21', 'test'))

    def test_migration_old_classification_recent_real_single_refresh_is_valid(self):
        a = self.classified_account(new_mode=False)
        m.previous_record(a, 'gpt-6-astra')['checked_at'] = (datetime.now(timezone.utc) - timedelta(days=7)).isoformat()
        self.assertIsNotNone(m.reusable_classification(a, 'medium', '21', 'test'))

    def test_narrow_policy_payload_only_astra_and_independent_limits_unchanged(self):
        a = self.account()
        a['extra']['model_rate_limits'] = {'gpt-6.1-sol': {'reset_at': '2099-01-01T00:00:00Z'}}
        r = self.result(a, ['29'] * 4)
        after = self.after_apply(a, r)
        api = mock.Mock(side_effect=[a, {'id': 1, 'applied': True}, after])
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(candy.legacy, 'admin', api):
            change = m.apply_account(a, [r], 'test-key', Path(folder))
        self.assertTrue(change['applied'])
        payload = api.call_args_list[1].args[3]
        self.assertEqual(payload['policy_mode'], m.QUALITY_MODE)
        self.assertEqual(set(payload['model_results']), {'gpt-6-astra'})
        self.assertEqual(payload['borrow_models'], list(m.BORROW_MODELS))
        self.assertEqual(len(payload['expected_policy']), 10)
        self.assertEqual(payload['expected_policy'], m.owned_policy(a))
        self.assertEqual(after['extra']['model_rate_limits'], a['extra']['model_rate_limits'])
        self.assertFalse(any(call.args[0] == 'PUT' for call in api.call_args_list))
        for field in ('extra', 'credentials', 'group_ids', 'schedulable', 'model_rate_limits'):
            self.assertNotIn(field, payload)

    def test_incomplete_does_not_post_even_legacy_bps(self):
        a = self.classified_account(new_mode=False, state='degraded')
        a['extra'].update(openai_excel_bps=True, openai_excel_bps_required_group_ids=[16],
            openai_excel_bps_required_models=list(m.BORROW_MODELS))
        r = self.result(a, [self.sample(http_status='429')], initial=False)
        api = mock.Mock(return_value=a)
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(candy.legacy, 'admin', api):
            change = m.apply_account(a, [r], 'test-key', Path(folder))
        self.assertEqual(change['skipped'], 'incomplete_probe_set')
        self.assertEqual(api.call_count, 1)

    def test_policy_pause_or_concurrent_edit_never_posts(self):
        a = self.account()
        r = self.result(a)
        for edit in ('pause', 'proxy', 'mode', 'ready'):
            current = copy.deepcopy(a)
            if edit == 'pause': current['schedulable'] = False
            if edit == 'proxy': current['proxy_id'] = 8
            if edit == 'mode': current['extra']['openai_gateway_borrow_quality_mode'] = 'other'
            if edit == 'ready': current['extra']['openai_gateway_borrow_initial_ready'] = True
            api = mock.Mock(return_value=current)
            with tempfile.TemporaryDirectory() as folder, mock.patch.object(candy.legacy, 'admin', api):
                self.assertFalse(m.apply_account(a, [r], 'test-key', Path(folder))['applied'])
            self.assertEqual(api.call_count, 1)

    def test_readback_checks_mode_pending_and_acl(self):
        a = self.account(); r = self.result(a)
        for field in ('mode', 'pending', 'acl'):
            after = self.after_apply(a, r)
            if field == 'mode': after['extra']['openai_gateway_borrow_quality_mode'] = 'old'
            if field == 'pending': after['extra']['openai_gateway_borrow_quality_pending'] = True
            if field == 'acl': after['account_groups'][0]['allowed_models'] = []
            api = mock.Mock(side_effect=[a, {'id': 1, 'applied': True}, after])
            with tempfile.TemporaryDirectory() as folder, mock.patch.object(candy.legacy, 'admin', api):
                with self.assertRaises(RuntimeError):
                    m.apply_account(a, [r], 'test-key', Path(folder))

    def test_source_uses_only_fresh_astra_without_sol_result(self):
        source = self.account(168); target = self.account(34)
        target['extra']['openai_gateway_borrow_models'] = list(m.BORROW_MODELS)
        r = self.result(source)
        current = {'cookie_pool': {'ttl_seconds': 120}, 'ws_session': {'ttl_seconds': 600}, 'unrelated': 9}
        desired = m.routing_payload(current, [source, target], [r])
        self.assertEqual(desired['cookie_pool']['source_account_ids'], [168])
        self.assertEqual(desired['cookie_pool']['target_models'], {'34': list(m.ROUTE_MODELS)})
        self.assertEqual(desired['cookie_pool']['models'], list(m.ROUTE_MODELS))
        self.assertEqual(desired['quality_mode'], m.QUALITY_MODE)
        self.assertEqual(desired['cookie_pool']['quality_mode'], m.QUALITY_MODE)
        self.assertEqual(desired['unrelated'], 9)
        self.assertEqual(desired['cookie_pool']['ttl_seconds'], 120)
        self.assertFalse(desired['account_scheduling'])
        self.assertFalse(desired['ws_session']['enabled'])

    def test_source_rejects_identity_proxy_stale_incomplete_cooldown(self):
        for field in ('identity', 'proxy', 'stale', 'incomplete', 'cooldown'):
            a = self.account(168); r = self.result(a)
            if field == 'identity': a['credentials']['access_token'] = 'other'
            if field == 'proxy': a['proxy_id'] = 8
            if field == 'stale': r['checked_at'] = (datetime.now(timezone.utc) - timedelta(minutes=11)).isoformat()
            if field == 'incomplete': r['samples'][0]['completed'] = False
            if field == 'cooldown': a['overload_until'] = (datetime.now(timezone.utc) + timedelta(minutes=1)).isoformat()
            self.assertEqual(m.routing_payload({}, [a], [r])['cookie_pool']['source_account_ids'], [])

    def test_source_members_stably_ordered_after_bounded_selection(self):
        accounts = [self.account(i) for i in range(101, 115)]
        results = [self.result(a) for a in accounts]
        desired = m.routing_payload({}, accounts, results)
        self.assertEqual(desired['cookie_pool']['source_account_ids'], list(range(103, 115)))
        reverse = m.routing_payload({}, list(reversed(accounts)), list(reversed(results)))
        self.assertEqual(desired, reverse)

    def test_migration_retains_existing_sources_only_without_fake_probe(self):
        old = self.classified_account(self.account(168), new_mode=False)
        other = self.classified_account(self.account(169), new_mode=False)
        results = [m.reusable_classification(a, 'medium', '21', 'run') for a in (old, other)]
        desired = m.routing_payload({'cookie_pool': {'source_account_ids': [168]}}, [old, other], results)
        self.assertEqual(desired['cookie_pool']['source_account_ids'], [168])
        self.assertEqual(m.routing_payload({}, [old, other], results)['cookie_pool']['source_account_ids'], [])

    def test_subset_migration_keeps_other_registered_trusted_source(self):
        old = self.classified_account(self.account(168), new_mode=False)
        other = self.classified_account(self.account(169), new_mode=False)
        reused = m.reusable_classification(other, 'medium', '21', 'run')
        desired = m.routing_payload({'cookie_pool': {'source_account_ids': [168]}}, [old, other], [reused])
        self.assertEqual(desired['cookie_pool']['source_account_ids'], [168])

    def test_pending_merge_retains_trusted_existing_sources_adds_target(self):
        old = self.classified_account(self.account(168))
        old['extra']['quality_candy']['checked_at'] = (datetime.now(timezone.utc) - timedelta(minutes=25)).isoformat()
        old['extra']['quality_candy']['latest_probe_at'] = old['extra']['quality_candy']['checked_at']
        target = self.account(34); target['extra']['openai_gateway_borrow_models'] = list(m.BORROW_MODELS)
        current = {'cookie_pool': {'source_account_ids': [168], 'ttl_seconds': 120}}
        desired = m.routing_payload(current, [old, target], [], pending_only=True)
        self.assertEqual(desired['cookie_pool']['source_account_ids'], [168])
        self.assertEqual(desired['cookie_pool']['target_account_ids'], [34])
        old['proxy_id'] = 8
        self.assertEqual(m.routing_payload(current, [old, target], [], pending_only=True)['cookie_pool']['source_account_ids'], [])

    def test_normal_cycle_does_not_use_persisted_source_in_place_of_probe(self):
        a = self.classified_account(self.account(168))
        current = {'cookie_pool': {'source_account_ids': [168]}}
        self.assertEqual(m.routing_payload(current, [a], [])['cookie_pool']['source_account_ids'], [])

    def test_run_only_astra_four_then_apply_and_single_quota_recovery(self):
        summary, calls, quota, recovery, routing, *_ = self.run_fake()
        self.assertEqual(calls, [('probe', 'gpt-6-astra')] * 4 + [('apply', 4)])
        self.assertEqual((quota, recovery), (1, 1))
        self.assertEqual([r['model'] for r in summary['results']], ['gpt-6-astra'])
        self.assertEqual(set(m.compact(summary)['models']), {'gpt-6-astra'})
        self.assertFalse(routing.kwargs['pending_only'])

    def test_run_migration_zero_requests_and_no_recovery(self):
        a = self.classified_account(new_mode=False)
        summary, calls, quota, recovery, *_ = self.run_fake(a, answers=[])
        self.assertEqual(calls, [('apply', 0)])
        self.assertEqual((quota, recovery), (1, 0))
        self.assertEqual(m.compact(summary)['probes'], 0)

    def test_run_incomplete_still_quota_cas_without_recovery(self):
        a = self.classified_account()
        summary, calls, quota, recovery, *_ = self.run_fake(a, answers=[self.sample(http_status='429')])
        self.assertEqual((quota, recovery), (1, 0))
        self.assertEqual(summary['results'][0]['state'], 'inconclusive')

    def test_failed_policy_does_not_block_quota_observation_or_allow_recovery(self):
        summary, _, quota, recovery, *_ = self.run_fake(apply_error=True)
        self.assertEqual((quota, recovery), (1, 0))
        self.assertEqual(summary['changes'][0]['error'], 'RuntimeError')

    def test_routing_failure_preserves_completed_account_policy(self):
        summary, *_ = self.run_fake(routing_error=True)
        self.assertTrue(summary['changes'][0]['applied'])
        self.assertEqual(summary['routing']['error'], 'RuntimeError')

    def test_pending_run_does_not_overwrite_regular_collector_files(self):
        a = self.account()
        a['extra'].update(openai_gateway_borrow_quality_pending=True, openai_gateway_borrow_initial_ready=True)
        summary, calls, _, _, routing, last, history = self.run_fake(a, pending_only=True)
        self.assertEqual(last, {'old': 1}); self.assertEqual(history, [{'old': 1}])
        self.assertTrue(summary['pending_only']); self.assertTrue(routing.kwargs['pending_only'])
        self.assertEqual(calls, [('probe', 'gpt-6-astra')] * 4 + [('apply', 4)])

    def test_empty_not_ready_or_unsupported_pending_has_no_write_or_key_read(self):
        for field in ('none', 'not_ready', 'no_astra'):
            a = self.account()
            if field != 'none': a['extra']['openai_gateway_borrow_quality_pending'] = True
            if field == 'no_astra':
                a['extra']['openai_gateway_borrow_initial_ready'] = True
                a['credentials']['model_mapping'].pop('gpt-6-astra')
            with tempfile.TemporaryDirectory() as folder, \
                 mock.patch.object(candy, 'inventory', return_value=[a]), \
                 mock.patch.object(candy, 'probe') as probe, \
                 mock.patch.object(m, 'update_routing') as routing:
                output = m.run_borrow(state_dir=folder, pending_only=True, apply=True)
                self.assertEqual(output['skipped'], 'no_ready_pending_accounts')
                probe.assert_not_called(); routing.assert_not_called()
                self.assertEqual([p.name for p in Path(folder).iterdir()], ['cycle.lock'])

    def test_pending_busy_lock_is_normal_skip_without_inventory(self):
        with tempfile.TemporaryDirectory() as folder:
            with Path(folder, 'cycle.lock').open('a') as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with mock.patch.object(candy, 'inventory') as inventory:
                    output = m.run_borrow(state_dir=folder, pending_only=True)
                self.assertEqual(output['skipped'], 'cycle_busy')
                inventory.assert_not_called()
                with self.assertRaises(BlockingIOError):
                    m.run_borrow(state_dir=folder)

    def test_settings_cas_and_readback_no_partial_routing(self):
        a = self.account()
        api = mock.Mock(side_effect=[{'other': 1}, {'items': []}, {'other': 2}])
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(candy.legacy, 'admin', api):
            result = m.update_routing([a], [], 'test-key', Path(folder))
        self.assertEqual(result['skipped'], 'concurrent_routing_change')
        self.assertFalse(any(call.args[0] == 'PUT' for call in api.call_args_list))
        desired = m.routing_payload({}, [a], [])
        api = mock.Mock(side_effect=[desired, {'items': []}, desired])
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(candy.legacy, 'admin', api):
            self.assertEqual(m.update_routing([a], [], 'test-key', Path(folder))['skipped'], 'unchanged')
        self.assertFalse(any(call.args[0] == 'PUT' for call in api.call_args_list))

    def test_settings_actual_write_independently_read_back(self):
        a = self.account(); r = self.result(a)
        current = {'revision': 'old', 'cookie_pool': {'ttl_seconds': 120}}
        desired = m.routing_payload(current, [a], [r]); desired['revision'] = 'new'
        api = mock.Mock(side_effect=[current, {'items': []}, current, desired, desired])
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(candy.legacy, 'admin', api):
            result = m.update_routing([a], [r], 'test-key', Path(folder))
        self.assertEqual(result['sources'], [1])
        self.assertEqual(api.call_args_list[-1].args[0], 'GET')
        self.assertEqual(api.call_args_list[-2].args[3]['quality_mode'], m.QUALITY_MODE)

    def test_expected_prompt_settings_cannot_silently_change(self):
        for kwargs in ({'reasoning': 'high'}, {'expected_answer': '29'}):
            with self.assertRaisesRegex(ValueError, 'Astra medium'):
                m.run_borrow(state_dir='/unused', **kwargs)
        with self.assertRaisesRegex(ValueError, 'requires borrow'):
            candy.run(route_mode='bps', pending_only=True)

    def test_isolated_cli_and_borrow_module_have_single_detection_model(self):
        path = Path(candy.__file__).resolve()
        completed = subprocess.run([sys.executable, '-I', str(path), '--help'], capture_output=True, text=True)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn('--pending-only', completed.stdout)
        code = """import importlib.util,sys,tempfile,pathlib
spec=importlib.util.spec_from_file_location('isolated_candy',sys.argv[1])
candy=importlib.util.module_from_spec(spec);sys.modules[spec.name]=candy;spec.loader.exec_module(candy)
with tempfile.TemporaryDirectory() as folder:
 pathlib.Path(folder,'admin-key').write_text('test-key')
 candy.inventory=lambda:[{'id':1,'credentials':{},'extra':{},'proxy_id':None}]
 def forbid(*args,**kwargs): raise AssertionError('unexpected provider request')
 candy.probe=forbid
 summary=candy.run(state_dir=folder,route_mode='borrow',workers=1)
 assert sys.modules['oauth_gateway_borrow'].candy is candy
 assert len(summary['results'])==1 and summary['results'][0]['total']==0
print('isolated_ok')
"""
        completed = subprocess.run([sys.executable, '-I', '-c', code, str(path)], capture_output=True, text=True)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn('isolated_ok', completed.stdout)


if __name__ == '__main__':
    unittest.main()
