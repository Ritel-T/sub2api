import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('candy', Path(__file__).with_name('oauth_candy_cycle.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class CandyCycleTests(unittest.TestCase):
    groups = {'good': 15, 'bad': 16}

    def account(self, state='healthy'):
        return {'id': 88, 'platform': 'openai', 'type': 'oauth', 'schedulable': False,
                'credentials': {'access_token': 'secret', 'plan_type': 'plus',
                                'model_mapping': {'gpt-6-luna': 'gpt-6-luna'}},
                'extra': {'unrelated': True, 'quality_model_results': {'gpt-6-astra': 'degraded'},
                          'quality_candy': {'version': 1, 'algorithm': m.ALGORITHM,
                                            'prompt_sha256': m.PROMPT_SHA256, 'model': m.DEFAULT_MODEL,
                                            'reasoning_effort': 'medium', 'expected_answer': '21',
                                            'total': 4, 'correct': 4 if state == 'healthy' else 0,
                                            'state': state}},
                'group_ids': [2, 15] + ([16] if state == 'degraded' else [])}

    def sample(self, answer='21', **overrides):
        return {'completed': True, 'http_status': '200', 'curl_exit': 0,
                'response_model': m.DEFAULT_MODEL, 'text': answer, **overrides}

    def assess(self, answers, *, state='healthy', initial=True):
        samples = [self.sample(a) if isinstance(a, str) else a for a in answers]
        probe = mock.Mock(side_effect=samples)
        result = m.assess_account(self.account(state), self.groups, initial=initial,
            model=m.DEFAULT_MODEL, reasoning='medium', expected_answer='21', run_id='test', probe_fn=probe)
        self.assertEqual(len(answers), probe.call_count)
        return result

    def test_four_samples_majority_threshold(self):
        for answers, state in [(['21'] * 4, 'healthy'), (['21'] * 3 + ['29'], 'healthy'),
                               (['21'] * 2 + ['29'] * 2, 'degraded'), (['29'] * 4, 'degraded')]:
            with self.subTest(answers=answers):
                result = self.assess(answers)
                self.assertEqual(state, result['state'])
                self.assertEqual('reconcile', result['action'])
                self.assertEqual(4, result['total'])

    def test_initial_error_still_collects_four_without_classifying(self):
        result = self.assess([self.sample('21', error='http_429:usage_limit_reached'), '21', '21', '21'])
        self.assertEqual(('inconclusive', 'preserve', 4), (result['state'], result['action'], result['total']))
        self.assertEqual({}, m.reconcile(self.account(), result, self.groups, {}))

    def test_matching_periodic_sample_keeps_everything_unchanged(self):
        for state, answer in [('healthy', '21'), ('degraded', '29')]:
            result = self.assess([answer], state=state, initial=False)
            self.assertEqual('unchanged', result['action'])
            self.assertEqual({}, m.reconcile(self.account(state), result, self.groups, {}))

    def test_mismatch_runs_three_more_and_uses_all_four(self):
        result = self.assess(['29', '21', '21', '21'], initial=False)
        self.assertEqual(('healthy', 3, 4), (result['state'], result['correct'], result['total']))
        result = self.assess(['21', '29', '29', '29'], state='degraded', initial=False)
        self.assertEqual(('degraded', 1, 4), (result['state'], result['correct'], result['total']))

    def test_periodic_incomplete_first_sample_stops_without_counting_wrong(self):
        result = self.assess([self.sample('21', completed=False)], initial=False)
        self.assertEqual(('inconclusive', 'preserve', 0, 1),
                         (result['state'], result['action'], result['correct'], result['total']))

    def test_retest_error_preserves_existing_classification(self):
        result = self.assess(['29', '21', self.sample('21', response_model='other'), '21'], initial=False)
        self.assertEqual('inconclusive', result['state'])

    def test_unclassified_account_retries_full_four_next_cycle(self):
        account = self.account('degraded')
        account['extra'].pop('quality_candy')
        probe = mock.Mock(side_effect=[self.sample('29')] * 4)
        result = m.assess_account(account, self.groups, initial=False, model=m.DEFAULT_MODEL,
            reasoning='medium', expected_answer='21', run_id='test', probe_fn=probe)
        self.assertEqual(('bootstrap', 4, 'degraded'),
                         (result['sampling_mode'], result['total'], result['state']))
        self.assertEqual(4, probe.call_count)

    def test_changed_prompt_grading_or_model_requires_new_four_sample_baseline(self):
        self.assertTrue(m.has_candy_classification(self.account(), m.DEFAULT_MODEL, 'medium', '21'))
        self.assertFalse(m.has_candy_classification(self.account(), m.DEFAULT_MODEL, 'medium', '29'))
        self.assertFalse(m.has_candy_classification(self.account(), 'gpt-6-sol', 'medium', '21'))
        self.assertFalse(m.has_candy_classification(self.account(), m.DEFAULT_MODEL, 'high', '21'))

    def test_strict_reference_grading_and_transport_validation(self):
        for answer, state in [('21', 'healthy'), (' 21\n', 'healthy'), ('29', 'degraded'),
                              ('答案是21', 'degraded'), ('', 'inconclusive')]:
            self.assertEqual(state, m.sample_class(self.sample(answer), m.DEFAULT_MODEL, '21'))
        for override in [{'completed': False}, {'http_status': '429'}, {'curl_exit': 28},
                         {'response_model': 'gpt-6-luna'}, {'error': 'http_401:invalid_token'}]:
            self.assertEqual('inconclusive', m.sample_class(self.sample(**override), m.DEFAULT_MODEL, '21'))

    def test_parse_requires_completed_model_and_http_success(self):
        event = {'type': 'response.completed', 'response': {'model': m.DEFAULT_MODEL, 'status': 'completed',
                  'output': [{'content': [{'type': 'output_text', 'text': '21'}]}]}}
        payload = 'data: ' + json.dumps(event)
        self.assertEqual('healthy', m.parse_response(payload, '200', 0, m.DEFAULT_MODEL, '21')['class'])
        self.assertEqual('inconclusive', m.parse_response(payload, '401', 0, m.DEFAULT_MODEL, '21')['class'])
        self.assertEqual('inconclusive', m.parse_response(payload, '200', 28, m.DEFAULT_MODEL, '21')['class'])
        self.assertEqual('inconclusive', m.parse_response(payload + '\ndata: {"type":"error"}',
                                                       '200', 0, m.DEFAULT_MODEL, '21')['class'])

    def test_degraded_policy_preserves_other_groups_and_scheduling(self):
        account = self.account()
        before = copy.deepcopy(account)
        result = self.assess(['29'] * 4)
        patch = m.reconcile(account, result, self.groups, {'models': m.BPS_MODELS})
        self.assertEqual(account, before)
        self.assertEqual([2, 15, 16], patch['group_ids'])
        extra = patch['extra']
        self.assertEqual([16], extra['openai_excel_bps_required_group_ids'])
        self.assertEqual(m.BPS_MODELS, extra['openai_excel_bps_required_models'])
        self.assertTrue(extra['openai_excel_bps'])
        self.assertFalse(extra['openai_excel_bps_auto_disable_on_403'])
        self.assertFalse(extra['openai_excel_bps_auto_move_on_403'])
        self.assertTrue(extra['unrelated'])
        self.assertNotIn('quality_model_results', extra)
        self.assertNotIn('schedulable', patch)
        self.assertEqual('21', extra['quality_candy']['expected_answer'])
        self.assertEqual({'2': ['gpt-6-luna'], '15': ['gpt-6-luna']}, patch['group_allowed_models'])

    def test_healthy_disables_bps_and_removes_only_degraded_group(self):
        patch = m.reconcile(self.account('degraded'), self.assess(['21'] * 4, state='degraded'),
                            self.groups, {'models': []})
        self.assertEqual([2, 15], patch['group_ids'])
        self.assertFalse(patch['extra']['openai_excel_bps'])
        self.assertEqual([], patch['extra']['openai_excel_bps_required_group_ids'])
        self.assertEqual([], patch['extra']['openai_excel_bps_required_models'])
        self.assertNotIn('credentials', patch)

    def test_new_sol_is_protected_without_changing_astra_classification(self):
        account = self.account()
        account['credentials']['model_mapping'].update({
            'gpt-6.1-sol': 'gpt-6.1-sol', 'gpt-5.6-sol': 'gpt-5.6-sol'})
        result = self.assess(['29'] * 4)
        patch = m.reconcile(account, result, self.groups,
            {'models': ['gpt-6-sol', 'gpt-6.1-sol', 'gpt-6-astra', 'gpt-6-luna']})
        extra = patch['extra']
        self.assertIn('gpt-6.1-sol', extra['openai_excel_bps_models'])
        self.assertIn('gpt-6.1-sol', extra['openai_excel_bps_required_models'])
        self.assertEqual('gpt-6.1-sol', patch['credentials']['model_mapping']['gpt-6.1-sol'])
        for models in patch['group_allowed_models'].values():
            self.assertNotIn('gpt-6.1-sol', models)
            self.assertIn('gpt-5.6-sol', models)
            self.assertIn('gpt-6-luna', models)
        self.assertEqual('gpt-6-astra', extra['quality_candy']['model'])
        self.assertEqual(('medium', '21', 4),
            tuple(extra['quality_candy'][key] for key in ('reasoning_effort', 'expected_answer', 'total')))

    def test_healthy_clears_existing_group_whitelists(self):
        account = self.account('degraded')
        account['account_groups'] = [{'group_id': 2, 'allowed_models': ['gpt-6-luna']}]
        patch = m.reconcile(account, self.assess(['21'] * 4, state='degraded'), self.groups, {})
        self.assertEqual({}, patch['group_allowed_models'])

    def test_apply_reads_group_binding_shape_and_replay_is_idempotent(self):
        account = self.account()
        result = self.assess(['29'] * 4)
        calls = []
        def admin(method, path, key, body=None):
            calls.append(method)
            if method == 'PUT':
                account.update({k: v for k, v in body.items() if k != 'group_allowed_models'})
                if 'group_allowed_models' in body:
                    account['account_groups'] = [{'group_id': int(k), 'allowed_models': v}
                                                  for k, v in body['group_allowed_models'].items()]
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(m.legacy, 'admin', side_effect=admin):
            first = m.apply_one(result, 'key', self.groups, {'plus': {'models': m.BPS_MODELS}}, Path(folder))
            saved = (Path(folder) / 'account-88.before.json').read_text()
            second = m.apply_one(result, 'key', self.groups, {'plus': {'models': m.BPS_MODELS}}, Path(folder))
            self.assertEqual(saved, (Path(folder) / 'account-88.before.json').read_text())
        self.assertIn('group_allowed_models', first['change'])
        self.assertEqual('already_applied', second['skipped'])
        self.assertEqual(1, calls.count('PUT'))
        self.assertFalse(account['schedulable'])

    def test_apply_preserves_concurrent_policy_edit(self):
        account = self.account()
        result = self.assess(['29'] * 4)
        account['group_ids'].append(99)
        with mock.patch.object(m.legacy, 'admin', return_value=account) as admin:
            outcome = m.apply_one(result, 'key', self.groups, {}, Path('/unused'))
        self.assertEqual('concurrent_policy_change', outcome['skipped'])
        admin.assert_called_once()

    def test_apply_readback_detects_missing_group_acl(self):
        account = self.account()
        result = self.assess(['29'] * 4)
        def admin(method, path, key, body=None):
            if body:
                account.update({k: v for k, v in body.items() if k != 'group_allowed_models'})
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(m.legacy, 'admin', side_effect=admin):
            with self.assertRaisesRegex(RuntimeError, 'group_allowed_models readback mismatch'):
                m.apply_one(result, 'key', self.groups, {}, Path(folder))

    def test_healthy_removes_bad_group_before_disabling_bps(self):
        account = self.account('degraded')
        account['extra'].update(openai_excel_bps=True,
                                openai_excel_bps_required_group_ids=[16],
                                openai_excel_bps_required_models=m.BPS_MODELS)
        result = self.assess(['21'] * 4, state='degraded')
        result['baseline_policy'] = m.policy_snapshot(account)
        puts = []
        def admin(method, path, key, body=None):
            if method == 'PUT':
                puts.append(copy.deepcopy(body))
                # Any write that disables BPS must already have removed bad.
                if body.get('extra', {}).get('openai_excel_bps') is False:
                    self.assertNotIn(16, account['group_ids'])
                account.update(body)
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(m.legacy, 'admin', side_effect=admin):
            m.apply_one(result, 'key', self.groups, {}, Path(folder))
        self.assertEqual({'group_ids': [2, 15]}, puts[0])
        self.assertFalse(puts[1]['extra']['openai_excel_bps'])

    def test_healthy_failed_second_phase_can_resume_without_overwriting_backup(self):
        account = self.account('degraded')
        result = self.assess(['21'] * 4, state='degraded')
        fail_extra = True
        def admin(method, path, key, body=None):
            if method == 'PUT':
                if fail_extra and 'extra' in body:
                    raise RuntimeError('simulated second-phase failure')
                account.update(body)
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(m.legacy, 'admin', side_effect=admin):
            with self.assertRaisesRegex(RuntimeError, 'second-phase failure'):
                m.apply_one(result, 'key', self.groups, {}, Path(folder))
            self.assertNotIn(16, account['group_ids'])
            saved = (Path(folder) / 'account-88.before.json').read_text()
            fail_extra = False
            applied = m.apply_one(result, 'key', self.groups, {}, Path(folder))
            self.assertTrue(applied['change'])
            self.assertEqual(saved, (Path(folder) / 'account-88.before.json').read_text())

    def test_healthy_failed_bad_group_removal_never_disables_bps(self):
        account = self.account('degraded')
        result = self.assess(['21'] * 4, state='degraded')
        puts = []
        def admin(method, path, key, body=None):
            if method == 'PUT':
                puts.append(body)
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(m.legacy, 'admin', side_effect=admin):
            with self.assertRaisesRegex(RuntimeError, 'bad-group removal'):
                m.apply_one(result, 'key', self.groups, {}, Path(folder))
        self.assertEqual([{'group_ids': [2, 15]}], puts)

    def test_concurrent_extra_edit_between_healthy_phases_is_preserved(self):
        account = self.account('degraded')
        result = self.assess(['21'] * 4, state='degraded')
        puts = []
        def admin(method, path, key, body=None):
            if method == 'PUT':
                puts.append(body)
                account.update(body)
                account['extra']['openai_excel_bps_required_models'] = ['gpt-6-sol']
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(m.legacy, 'admin', side_effect=admin):
            with self.assertRaisesRegex(RuntimeError, 'policy changed after bad-group removal'):
                m.apply_one(result, 'key', self.groups, {}, Path(folder))
        self.assertEqual([{'group_ids': [2, 15]}], puts)
        self.assertEqual(['gpt-6-sol'], account['extra']['openai_excel_bps_required_models'])

    def test_live_runtime_cooldown_does_not_cause_false_policy_readback_failure(self):
        account = self.account()
        result = self.assess(['29'] * 4)
        def admin(method, path, key, body=None):
            if method == 'PUT':
                account.update({k: v for k, v in body.items() if k != 'group_allowed_models'})
                account['account_groups'] = [{'group_id': int(k), 'allowed_models': v}
                                              for k, v in body.get('group_allowed_models', {}).items()]
                account['extra']['openai_excel_bps_rate_limit_reset_at'] = '2099-01-01T00:00:00Z'
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(m.legacy, 'admin', side_effect=admin):
            applied = m.apply_one(result, 'key', self.groups, {}, Path(folder))
        self.assertIn('extra', applied['change'])
        self.assertEqual('2099-01-01T00:00:00Z', account['extra']['openai_excel_bps_rate_limit_reset_at'])

    def test_apply_record_rejects_tampered_sample(self):
        result = self.assess(['21'] * 4)
        summary = {'at': m.utcnow(), 'algorithm': m.ALGORITHM, 'prompt_sha256': m.PROMPT_SHA256,
                   'run_id': 'test', 'model': m.DEFAULT_MODEL, 'reasoning_effort': 'medium',
                   'expected_answer': '21', 'results': [result]}
        m.validate_record(summary)
        result['samples'][0]['response_model'] = 'other'
        with self.assertRaises(ValueError):
            m.validate_record(summary)

    def test_native_probe_payload_and_secrets_are_not_in_argv_or_result(self):
        response = mock.Mock(returncode=0, stdout='data: ' + json.dumps({'type': 'response.completed',
            'response': {'model': m.DEFAULT_MODEL, 'output': [{'content': [{'type': 'output_text', 'text': '21'}]}]}})
            + '\n__HTTP_STATUS__:200')
        with mock.patch.object(m.subprocess, 'run', return_value=response) as run:
            result = m.probe(self.account(), m.DEFAULT_MODEL, 'medium', '21')
        self.assertNotIn('secret', json.dumps(run.call_args.args))
        self.assertNotIn('secret', json.dumps(result))
        self.assertIn('https://chatgpt.com/backend-api/codex/responses', run.call_args.kwargs['input'])
        self.assertEqual('healthy', result['class'])

    def test_reference_21_uses_tactile_shape_selection(self):
        # Worst-case sets without either desired pair. Shape can be selected
        # by touch. Taking 9 circles and 12 stars defeats every such set.
        def can_fail(circles, stars):
            return any(circles <= max_c and stars <= max_s for max_c, max_s in
                       [(24, 4), (8, 17), (15, 11), (17, 10)])
        self.assertFalse(can_fail(9, 12))
        self.assertTrue(all(can_fail(c, 20 - c) for c in range(21) if c <= 24 and 20-c <= 17))


if __name__ == '__main__':
    unittest.main()
