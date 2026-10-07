import copy
from datetime import datetime, timedelta, timezone
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('candy_recovery', Path(__file__).with_name('oauth_candy_cycle.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class CooldownRecoveryTests(unittest.TestCase):
    def fixture(self, answer='21', *, total=1):
        now = datetime.now(timezone.utc)
        account = {'id': 88, 'platform': 'openai', 'type': 'oauth', 'status': 'active',
            'schedulable': True, 'proxy_id': 9, 'concurrency': 3, 'load_factor': 1, 'priority': 50,
            'rate_limited_at': (now - timedelta(hours=2)).isoformat(),
            'rate_limit_reset_at': (now + timedelta(days=1)).isoformat(),
            'overload_until': None, 'temp_unschedulable_until': None, 'temp_unschedulable_reason': None,
            'credentials': {'access_token': 'secret', 'chatgpt_account_id': 'account'},
            'group_ids': [15, 16],
            'extra': {'openai_excel_bps': True, 'openai_excel_bps_required_group_ids': [16],
                      'openai_excel_bps_rate_limit_reset_at': '2099-01-01T00:00:00Z',
                      'model_rate_limits': {'gpt-6-luna': {'reset_at': '2099-01-01T00:00:00Z'}}}}
        sample = {'completed': True, 'http_status': '200', 'curl_exit': 0,
                  'response_model': m.DEFAULT_MODEL, 'text': answer}
        result = {'id': 88, 'model': m.DEFAULT_MODEL, 'expected_answer': '21', 'samples': [copy.deepcopy(sample) for _ in range(total)],
                  'total': total, 'checked_at': now.isoformat(), 'probe_started_at': (now-timedelta(minutes=1)).isoformat(),
                  'baseline_recovery': m.recovery_snapshot(account), 'action': 'unchanged'}
        return account, result

    def execute(self, account, result, *, conditional=True, readback_fail=False, new_cooldown=False):
        calls=[]
        def admin(method, path, key, body=None):
            calls.append((method, path, body))
            if method == 'POST':
                self.assertTrue(path.endswith('/clear-native-rate-limit'))
                self.assertEqual(9, body['observed_proxy_id'])
                if conditional and not readback_fail:
                    account['rate_limited_at']=None;account['rate_limit_reset_at']=None
                    if new_cooldown:
                        account['rate_limited_at']=m.utcnow()
                        account['rate_limit_reset_at']='2099-01-01T00:00:00Z'
                return {'id':88,'cleared':conditional}
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(m.legacy,'admin',side_effect=admin):
            outcome=m.recover_one(result,'key',Path(folder))
        return outcome,calls

    def test_periodic_unchanged_success_clears_native_only(self):
        account,result=self.fixture()
        prior=copy.deepcopy(account)
        outcome,calls=self.execute(account,result)
        self.assertTrue(outcome['recovered'])
        self.assertEqual(prior['extra'],account['extra'])
        self.assertEqual(prior['group_ids'],account['group_ids'])
        self.assertTrue(account['schedulable'])
        self.assertEqual(1,sum(call[0]=='POST' for call in calls))

    def test_degraded_request_success_can_recover_without_removing_bps(self):
        account,result=self.fixture('29',total=4)
        result['action']='reconcile'
        outcome,_=self.execute(account,result)
        self.assertTrue(outcome['recovered'])
        self.assertTrue(account['extra']['openai_excel_bps'])
        self.assertEqual([15,16],account['group_ids'])

    def test_partial_failure_and_wrong_model_never_clear(self):
        for failure in ({'error':'http_429'}, {'completed':False}, {'response_model':'other'}, {'http_status':'401'}):
            with self.subTest(failure=failure):
                account,result=self.fixture(total=4);result['samples'][2].update(failure)
                outcome,calls=self.execute(account,result)
                self.assertEqual('incomplete_probe_set',outcome['skipped'])
                self.assertFalse(any(c[0]=='POST' for c in calls))

    def test_paused_before_or_after_probe_stays_paused(self):
        for before in (False,True):
            account,result=self.fixture();account['schedulable']=False
            if before:result['baseline_recovery']=m.recovery_snapshot(account)
            outcome,calls=self.execute(account,result)
            self.assertFalse(outcome['recovered']);self.assertFalse(account['schedulable'])
            self.assertFalse(any(c[0]=='POST' for c in calls))

    def test_changed_cooldown_proxy_or_token_skips(self):
        for field,value in (('rate_limit_reset_at','2099-01-01T00:00:00Z'),('proxy_id',10),('status','error')):
            account,result=self.fixture();account[field]=value
            outcome,calls=self.execute(account,result)
            self.assertEqual('concurrent_gate_or_identity_change',outcome['skipped'])
            self.assertFalse(any(c[0]=='POST' for c in calls))
        account,result=self.fixture();account['credentials']['access_token']='new-token'
        outcome,calls=self.execute(account,result)
        self.assertFalse(outcome['recovered']);self.assertFalse(any(c[0]=='POST' for c in calls))

    def test_admin_redacted_credentials_use_atomic_token_hash(self):
        account,result=self.fixture()
        account['credentials'].pop('access_token')
        outcome,calls=self.execute(account,result)
        self.assertTrue(outcome['recovered'])
        body=next(call[2] for call in calls if call[0]=='POST')
        self.assertEqual(result['baseline_recovery']['access_token_sha256'],body['observed_access_token_sha256'])

    def test_empty_api_temp_reason_matches_sql_null(self):
        account,result=self.fixture()
        account['temp_unschedulable_reason']=''
        outcome,_=self.execute(account,result)
        self.assertTrue(outcome['recovered'])

    def test_timezones_are_semantically_equal(self):
        account,result=self.fixture()
        for field in ('rate_limited_at','rate_limit_reset_at'):
            account[field]=datetime.fromisoformat(account[field]).astimezone(timezone(timedelta(hours=9))).isoformat()
        outcome,_=self.execute(account,result)
        self.assertTrue(outcome['recovered'])

    def test_old_or_missing_baseline_does_not_clear(self):
        for old in (False,True):
            account,result=self.fixture()
            if old:result['checked_at']=(datetime.now(timezone.utc)-timedelta(minutes=11)).isoformat()
            else:result.pop('baseline_recovery')
            outcome,calls=self.execute(account,result)
            self.assertFalse(outcome['recovered']);self.assertFalse(any(c[0]=='POST' for c in calls))

    def test_active_other_restriction_not_touched(self):
        account,result=self.fixture();account['temp_unschedulable_until']='2099-01-01T00:00:00Z'
        result['baseline_recovery']=m.recovery_snapshot(account)
        outcome,calls=self.execute(account,result)
        self.assertEqual('other_restriction_present',outcome['skipped'])
        self.assertFalse(any(c[0]=='POST' for c in calls))

    def test_conditional_rejection_is_safe_skip(self):
        account,result=self.fixture();prior=copy.deepcopy(account)
        outcome,_=self.execute(account,result,conditional=False)
        self.assertEqual('conditional_clear_not_applied',outcome['skipped'])
        self.assertEqual(prior,account)

    def test_failed_clear_readback_is_reported(self):
        account,result=self.fixture()
        with self.assertRaisesRegex(RuntimeError,'readback mismatch'):
            self.execute(account,result,readback_fail=True)

    def test_new_cooldown_after_clear_is_not_cleared_again(self):
        account,result=self.fixture()
        outcome,calls=self.execute(account,result,new_cooldown=True)
        self.assertEqual('new_cooldown_after_clear',outcome['skipped'])
        self.assertFalse(outcome['recovered']);self.assertTrue(outcome['clear_applied'])
        self.assertEqual(1,sum(c[0]=='POST' for c in calls))

    def test_cycle_calls_recovery_for_unchanged_and_recovery_errors_fail_health(self):
        account,result=self.fixture()
        account['group_ids']=[15]
        result.update(previous_state='healthy',baseline_policy=m.policy_snapshot(account),
                      run_id='test',reasoning_effort='medium',correct=1,state='healthy',sampling_mode='periodic')
        def admin(method,path,key,body=None):
            if path.startswith('/admin/groups'):
                return {'items':[]}
            return copy.deepcopy(account)
        with tempfile.TemporaryDirectory() as folder:
            state=Path(folder);(state/'admin-key').write_text('key')
            (state/'account-defaults.json').write_text(json.dumps({'templates':{}}))
            with mock.patch.object(m,'inventory',return_value=[account]),                  mock.patch.object(m,'assess_account',return_value=result),                  mock.patch.object(m,'validate_record'),                  mock.patch.object(m.legacy,'admin',side_effect=admin),                  mock.patch.object(m.legacy,'quality_group_ids',return_value={'good':15,'bad':16}),                  mock.patch.object(m,'recover_one',side_effect=RuntimeError('account 88 failed')) as recover:
                with self.assertRaisesRegex(RuntimeError,'recovery'):
                    m.run(state_dir=state,apply=True)
                recover.assert_called_once()
                saved=json.loads((state/'last-run.json').read_text())
                self.assertEqual([],saved['changes'][0]['change'])
                self.assertEqual([88],m.compact(saved)['recovery_errors'])


if __name__=='__main__':
    unittest.main()
