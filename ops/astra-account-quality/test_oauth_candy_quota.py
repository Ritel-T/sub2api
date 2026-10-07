import copy
from datetime import datetime, timedelta, timezone
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('candy_quota', Path(__file__).with_name('oauth_candy_cycle.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class QuotaHeaderTests(unittest.TestCase):
    def fixture(self, status='200'):
        at = datetime.now(timezone.utc).isoformat()
        account = {'id': 88, 'platform': 'openai', 'type': 'oauth', 'proxy_id': 9,
                   'credentials': {'access_token': 'secret'},
                   'extra': {'unrelated': True, 'openai_excel_bps': True}}
        sample = {'http_status': status, 'curl_exit': 0,
                  'quota_observed_at': at,
                  'quota_headers': {'x-codex-primary-used-percent': '19'}}
        result = {'id': 88, 'baseline_recovery': m.recovery_snapshot(account), 'samples': [sample]}
        return account, result

    def execute(self, account, result, *, updated=True, newer=False, failed=False):
        expected = {'codex_primary_used_percent': 19, 'codex_usage_updated_at': result['samples'][-1]['quota_observed_at']}
        calls = []
        def admin(method, path, key, body=None):
            calls.append((method, path, body))
            if method == 'POST':
                self.assertTrue(path.endswith('/codex-usage-snapshot'))
                self.assertNotIn('secret', str(body))
                self.assertEqual(result['baseline_recovery']['access_token_sha256'], body['observed_access_token_sha256'])
                if updated and not failed:
                    account['extra'].update(expected)
                    if newer:
                        account['extra']['codex_usage_updated_at'] = (datetime.now(timezone.utc)+timedelta(seconds=1)).isoformat()
                        account['extra']['codex_primary_used_percent'] = 20
                return {'id': 88, 'updated': updated, 'quota': expected}
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(m.legacy, 'admin', side_effect=admin):
            outcome = m.persist_quota_one(result, 'key', Path(folder))
        return outcome, calls

    def test_only_final_header_block_and_quota_allowlist_retained(self):
        raw = ('HTTP/1.1 200 Connection established\r\n'
               'x-codex-primary-used-percent: 99\r\nProxy-Secret: hidden\r\n\r\n'
               'HTTP/2 200\r\nDate: Sat, 03 Oct 2026 11:30:00 GMT\r\n'
               'X-Codex-Primary-Used-Percent: 19\r\nSet-Cookie: private\r\n\r\n')
        actual = m.response_quota_headers(raw, 'fallback')
        self.assertEqual({'x-codex-primary-used-percent': '19'}, actual['quota_headers'])
        self.assertEqual('2026-10-03T11:30:00+00:00', actual['quota_observed_at'])
        self.assertNotIn('private', str(actual))
        self.assertNotIn('hidden', str(actual))

    def test_missing_headers_does_not_fabricate_snapshot(self):
        self.assertEqual({}, m.response_quota_headers('HTTP/2 200\nDate: invalid\n', 'fallback'))

    def test_invalid_date_uses_probe_start(self):
        actual = m.response_quota_headers('HTTP/2 200\nDate: invalid\nx-codex-primary-used-percent: 19', 'fallback')
        self.assertEqual('fallback', actual['quota_observed_at'])

    def test_same_probe_persists_quota_preserving_other_extra(self):
        account, result = self.fixture()
        outcome, calls = self.execute(account, result)
        self.assertTrue(outcome['updated'])
        self.assertTrue(account['extra']['unrelated'])
        self.assertTrue(account['extra']['openai_excel_bps'])
        self.assertEqual('matched', outcome['readback'])
        self.assertEqual(1, sum(c[0]=='POST' for c in calls))

    def test_429_headers_update_quota_without_being_quality_success(self):
        account, result = self.fixture('429')
        result['samples'][0]['error'] = 'http_429'
        outcome, _ = self.execute(account, result)
        self.assertTrue(outcome['updated'])

    def test_401_and_transport_error_are_not_quota_observations(self):
        for overrides in ({'http_status':'401'}, {'curl_exit':28}, {'quota_headers':{}}):
            account, result = self.fixture()
            result['samples'][0].update(overrides)
            outcome, calls = self.execute(account, result)
            self.assertFalse(outcome['updated'])
            self.assertFalse(any(c[0]=='POST' for c in calls))

    def test_old_header_does_not_overwrite(self):
        account, result = self.fixture()
        result['samples'][0]['quota_observed_at'] = (datetime.now(timezone.utc)-timedelta(minutes=11)).isoformat()
        outcome, calls = self.execute(account, result)
        self.assertEqual('stale_quota_response_headers', outcome['skipped'])
        self.assertFalse(calls)

    def test_endpoint_rejects_concurrent_snapshot_safely(self):
        account, result = self.fixture()
        prior = copy.deepcopy(account)
        outcome, _ = self.execute(account, result, updated=False)
        self.assertEqual('newer_snapshot_or_identity_changed', outcome['skipped'])
        self.assertEqual(prior, account)

    def test_fresher_business_snapshot_after_write_is_valid(self):
        account, result = self.fixture()
        outcome, _ = self.execute(account, result, newer=True)
        self.assertEqual('newer_snapshot', outcome['readback'])

    def test_failed_write_readback_is_error(self):
        account, result = self.fixture()
        with self.assertRaisesRegex(RuntimeError, 'readback mismatch'):
            self.execute(account, result, failed=True)

    def test_cycle_updates_quota_when_quality_is_unchanged_and_errors_fail_service(self):
        account, result = self.fixture()
        account['group_ids'] = [15]
        result.update(model=m.DEFAULT_MODEL, expected_answer='21', action='unchanged',
                      total=1, state='healthy', correct=1, checked_at=m.utcnow(), run_id='test')
        def admin(method, path, key, body=None):
            if path.startswith('/admin/groups'):
                return {'items': []}
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder:
            state = Path(folder)
            (state/'admin-key').write_text('key')
            (state/'account-defaults.json').write_text(json.dumps({'templates': {}}))
            with mock.patch.object(m, 'inventory', return_value=[account]), \
                 mock.patch.object(m, 'assess_account', return_value=result), \
                 mock.patch.object(m, 'validate_record'), \
                 mock.patch.object(m.legacy, 'admin', side_effect=admin), \
                 mock.patch.object(m.legacy, 'quality_group_ids', return_value={'good':15, 'bad':16}), \
                 mock.patch.object(m, 'recover_one', return_value={'id':88, 'recovered':False}), \
                 mock.patch.object(m, 'persist_quota_one', side_effect=RuntimeError('account 88 quota failure')) as persist:
                with self.assertRaises(RuntimeError):
                    m.run(state_dir=state, apply=True)
                persist.assert_called_once()
                saved = json.loads((state/'last-run.json').read_text())
                self.assertEqual([], saved['changes'][0]['change'])
                self.assertEqual([88], m.compact(saved)['quota_errors'])


if __name__ == '__main__':
    unittest.main()
