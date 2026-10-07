"""Offline coordination tests; no production API or raw credentials are logged."""
import contextlib
import copy
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

SCRIPT = Path(__file__).with_name('account_defaults_live.py')
SPEC = importlib.util.spec_from_file_location('defaults_live_under_test', SCRIPT)
defaults = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(defaults)


def account(*, pending=True, configured=False, astra=True):
    return {
        'id': 901, 'platform': 'openai', 'type': 'oauth', 'status': 'active',
        'schedulable': True, 'proxy_id': None, 'group_ids': [2] if configured else [],
        'concurrency': 3 if configured else 1, 'load_factor': 3 if configured else 1,
        'priority': 3 if configured else 1,
        'credentials': {'access_token': 'mock-secret', 'chatgpt_account_id': 'mock-owner',
                        'plan_type': 'plus', 'model_mapping': {'gpt-6-astra': 'gpt-6-astra'} if astra else {'gpt-6.1-sol': 'gpt-6.1-sol'}},
        'extra': {'openai_gateway_borrow_quality_mode': defaults.INITIAL_QUALITY_MODE,
                  'openai_gateway_borrow_quality_pending': pending,
                  'openai_gateway_borrow_initial_ready': False},
    }


class Environment:
    def __init__(self, target, *, completed=False):
        self.target = copy.deepcopy(target)
        self.calls = []
        self.ready_behaviour = 'apply'
        self.fail_next_get = False
        self.defaults_mismatch = False
        self.defaults_unknown = False
        self.post_ready_mutation = None
        self.storage = tempfile.TemporaryDirectory()
        self.root = Path(self.storage.name)
        self.groups = [{'id': 2, 'platform': 'openai', 'status': 'active'}]
        self.template = {'models': list(target['credentials']['model_mapping']),
                         'concurrency': 3, 'load_factor': 3, 'priority': 3}
        defaults.save(self.root / 'account-defaults.json', {'templates': {'plus': self.template}})
        defaults.save(self.root / 'account-defaults-state.json', {'completed': [901] if completed else []})

    def state(self):
        return json.loads((self.root / 'account-defaults-state.json').read_text())

    def listing(self, path):
        if path == '/admin/accounts':
            return [copy.deepcopy(self.target)]
        if path == '/admin/groups':
            return copy.deepcopy(self.groups)
        if path == '/admin/proxies':
            return []
        raise AssertionError(path)

    def api(self, method, path, body=None):
        self.calls.append((method, path, copy.deepcopy(body)))
        if method == 'GET':
            if self.fail_next_get:
                self.fail_next_get = False
                raise RuntimeError('Mock read unavailable')
            return copy.deepcopy(self.target)
        if method == 'PUT':
            if not self.defaults_mismatch:
                self.target.update(copy.deepcopy(body))
            if self.defaults_unknown:
                raise RuntimeError('Mock write response unknown')
            return copy.deepcopy(self.target)
        if method == 'POST' and path.endswith('/gateway-borrow-initial-ready'):
            if self.ready_behaviour not in ('unknown_unapplied', 'skip'):
                self.target['extra']['openai_gateway_borrow_initial_ready'] = True
                self.target['extra']['openai_gateway_borrow_initial_ready_fingerprint'] = defaults.initial_fingerprint(self.target)
            if self.post_ready_mutation:
                self.post_ready_mutation(self.target)
            if self.ready_behaviour == 'unknown_get_failed':
                self.fail_next_get = True
            if self.ready_behaviour.startswith('unknown'):
                raise RuntimeError('Mock write response unknown')
            return {'id': 901, 'applied': True, 'skipped': False, 'reason': 'initial_defaults_ready'}
        raise AssertionError((method, path))

    def count(self, method):
        return sum(call[0] == method for call in self.calls)

    def run(self):
        output = io.StringIO()
        with mock.patch.object(defaults, 'ROOT', self.root), \
                mock.patch.object(defaults, 'listing', self.listing), \
                mock.patch.object(defaults, 'api', self.api), \
                mock.patch('sys.argv', ['defaults']), contextlib.redirect_stdout(output):
            try:
                defaults.main()
            except SystemExit as exc:
                return exc.code, output.getvalue()
        return 0, output.getvalue()

    def close(self):
        self.storage.cleanup()


class InitialReadyTests(unittest.TestCase):
    def environment(self, *args, **kwargs):
        env = Environment(*args, **kwargs)
        self.addCleanup(env.close)
        return env

    def test_unknown_ready_response_is_confirmed_by_get(self):
        env = self.environment(account())
        env.ready_behaviour = 'unknown_applied'
        code, output = env.run()
        self.assertEqual(code, 0)
        self.assertEqual(env.state()['completed'], [901])
        self.assertEqual(env.state()['initial_ready_pending'], {})
        self.assertEqual(env.count('POST'), 1)
        self.assertNotIn('mock-secret', output)
        self.assertNotIn('credential_sha256', output)

    def test_unknown_unapplied_ready_is_retried_without_defaults_put(self):
        env = self.environment(account())
        env.ready_behaviour = 'unknown_unapplied'
        self.assertEqual(env.run()[0], 1)
        self.assertEqual(env.state()['completed'], [])
        self.assertIn('901', env.state()['initial_ready_pending'])
        self.assertEqual(env.count('PUT'), 1)
        env.ready_behaviour = 'apply'
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.state()['completed'], [901])
        self.assertEqual(env.count('PUT'), 1)
        self.assertEqual(env.count('POST'), 2)

    def test_unknown_ready_and_failed_get_next_cycle_reads_before_retry(self):
        env = self.environment(account())
        env.ready_behaviour = 'unknown_get_failed'
        self.assertEqual(env.run()[0], 1)
        self.assertEqual(env.state()['completed'], [])
        env.ready_behaviour = 'apply'
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.count('PUT'), 1)
        self.assertEqual(env.count('POST'), 1)
        self.assertEqual(env.state()['completed'], [901])

    def test_incomplete_defaults_readback_does_not_mark_ready(self):
        env = self.environment(account())
        env.defaults_mismatch = True
        self.assertEqual(env.run()[0], 1)
        self.assertEqual(env.count('POST'), 0)
        self.assertEqual(env.state()['completed'], [])
        self.assertFalse(env.target['extra']['openai_gateway_borrow_initial_ready'])

    def test_existing_nonpending_account_does_not_receive_ready(self):
        env = self.environment(account(pending=False, configured=True), completed=True)
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.count('PUT'), 0)
        self.assertEqual(env.count('POST'), 0)

    def test_legacy_quality_mode_does_not_receive_ready(self):
        target = account(configured=True)
        target['extra']['openai_gateway_borrow_quality_mode'] = 'legacy'
        env = self.environment(target, completed=True)
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.count('PUT'), 0)
        self.assertEqual(env.count('POST'), 0)

    def test_completed_pending_account_only_coordinates_ready(self):
        env = self.environment(account(configured=True), completed=True)
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.count('PUT'), 0)
        self.assertEqual(env.count('POST'), 1)
        self.assertTrue(env.target['extra']['openai_gateway_borrow_initial_ready'])

    def test_completed_pending_unknown_ready_retries_next_cycle_only_ready(self):
        env = self.environment(account(configured=True), completed=True)
        env.ready_behaviour = 'unknown_unapplied'
        self.assertEqual(env.run()[0], 1)
        self.assertEqual(env.state()['completed'], [901])
        self.assertIn('901', env.state()['initial_ready_pending'])
        env.ready_behaviour = 'apply'
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.count('PUT'), 0)
        self.assertEqual(env.count('POST'), 2)
        self.assertEqual(env.state()['initial_ready_pending'], {})

    def test_completed_pending_incomplete_defaults_not_reconfigured(self):
        env = self.environment(account(configured=False), completed=True)
        self.assertEqual(env.run()[0], 1)
        self.assertEqual(env.count('PUT'), 0)
        self.assertEqual(env.count('POST'), 0)
        self.assertEqual(env.state()['completed'], [901])

    def test_completed_pending_stale_fingerprint_is_refreshed_without_put(self):
        target = account(configured=True)
        target['extra'].update(openai_gateway_borrow_initial_ready=True,
                               openai_gateway_borrow_initial_ready_fingerprint='0' * 64)
        env = self.environment(target, completed=True)
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.count('PUT'), 0)
        self.assertEqual(env.count('POST'), 1)
        self.assertTrue(defaults.initial_ready_matches(env.target))

    def test_completed_pending_matching_fingerprint_does_not_rewrite(self):
        target = account(configured=True)
        target['extra'].update(openai_gateway_borrow_initial_ready=True,
                               openai_gateway_borrow_initial_ready_fingerprint=defaults.initial_fingerprint(target))
        env = self.environment(target, completed=True)
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.count('PUT'), 0)
        self.assertEqual(env.count('POST'), 0)

    def test_ready_saved_observation_drift_defers_without_rewrite(self):
        env = self.environment(account())
        env.ready_behaviour = 'unknown_unapplied'
        self.assertEqual(env.run()[0], 1)
        env.target['credentials']['access_token'] = 'different-mock-secret'
        env.target['credentials']['chatgpt_account_id'] = 'different-mock-owner'
        env.ready_behaviour = 'apply'
        code, output = env.run()
        self.assertEqual(code, 1)
        self.assertEqual(env.count('PUT'), 1)
        self.assertEqual(env.count('POST'), 1)
        self.assertNotIn('different-mock-secret', output)

    def test_same_owner_token_refresh_updates_only_ready_observation(self):
        env = self.environment(account())
        env.ready_behaviour = 'unknown_unapplied'
        self.assertEqual(env.run()[0], 1)
        env.target['credentials']['access_token'] = 'refreshed-mock-secret'
        env.ready_behaviour = 'apply'
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.count('PUT'), 1)
        self.assertEqual(env.count('POST'), 2)
        self.assertEqual(env.state()['completed'], [901])
        self.assertTrue(defaults.initial_ready_matches(env.target))

    def test_missing_token_does_not_refresh_ready_observation(self):
        env = self.environment(account())
        env.ready_behaviour = 'unknown_unapplied'
        self.assertEqual(env.run()[0], 1)
        env.target['credentials']['access_token'] = None
        env.ready_behaviour = 'apply'
        self.assertEqual(env.run()[0], 1)
        self.assertEqual(env.count('PUT'), 1)
        self.assertEqual(env.count('POST'), 1)

    def test_config_changed_during_token_refresh_defers_without_rewrite(self):
        env = self.environment(account())
        env.ready_behaviour = 'unknown_unapplied'
        self.assertEqual(env.run()[0], 1)
        env.target['credentials']['access_token'] = 'refreshed-mock-secret'
        env.target['concurrency'] = 4
        env.ready_behaviour = 'apply'
        self.assertEqual(env.run()[0], 1)
        self.assertEqual(env.count('PUT'), 1)
        self.assertEqual(env.count('POST'), 1)

    def test_proxy_changed_during_token_refresh_defers_without_rewrite(self):
        env = self.environment(account())
        env.ready_behaviour = 'unknown_unapplied'
        self.assertEqual(env.run()[0], 1)
        env.target['credentials']['access_token'] = 'refreshed-mock-secret'
        env.target['proxy_id'] = 17
        env.ready_behaviour = 'apply'
        self.assertEqual(env.run()[0], 1)
        self.assertEqual(env.count('PUT'), 1)
        self.assertEqual(env.count('POST'), 1)

    def test_coordination_state_does_not_store_raw_credentials_or_owner(self):
        env = self.environment(account())
        env.ready_behaviour = 'unknown_unapplied'
        code, output = env.run()
        self.assertEqual(code, 1)
        state_json = json.dumps(env.state())
        self.assertNotIn('mock-secret', state_json)
        self.assertNotIn('mock-owner', state_json)
        self.assertNotIn('mock-secret', output)
        self.assertNotIn('mock-owner', output)

    def test_immediate_classification_does_not_race_readback(self):
        env = self.environment(account())
        env.post_ready_mutation = lambda a: a['extra'].update(openai_gateway_borrow_quality_pending=False)
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.state()['completed'], [901])

    def test_no_astra_account_does_not_receive_ready(self):
        env = self.environment(account(astra=False))
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.count('POST'), 0)
        self.assertEqual(env.state()['completed'], [901])

    def test_unknown_defaults_response_is_readback_verified(self):
        env = self.environment(account())
        env.defaults_unknown = True
        self.assertEqual(env.run()[0], 0)
        self.assertEqual(env.state()['completed'], [901])
        self.assertEqual(env.count('PUT'), 1)
        self.assertEqual(env.count('POST'), 1)

    def test_readiness_observation_contract_does_not_include_raw_identity(self):
        env = self.environment(account())
        self.assertEqual(env.run()[0], 0)
        body = next(body for method, path, body in env.calls if method == 'POST')
        self.assertEqual(set(body), {'observed_at', 'expected_proxy_id', 'credential_sha256', 'expected_policy', 'expected_config'})
        self.assertEqual(set(body['expected_policy']), set(defaults.INITIAL_POLICY_KEYS))
        self.assertEqual(set(body['expected_config']), {'concurrency', 'load_factor', 'priority', 'group_ids', 'model_mapping'})
        self.assertEqual(body['expected_config']['group_ids'], [2])
        self.assertEqual(len(body['credential_sha256']), 64)
        self.assertNotIn('mock-secret', json.dumps(body))
        self.assertTrue(body['observed_at'].endswith('Z'))

    def test_fingerprint_matches_independent_go_json_vectors(self):
        # Generated independently by testdata/account_defaults_fingerprint.go.
        target = account(configured=True)
        vectors = [
            ({'gpt-6-astra': 'gpt-6-astra'}, 'c9177c79128bfc140702866b48ff7e71ebacf6434e6841e72b9c2ab4c5208cbf'),
            ({'gpt-6-astra': '<&>模型\u2028\u2029'}, '6940ddd5e63ca7b854239e0e7cc27e9aaad854ce0641e2b54913f0414b392d6d'),
            (None, '3aa99d142521dc79b6ac1b658330ba7e862f801fde024471101995c58c2d82a2'),
        ]
        for mapping, expected in vectors:
            with self.subTest(mapping=mapping):
                target['credentials']['model_mapping'] = mapping
                self.assertEqual(defaults.initial_fingerprint(target), expected)


if __name__ == '__main__':
    unittest.main()
