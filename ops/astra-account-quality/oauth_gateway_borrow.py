"""Astra account-quality evidence and conditional Astra/Sol borrowing policy.

No account/group PUT, cookie probe, quota refresh, or reset-credit use occurs.
"""
import concurrent.futures
import copy
from datetime import datetime, timedelta, timezone
import fcntl
import json
import math
import os
from pathlib import Path
import re
import uuid
# The CLI can run under python -I; never depend on cwd/scriptdir in sys.path.
if 'candy' not in globals():
    import importlib.util
    import sys
    candy_spec = importlib.util.spec_from_file_location(
        'oauth_candy_cycle', Path(__file__).with_name('oauth_candy_cycle.py'))
    candy = sys.modules.get(candy_spec.name)
    if candy is None or Path(getattr(candy, '__file__', '')).resolve() != Path(candy_spec.origin).resolve():
        candy = importlib.util.module_from_spec(candy_spec)
        sys.modules[candy_spec.name] = candy
        candy_spec.loader.exec_module(candy)

SOURCE_POOL_LIMIT = 12
PENDING_RETRY_SECONDS = 300
PENDING_MAX_RETRY_SECONDS = 7 * 24 * 3600

QUALITY_MODE = 'astra_controls_sol_v2'
DETECTION_MODELS = ('gpt-6-astra',)
ROUTE_MODELS = ('gpt-6-astra', 'gpt-6.1-sol')
BORROW_MODELS = tuple(sorted((*ROUTE_MODELS, 'gpt-6-sol')))
# Keep the old import surface for operators; it describes route models only.
MODELS = ROUTE_MODELS
POLICY_KEYS = ('openai_gateway_borrow_models', 'quality_candy_models', 'quality_candy',
               'openai_excel_bps', 'openai_excel_bps_required_group_ids',
               'openai_excel_bps_required_models', 'openai_gateway_borrow_quality_mode',
               'openai_gateway_borrow_quality_pending', 'openai_gateway_borrow_initial_ready',
               'openai_gateway_borrow_initial_ready_fingerprint')


def owned_policy(account):
    extra = account.get('extra') or {}
    return {key: copy.deepcopy(extra.get(key)) for key in POLICY_KEYS}


def identity(account):
    baseline = candy.recovery_snapshot(account)
    return {'credential_sha256': baseline['credential_sha256'], 'proxy_id': baseline['proxy_id']}


def model_supported(account, model):
    mapping = (account.get('credentials') or {}).get('model_mapping')
    return isinstance(mapping, dict) and model in mapping and mapping[model] == model


def previous_record(account, model):
    extra = account.get('extra') or {}
    if model not in DETECTION_MODELS:
        return None
    candidates = [extra.get('quality_candy')]
    if extra.get('openai_gateway_borrow_quality_mode') != QUALITY_MODE:
        candidates.append((extra.get('quality_candy_models') or {}).get(model))
    valid = [record for record in candidates if isinstance(record, dict)
             and matching_classification(account, record, model, 'medium', '21', fresh=True)]
    # Match the backend: latest confirmed classification wins; a later single
    # refresh breaks ties, and account record wins an exact tie.
    return max(valid, key=lambda record: (candy.timestamp(record['checked_at']),
               candy.timestamp(record['latest_probe_at'])), default=None)


def matching_classification(account, prior, model, reasoning, answer, *, fresh=False):
    if not classified(prior, model, reasoning, answer):
        return False
    if ('latest_probe_proxy_id' not in prior
            or prior.get('latest_probe_credential_sha256') != identity(account)['credential_sha256']
            or prior.get('latest_probe_proxy_id') != account.get('proxy_id')):
        return False
    if fresh:
        observed = candy.timestamp(prior.get('latest_probe_at'))
        confirmed = candy.timestamp(prior.get('checked_at'))
        if (observed is None or confirmed is None or observed < confirmed
                or not -30 <= (datetime.now(timezone.utc) - observed).total_seconds() <= 24 * 3600):
            return False
    return True


def reusable_classification(account, reasoning, answer, run_id):
    """Migrate a stored real Astra classification without inventing a new sample."""
    extra = account.get('extra') or {}
    if extra.get('openai_gateway_borrow_quality_mode') == QUALITY_MODE:
        return None
    if extra.get('openai_gateway_borrow_quality_pending') is True:
        return None
    prior = previous_record(account, DETECTION_MODELS[0])
    if not matching_classification(account, prior, DETECTION_MODELS[0], reasoning, answer, fresh=True):
        return None
    return {'id': account['id'], 'previous_state': prior['state'], 'state': prior['state'],
        'baseline_policy': owned_policy(account), 'baseline_recovery': candy.recovery_snapshot(account),
        'checked_at': candy.utcnow(), 'classification_checked_at': prior['checked_at'],
        'samples': [], 'total': 0, 'correct': 0, 'run_id': run_id, 'model': DETECTION_MODELS[0],
        'reasoning_effort': reasoning, 'expected_answer': answer,
        'sampling_mode': 'migration', 'action': 'reuse_classification'}


def classified(prior, model, reasoning, answer):
    return bool(isinstance(prior, dict) and type(prior.get('version')) is int and prior['version'] == 1
        and re.fullmatch(r'[0-9]{8}T[0-9]{6}Z-[-_a-zA-Z0-9]{1,64}', str(prior.get('run_id') or ''))
        and prior.get('algorithm') == candy.ALGORITHM
        and prior.get('prompt_sha256') == candy.PROMPT_SHA256 and prior.get('model') == model
        and prior.get('reasoning_effort') == reasoning and prior.get('expected_answer') == answer
        and type(prior.get('total')) is int and prior['total'] == 4 and type(prior.get('correct')) is int
        and 0 <= prior['correct'] <= 4
        and prior.get('state') == ('healthy' if prior['correct'] >= 3 else 'degraded'))


def exhausted_quota(sample):
    if sample.get('curl_exit') != 0 or sample.get('http_status') != '429':
        return False
    if sample.get('failure_kind') == 'quota_exhausted':
        return True
    if sample.get('error_code') in ('usage_limit_reached', 'quota_exceeded', 'insufficient_quota', 'billing_hard_limit_reached'):
        return True
    headers = sample.get('quota_headers') or {}
    for window in ('primary', 'secondary'):
        try:
            if (float(headers.get('x-codex-' + window + '-used-percent', '-1')) >= 100
                    and float(headers.get('x-codex-' + window + '-reset-after-seconds', '0')) > 0):
                return True
        except (ValueError, TypeError):
            pass
    return False


def probe_skip_reason(account):
    extra = account.get('extra') or {}
    if (extra.get('openai_gateway_borrow_quality_pending') is True
            and extra.get('openai_gateway_borrow_initial_ready') is not True):
        return 'initial_defaults_pending'
    if account.get('status') not in (None, 'active') or account.get('schedulable') is False:
        return 'paused_or_inactive'
    now = datetime.now(timezone.utc)
    for field in ('rate_limit_reset_at', 'overload_until', 'temp_unschedulable_until'):
        until = candy.timestamp(account.get(field))
        if until and until > now:
            # A bounded near-expiry sample can still establish the existing safe native recovery proof.
            if field == 'rate_limit_reset_at' and (until - now).total_seconds() <= 120:
                continue
            return 'active_' + field
    return None


def assess_model(account, model, *, initial, reasoning, expected_answer, run_id,
                 probe_fn=None, on_sample=None, skip_reason=None):
    prior = previous_record(account, model)
    valid_prior = matching_classification(account, prior, model, reasoning, expected_answer, fresh=True)
    previous = prior['state'] if valid_prior else None
    bootstrap = not valid_prior
    result = {'id': account['id'], 'previous_state': previous,
        'baseline_policy': owned_policy(account), 'baseline_recovery': candy.recovery_snapshot(account),
        'probe_started_at': candy.utcnow(), 'samples': [], 'run_id': run_id, 'model': model,
        'reasoning_effort': reasoning, 'expected_answer': expected_answer,
        'sampling_mode': 'initial' if initial else 'bootstrap' if bootstrap else 'periodic'}
    skip_reason = skip_reason or probe_skip_reason(account)
    if skip_reason:
        result.update(state='inconclusive', action='preserve', total=0, correct=0,
                      checked_at=candy.utcnow(), skipped=skip_reason)
        return result
    if model not in DETECTION_MODELS or not model_supported(account, model):
        result.update(state='inconclusive', action='preserve', total=0, correct=0,
                      checked_at=candy.utcnow(), skipped='unsupported_model')
        return result
    target = 4 if initial or bootstrap else 1
    probe_fn = probe_fn or candy.probe
    while len(result['samples']) < target:
        sample = probe_fn(account, model, reasoning, expected_answer)
        sample.update(number=len(result['samples']) + 1)
        sample['class'] = candy.sample_class(sample, model, expected_answer)
        result['samples'].append(sample)
        if on_sample:
            on_sample(result)
        if exhausted_quota(sample):
            result['stopped'] = ('confirmed_shared_quota_exhausted'
                if sample.get('failure_kind') == 'quota_exhausted' or sample.get('error_code') in (
                    'usage_limit_reached', 'quota_exceeded', 'insufficient_quota', 'billing_hard_limit_reached')
                else 'confirmed_model_quota_exhausted')
            break
        if sample.get('curl_exit') == 0 and sample.get('http_status') == '429':
            result['stopped'] = 'transient_model_rate_limit'
            break
        if sample.get('curl_exit') == 0 and sample.get('http_status') == '401':
            result['stopped'] = sample.get('failure_kind') or 'credential_rejected'
            break
        if sample['class'] == 'inconclusive':
            # Any incomplete attempt makes this confirmation round unusable.
            # Remaining attempts cannot establish a classification, so stop.
            result['stopped'] = 'incomplete_native_sample'
            break
        if target == 1 and sample['class'] in ('healthy', 'degraded') and sample['class'] != previous:
            target = 4
    complete = all(s['class'] != 'inconclusive' for s in result['samples'])
    result.update(total=len(result['samples']), correct=sum(s['class'] == 'healthy' for s in result['samples']),
                  checked_at=candy.utcnow(), state='inconclusive', action='preserve')
    if complete:
        result['state'] = ('healthy' if result['correct'] >= 3 else 'degraded') if target == 4 else result['samples'][0]['class']
        result['action'] = 'reconcile' if target == 4 else 'unchanged'
    return result


def evidence(result):
    return {key: result[key] for key in ('state', 'model', 'reasoning_effort', 'expected_answer',
                                       'correct', 'total', 'checked_at', 'run_id')} | {
        'algorithm': candy.ALGORITHM, 'prompt_sha256': candy.PROMPT_SHA256}


def legacy_borrow_seed(account):
    """Carry the old fail-closed scope forward, without claiming per-model quality."""
    extra = account.get('extra') or {}
    prior = extra.get('quality_candy')
    if (extra.get('openai_gateway_borrow_models') is not None
            or extra.get('openai_excel_bps') is not True
            or not extra.get('openai_excel_bps_required_group_ids')
            or not isinstance(extra.get('openai_excel_bps_required_models'), list)
            or not isinstance(prior, dict)
            or not classified(prior, 'gpt-6-astra', prior.get('reasoning_effort'), prior.get('expected_answer'))):
        return []
    aliases = {'gpt-6-sol': 'gpt-6.1-sol', 'gpt-6.1-sol': 'gpt-6.1-sol', 'gpt-6-astra': 'gpt-6-astra'}
    return sorted({aliases[model] for model in extra['openai_excel_bps_required_models'] if model in aliases})


def desired_borrow(account, results):
    models = set((account.get('extra') or {}).get('openai_gateway_borrow_models') or legacy_borrow_seed(account))
    for result in results:
        if (result.get('model') not in DETECTION_MODELS
                or result['state'] not in ('healthy', 'degraded')
                or not (complete_native(result) or result.get('action') == 'reuse_classification')):
            continue
        # Account quality is controlled by Astra, including the legacy Sol alias.
        return list(BORROW_MODELS) if result['state'] == 'degraded' else []
    return sorted(models)


def apply_account(account, results, key, backup_dir):
    account_id = account['id']
    current = candy.legacy.admin('GET', f'/admin/accounts/{account_id}', key)
    if owned_policy(current) != owned_policy(account) or current.get('proxy_id') != account.get('proxy_id'):
        return {'id': account_id, 'applied': False, 'skipped': 'concurrent_policy_change'}
    if current.get('status') != 'active' or current.get('schedulable') is not True:
        return {'id': account_id, 'applied': False, 'skipped': 'paused_or_inactive'}
    completed = [r for r in results if r.get('model') in DETECTION_MODELS
                 and r['state'] in ('healthy', 'degraded') and complete_native(r)]
    reused = [r for r in results if r.get('model') in DETECTION_MODELS
              and r.get('action') == 'reuse_classification']
    if not completed and not reused:
        return {'id': account_id, 'applied': False, 'skipped': 'incomplete_probe_set'}
    candy.legacy.save_private(backup_dir / f'account-{account_id}.borrow-before.json', current)
    payload = {'policy_mode': QUALITY_MODE,
        'observed_at': min(r['checked_at'] for r in completed) if completed else candy.utcnow(),
        'expected_proxy_id': account.get('proxy_id'),
        'credential_sha256': identity(account)['credential_sha256'],
        'expected_policy': owned_policy(account),
        'model_results': {r['model']: evidence(r) for r in completed},
        'borrow_models': desired_borrow(account, completed or reused), 'retire_bps': bool(legacy_borrow_seed(account))}
    outcome = candy.legacy.admin('POST', f'/admin/accounts/{account_id}/gateway-borrow-policy', key, payload)
    if outcome.get('id') != account_id:
        raise RuntimeError(f'account {account_id} unexpected borrow response identity')
    if not outcome.get('applied'):
        return {'id': account_id, 'applied': False, 'skipped': outcome.get('reason', 'conditional_policy_not_applied')}
    after = candy.legacy.admin('GET', f'/admin/accounts/{account_id}', key)
    if sorted((after.get('extra') or {}).get('openai_gateway_borrow_models') or []) != payload['borrow_models']:
        raise RuntimeError(f'account {account_id} borrow policy readback mismatch')
    if ((after.get('extra') or {}).get('openai_gateway_borrow_quality_mode') != QUALITY_MODE
            or (after.get('extra') or {}).get('openai_gateway_borrow_quality_pending') is True):
        raise RuntimeError(f'account {account_id} quality mode readback mismatch')
    for field in ('group_ids', 'status', 'schedulable', 'proxy_id', 'concurrency', 'load_factor', 'priority'):
        if after.get(field) != current.get(field):
            raise RuntimeError(f'account {account_id} unexpected borrow {field} change')
    if candy.group_allowed_models(after) != candy.group_allowed_models(current):
        raise RuntimeError(f'account {account_id} unexpected group ACL change')
    return {'id': account_id, 'applied': True, 'change': ['gateway_borrow_policy'], 'reason': outcome.get('reason')}


def complete_native(result):
    samples = result.get('samples') or []
    return len(samples) in (1, 4) and result['total'] == len(samples) and all(
        candy.sample_class(sample, result['model'], result['expected_answer']) in ('healthy', 'degraded')
        for sample in samples)


def pending_retry_record(account, results, *, now=None):
    """Bound minute retries without changing account quality or cooldowns."""
    now = now or datetime.now(timezone.utc)
    retry_at = now + timedelta(seconds=PENDING_RETRY_SECONDS)
    reason = 'initial_classification_not_verified'
    reset_times = []
    for result in results:
        for sample in result.get('samples') or []:
            if sample.get('curl_exit') != 0 or sample.get('http_status') != '429':
                continue
            headers = sample.get('quota_headers') or {}
            observed = candy.timestamp(sample.get('quota_observed_at'))
            if observed is None or not -30 <= (now - observed).total_seconds() <= 600:
                continue
            for window in ('primary', 'secondary'):
                try:
                    used = float(headers.get('x-codex-' + window + '-used-percent', '-1'))
                    seconds = float(headers.get('x-codex-' + window + '-reset-after-seconds', '0'))
                    if not (math.isfinite(used) and used == 100 and math.isfinite(seconds) and seconds > 0):
                        continue
                    reset = observed + timedelta(seconds=min(seconds, PENDING_MAX_RETRY_SECONDS))
                    if reset > now:
                        reset_times.append(reset)
                except (ValueError, TypeError, OverflowError):
                    continue
    if reset_times:
        retry_at = min(max(reset_times), now + timedelta(seconds=PENDING_MAX_RETRY_SECONDS))
        reason = 'observed_quota_exhausted'
    return {**identity(account), 'retry_after': retry_at.isoformat(), 'reason': reason}


def pending_retry_active(account, record, *, now=None):
    if not isinstance(record, dict) or any(record.get(key) != value for key, value in identity(account).items()):
        return False
    until = candy.timestamp(record.get('retry_after'))
    now = now or datetime.now(timezone.utc)
    return until is not None and 0 < (until - now).total_seconds() <= PENDING_MAX_RETRY_SECONDS


def source_history_scores(items):
    """Rank actual target success and attributable failures; never infer model readiness."""
    by_source = {}
    now = datetime.now(timezone.utc)
    attributed_failures = {'target_probe_degraded', 'target_quality_failed',
                          'target_ticket_failed', 'ticket_not_qualified',
                          'target_ticket_validation_failed'}
    for item in items:
        source = item.get('source_account_id')
        if type(source) is not int or source <= 0:
            continue
        passed = candy.timestamp(item.get('last_pass'))
        failed = candy.timestamp(item.get('last_failure'))
        reason = item.get('last_reason')
        events = by_source.setdefault(source, {'pass': None, 'failure': None})
        if (item.get('target_account_id', 1) > 0 and passed
                and 0 <= (now - passed).total_seconds() <= 24 * 3600):
            events['pass'] = max(events['pass'] or passed, passed)
        if (reason in attributed_failures and failed
                and 0 <= (now - failed).total_seconds() <= 15 * 60):
            events['failure'] = max(events['failure'] or failed, failed)
    scores = {}
    for source, events in by_source.items():
        passed, failed = events['pass'], events['failure']
        if failed and (not passed or failed >= passed):
            scores[source] = (-1, failed.timestamp())
        elif passed:
            scores[source] = (1, passed.timestamp())
    return scores


def routing_payload(current, accounts, results, history_items=(), *, pending_only=False, partial=False,
                    observed_account_ids=None):
    by_id = {a['id']: a for a in accounts}
    by_model = {(r['id'], r['model']): r for r in results}
    observed_ids = set(observed_account_ids if observed_account_ids is not None else
                       (r['id'] for r in results if r.get('samples')))
    targets = {}
    for account in accounts:
        owned = (account.get('extra') or {}).get('openai_gateway_borrow_models') or []
        models = list(ROUTE_MODELS) if any(model in owned for model in BORROW_MODELS) else []
        if models:
            targets[str(account['id'])] = models
    # Quality is account-wide; normal cycles require a real Astra probe in this run.
    sources = []
    preserve_registered = pending_only or any(r.get('action') == 'reuse_classification' for r in results)
    for account_id, account in by_id.items():
        if (str(account_id) in targets or account.get('status') != 'active' or account.get('schedulable') is not True
                or not model_supported(account, DETECTION_MODELS[0])):
            continue
        if any(candy.timestamp(account.get(field)) and candy.timestamp(account[field]) > datetime.now(timezone.utc)
               for field in ('rate_limit_reset_at', 'overload_until', 'temp_unschedulable_until')):
            continue
        r = by_model.get((account_id, DETECTION_MODELS[0]))
        if (r and r['state'] == 'healthy' and complete_native(r)
                and r['baseline_recovery']['credential_sha256'] == identity(account)['credential_sha256']
                and r['baseline_recovery']['proxy_id'] == account.get('proxy_id')
                and candy.timestamp(r['checked_at']) is not None
                and 0 <= (datetime.now(timezone.utc) - candy.timestamp(r['checked_at'])).total_seconds() <= 600):
            sources.append((r['checked_at'], account_id))
        elif account_id not in observed_ids and (preserve_registered or (partial and r is None)) and account_id in (
                (current.get('cookie_pool') or {}).get('source_account_ids') or []):
            # The minute pending run must not empty a live source pool just because
            # it deliberately does not re-probe established accounts.
            prior = previous_record(account, DETECTION_MODELS[0])
            if matching_classification(account, prior, DETECTION_MODELS[0], 'medium', '21', fresh=True) and prior['state'] == 'healthy':
                sources.append((prior.get('latest_probe_at') or prior['checked_at'], account_id))
    history_scores = source_history_scores(history_items)
    old_sources = set((current.get('cookie_pool') or {}).get('source_account_ids') or [])
    # Keep demonstrated good candidates; rotate only the neutral exploration slots.
    sources.sort(key=lambda entry: (history_scores.get(entry[1], (0, 0)),
        entry[1] not in old_sources if history_scores.get(entry[1], (0, 0))[0] == 0 else False,
        entry[0], entry[1]), reverse=True)
    # Membership carries selection; stable ordering avoids needless pool revisions.
    source_ids = sorted(account_id for _, account_id in sources[:SOURCE_POOL_LIMIT])
    target_ids = sorted(int(account_id) for account_id in targets)
    if len(target_ids) > 64:
        raise RuntimeError('borrow target pool exceeds configured capacity; required account policy retained')
    desired = copy.deepcopy(current)
    desired['auto_quality'] = True
    desired['quality_mode'] = QUALITY_MODE
    desired['account_scheduling'] = False
    pool = desired.setdefault('cookie_pool', {})
    pool.update(enabled=True, quality_mode=QUALITY_MODE, models=list(ROUTE_MODELS), target_models=targets,
                source_account_ids=source_ids, target_account_ids=target_ids,
                ip_affinity=False, rotate_nodes=False)
    desired.setdefault('ws_session', {})['enabled'] = False
    return desired


def update_routing(accounts, results, key, backup_dir, *, pending_only=False, partial=False,
                   observed_account_ids=None):
    current = candy.legacy.admin('GET', '/admin/settings/astra-routing', key)
    # The bounded read contains no credentials and is advisory only; failure keeps fresh-native ordering.
    try:
        history = candy.legacy.admin('GET', '/admin/accounts/astra-gateway/history?passed=false&page=1', key)
        history_items = (history.get('items') or [])[:20]
        for page in (2, 3):
            if len(history_items) < (page - 1) * 20 or history.get('total', len(history_items)) <= len(history_items):
                break
            history = candy.legacy.admin('GET', f'/admin/accounts/astra-gateway/history?passed=false&page={page}', key)
            history_items.extend((history.get('items') or [])[:20])
    except Exception:
        history_items = []
    desired = routing_payload(current, accounts, results, history_items, pending_only=pending_only, partial=partial,
                              observed_account_ids=observed_account_ids)
    candy.legacy.save_private(backup_dir / 'astra-routing-before.json', current)
    fresh = candy.legacy.admin('GET', '/admin/settings/astra-routing', key)
    if fresh != current:
        return {'updated': False, 'skipped': 'concurrent_routing_change'}
    if desired == current:
        pool = current.get('cookie_pool') or {}
        return {'updated': False, 'skipped': 'unchanged', 'sources': pool.get('source_account_ids', []),
                'targets': pool.get('target_account_ids', [])}
    response = candy.legacy.admin('PUT', '/admin/settings/astra-routing', key, desired)
    after = candy.legacy.admin('GET', '/admin/settings/astra-routing', key)
    for field in ('auto_quality', 'quality_mode', 'account_scheduling', 'cookie_pool', 'ws_session'):
        if after.get(field) != response.get(field):
            raise RuntimeError('borrow routing readback mismatch')
    if after.get('quality_mode') != QUALITY_MODE:
        raise RuntimeError('borrow routing quality mode mismatch')
    pool = after.get('cookie_pool') or {}
    for field in ('enabled', 'quality_mode', 'models', 'target_models', 'source_account_ids', 'target_account_ids', 'ip_affinity', 'rotate_nodes'):
        if pool.get(field) != desired['cookie_pool'].get(field):
            raise RuntimeError('borrow routing desired policy mismatch')
    return {'updated': True, 'sources': pool['source_account_ids'], 'targets': pool['target_account_ids']}


def compact(summary):
    results = summary['results']
    output = {'run_id': summary['run_id'], 'route_mode': 'borrow',
        'accounts': len({r['id'] for r in results}), 'probes': sum(r['total'] for r in results),
        'quality_mode': QUALITY_MODE,
        'models': {model: {state: sum(r['model'] == model and r['state'] == state for r in results)
            for state in ('healthy', 'degraded', 'inconclusive')} for model in DETECTION_MODELS},
        'changed': sum(bool(c.get('applied')) for c in summary.get('changes', [])),
        'apply_errors': [c['id'] for c in summary.get('changes', []) if c.get('error')],
        'skipped_concurrent': [c['id'] for c in summary.get('changes', []) if c.get('skipped') in (
            'concurrent_policy_change', 'policy_or_identity_changed', 'stale_observation', 'stale_model_result')],
        'apply_skipped': {reason: sum(c.get('skipped') == reason for c in summary.get('changes', []))
                          for reason in sorted({c['skipped'] for c in summary.get('changes', []) if c.get('skipped')})},
        'recovered': sum(bool(r.get('recovered')) for r in summary.get('recoveries', [])),
        'recovery_errors': [r['id'] for r in summary.get('recoveries', []) if r.get('error')],
        'quota_updated': sum(bool(q.get('updated')) for q in summary.get('quota_updates', [])),
        'quota_errors': [q['id'] for q in summary.get('quota_updates', []) if q.get('error')],
        'routing': summary.get('routing')}
    # Existing health collector consumes these aggregate keys.
    output.update({state: sum(r['state'] == state for r in results) for state in ('healthy', 'degraded', 'inconclusive')})
    return output


def run_borrow(*, state_dir, initial=False, apply=False, account_ids=(), reasoning='medium',
               expected_answer='21', apply_record=None, max_age_minutes=180, workers=4,
               pending_only=False):
    if workers not in range(1, 5) or not re.fullmatch(r'\d+', expected_answer):
        raise ValueError('invalid candy run arguments')
    if reasoning != 'medium' or expected_answer != '21':
        raise ValueError('account-quality mode requires Astra medium and answer 21')
    if apply_record:
        raise ValueError('borrow mode requires fresh native probes; replay is disabled')
    os.umask(0o077)
    state_dir = Path(state_dir)
    state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (state_dir / 'cycle.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            output = {'route_mode': 'borrow', 'pending_only': pending_only, 'skipped': 'cycle_busy'}
            print(json.dumps(output, sort_keys=True), flush=True)
            return output
        accounts = candy.inventory()
        selected = [a for a in accounts if not account_ids or a['id'] in account_ids]
        retry_path = state_dir / 'pending-retry.json'
        pending_retries = json.loads(retry_path.read_text()) if retry_path.exists() else {}
        if not isinstance(pending_retries, dict):
            raise ValueError('invalid initial-classification retry state')
        if pending_only:
            selected = [a for a in selected
                if (a.get('extra') or {}).get('openai_gateway_borrow_quality_pending') is True
                and (a.get('extra') or {}).get('openai_gateway_borrow_initial_ready') is True
                and a.get('status') == 'active' and a.get('schedulable') is True
                and model_supported(a, DETECTION_MODELS[0])]
            ready_count = len(selected)
            selected = [a for a in selected if not pending_retry_active(a, pending_retries.get(str(a['id'])))]
        if not selected:
            if pending_only:
                output = {'route_mode': 'borrow', 'pending_only': True,
                          'skipped': 'pending_retry_deferred' if ready_count else 'no_ready_pending_accounts'}
                print(json.dumps(output, sort_keys=True), flush=True)
                return output
            raise ValueError('empty OAuth inventory')
        key = (state_dir / 'admin-key').read_text().strip()
        run_id = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8]
        run_dir = state_dir / 'candy-runs' / run_id
        run_dir.mkdir(mode=0o700, parents=True)
        summary = {'at': candy.utcnow(), 'run_id': run_id, 'algorithm': candy.ALGORITHM,
                   'prompt_sha256': candy.PROMPT_SHA256, 'route_mode': 'borrow', 'results': [],
                   'quality_mode': QUALITY_MODE, 'pending_only': pending_only,
                   'changes': [], 'recoveries': [], 'quota_updates': [], 'applied': apply}
        def task(account):
            out = []
            stop_reason = None
            for model in DETECTION_MODELS:
                path = run_dir / f"account-{account['id']}-{model}.json"
                result = None if initial else reusable_classification(account, reasoning, expected_answer, run_id)
                if result is None:
                    result = assess_model(account, model, initial=initial or pending_only, reasoning=reasoning,
                        expected_answer=expected_answer, run_id=run_id, skip_reason=stop_reason,
                        on_sample=lambda partial: candy.legacy.save_private(path, partial))
                candy.legacy.save_private(path, result)
                out.append(result)
                if result.get('stopped') and result['stopped'] not in ('transient_model_rate_limit', 'confirmed_model_quota_exhausted'):
                    stop_reason = result['stopped']
            return out
        def apply_fresh(account, results):
            try:
                change = apply_account(account, results, key, run_dir)
            except Exception as exc:
                change = {'id': account['id'], 'error': type(exc).__name__}
            summary['changes'].append(change)
            verified = change.get('applied') or change.get('skipped') == 'unchanged'
            retry_key = str(account['id'])
            if verified and retry_key in pending_retries:
                pending_retries.pop(retry_key)
                candy.legacy.save_private(retry_path, pending_retries)
            elif pending_only and not verified and any(r.get('total', 0) > 0 for r in results):
                pending_retries[retry_key] = pending_retry_record(account, results)
                candy.legacy.save_private(retry_path, pending_retries)
            for result in results:
                # Quota headers have their own token/proxy/freshness CAS and do not
                # depend on generation completion or a quality-policy mutation.
                try:
                    summary['quota_updates'].append(candy.persist_quota_one(result, key, run_dir))
                except Exception as exc:
                    summary['quota_updates'].append({'id': account['id'], 'error': type(exc).__name__})
            # Preserve the previously authorized narrow native recovery proof; borrowed success is never evidence.
            complete = [r for r in results if r.get('skipped') != 'unsupported_model']
            if verified and complete and all(complete_native(r) for r in complete):
                try:
                    summary['recoveries'].append(candy.recover_one(complete[-1], key, run_dir))
                except Exception as exc:
                    summary['recoveries'].append({'id': account['id'], 'error': type(exc).__name__})
            candy.legacy.save_private(run_dir / 'applied.json', summary)
        with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
            jobs = {pool.submit(task, account): account for account in selected}
            for job in concurrent.futures.as_completed(jobs):
                results = job.result()
                summary['results'].extend(results)
                # Apply immediately, so a long full-pool bootstrap cannot stale the early CAS evidence.
                if apply:
                    apply_fresh(jobs[job], results)
                candy.legacy.save_private(run_dir / 'result.json', summary)
        if apply:
            try:
                verified_ids = {c['id'] for c in summary['changes'] if c.get('applied') or c.get('skipped') == 'unchanged'}
                routing_results = [r for r in summary['results'] if r['id'] in verified_ids]
                summary['routing'] = update_routing(candy.inventory(), routing_results, key, run_dir,
                                                   pending_only=pending_only, partial=bool(account_ids),
                                                   observed_account_ids={r['id'] for r in summary['results'] if r.get('samples')})
            except Exception as exc:
                summary['routing'] = {'updated': False, 'error': type(exc).__name__}
        summary['completed_at'] = candy.utcnow()
        candy.legacy.save_private(run_dir / ('applied.json' if apply else 'result.json'), summary)
        candy.legacy.save_private(state_dir / ('pending-last-run.json' if pending_only else 'last-run.json'), summary)
        output = compact(summary)
        if not pending_only:
            history_path = state_dir / 'candy-history.json'
            history = json.loads(history_path.read_text()) if history_path.exists() else []
            history.append({'at': summary['at'], **output})
            candy.legacy.save_private(history_path, history[-96:])
        print(json.dumps(output, sort_keys=True), flush=True)
        if output['apply_errors'] or output['skipped_concurrent'] or output['recovery_errors'] or output['quota_errors'] or (summary.get('routing') or {}).get('error'):
            raise RuntimeError('borrow cycle operations need reconciliation')
        return summary
