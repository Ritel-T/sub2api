#!/usr/bin/env python3
"""Native OAuth candy probes and conservative account/group reconciliation.

This module never calls legacy Telegram helpers. Fresh complete native probes
can clear an unchanged native cooldown without enabling paused accounts.
--initial records exactly four sequential attempts per account. Normal cycles
take one sample and, on a classification mismatch, three further samples.
"""

import argparse
import concurrent.futures
import copy
from datetime import datetime, timezone
from email.utils import parsedate_to_datetime
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import uuid

_spec = importlib.util.spec_from_file_location(
    'legacy_quality_cycle', Path(__file__).with_name('oauth_quality_cycle.py'))
legacy = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(legacy)

CANDY_PROMPT = '''在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）
苹果味 桃子味 西瓜味
圆形 7 9 8
五角星形 7 6 4'''
PROMPT = CANDY_PROMPT + '\n\n只输出最终整数，不要解释。'
PROMPT_SHA256 = hashlib.sha256(PROMPT.encode()).hexdigest()
DEFAULT_MODEL = 'gpt-6-astra'
DEFAULT_REASONING = 'medium'
DEFAULT_ANSWER = '21'
ALGORITHM = 'ranxi-candy-sequential-four-v1'
BPS_MODELS = ['gpt-6-sol', 'gpt-6.1-sol', 'gpt-6-astra']
POLICY_KEYS = ('quality_candy', 'quality_model_results', 'openai_excel_bps',
               'openai_excel_bps_models', 'openai_excel_bps_required_group_ids',
               'openai_excel_bps_required_models',
                'openai_excel_bps_auto_disable_on_403', 'openai_excel_bps_auto_move_on_403')
QUOTA_HEADERS = tuple('x-codex-' + key for key in (
    'primary-used-percent', 'primary-reset-after-seconds', 'primary-window-minutes',
    'secondary-used-percent', 'secondary-reset-after-seconds', 'secondary-window-minutes',
    'primary-over-secondary-limit-percent'))


def response_quota_headers(raw, fallback_at):
    """Keep only the final response's quota headers, never cookies or proxy headers."""
    selected = {}
    response_date = None
    for line in raw.splitlines():
        if line.startswith('HTTP/'):
            selected = {}
            response_date = None
        elif ':' in line:
            name, value = line.split(':', 1)
            name = name.strip().lower()
            if name in QUOTA_HEADERS:
                selected[name] = value.strip()
            elif name == 'date':
                response_date = value.strip()
    if not selected:
        return {}
    observed = fallback_at
    if response_date:
        try:
            date = parsedate_to_datetime(response_date)
            if date.tzinfo is not None:
                observed = date.astimezone(timezone.utc).isoformat()
        except (ValueError, TypeError, OverflowError):
            pass
    return {'quota_headers': selected, 'quota_observed_at': observed}


def utcnow():
    return datetime.now(timezone.utc).isoformat()


def group_allowed_models(account):
    """Normalize the GET representation; empty means no extra restriction."""
    return {str(binding['group_id']): sorted(set(binding['allowed_models']))
            for binding in account.get('account_groups') or [] if binding.get('allowed_models')}


def policy_snapshot(account):
    extra = account.get('extra') or {}
    return {'group_ids': sorted(account.get('group_ids') or []),
            'group_allowed_models': group_allowed_models(account),
            'extra': {key: copy.deepcopy(extra[key]) for key in POLICY_KEYS if key in extra}}


def inventory():
    # Read-only SQL is used solely to obtain tokens/proxies locally, never to
    # mutate account scheduling or classification. No raw inventory is logged.
    query = """
SELECT json_build_object('id',a.id,'type',a.type,'platform',a.platform,
       'credentials',a.credentials,'extra',a.extra,'proxy',row_to_json(p),
       'status',a.status,'schedulable',a.schedulable,'proxy_id',a.proxy_id,
       'rate_limited_at',a.rate_limited_at,'rate_limit_reset_at',a.rate_limit_reset_at,
       'overload_until',a.overload_until,'temp_unschedulable_until',a.temp_unschedulable_until,
       'temp_unschedulable_reason',a.temp_unschedulable_reason,
       'group_ids',(SELECT COALESCE(json_agg(group_id ORDER BY group_id),'[]'::json)
                    FROM account_groups WHERE account_id=a.id),
       'account_groups',(SELECT COALESCE(json_agg(json_build_object('group_id',group_id,
                                  'allowed_models',allowed_models)),'[]'::json)
                         FROM account_groups WHERE account_id=a.id))
FROM accounts a LEFT JOIN proxies p ON p.id=a.proxy_id
WHERE a.platform='openai' AND a.type='oauth' AND a.deleted_at IS NULL ORDER BY a.id
"""
    process = subprocess.run(['sudo', '-n', 'docker', 'exec', 'sub2api-postgres',
        'psql', '-U', 'sub2api', '-d', 'sub2api', '-At', '-v', 'ON_ERROR_STOP=1', '-c', query],
        capture_output=True, text=True, timeout=30, check=False)
    if process.returncode:
        raise RuntimeError('OAuth inventory read failed')
    return [json.loads(row) for row in process.stdout.splitlines()]


def sample_class(sample, requested_model, expected_answer):
    if (sample.get('completed') is not True or sample.get('http_status') != '200'
            or sample.get('curl_exit') != 0 or sample.get('error')
            or sample.get('response_model') != requested_model
            or not str(sample.get('text') or '').strip()):
        return 'inconclusive'
    return 'healthy' if sample['text'].strip() == expected_answer else 'degraded'


def failure_metadata(error, payload, http_status, curl_exit):
    if not isinstance(error, dict):
        try:
            parsed = json.loads(payload.strip())
            error = parsed.get('error', parsed) if isinstance(parsed, dict) else {}
        except (ValueError, TypeError):
            error = {}
    error = error if isinstance(error, dict) else {}
    code = str(error.get('code') or error.get('type') or '')
    code = code if re.fullmatch(r'[a-zA-Z0-9_-]{1,80}', code) else ''
    kind = 'transient'
    if curl_exit == 0 and http_status == '401':
        if code in ('token_revoked', 'token_invalidated', 'invalidated_token', 'refresh_token_revoked'):
            kind = 'credential_revoked'
        elif code in ('token_expired', 'access_token_expired', 'expired_token'):
            kind = 'credential_expired'
        else:
            kind = 'credential_rejected'
    elif curl_exit == 0 and http_status == '429' and code in (
            'usage_limit_reached', 'quota_exceeded', 'insufficient_quota', 'billing_hard_limit_reached'):
        kind = 'quota_exhausted'
    return {'error_code': code, 'failure_kind': kind}


def parse_response(payload, http_status, curl_exit, model, expected_answer):
    parts, completed, response_model, error = [], False, None, None
    for line in payload.splitlines():
        if not line.startswith('data:'):
            continue
        try:
            event = json.loads(line[5:])
        except ValueError:
            continue
        if not isinstance(event, dict):
            continue
        kind = event.get('type')
        if kind == 'response.output_text.delta':
            parts.append(str(event.get('delta') or ''))
        elif kind == 'response.completed':
            final = event.get('response') or {}
            completed = final.get('status', 'completed') == 'completed'
            response_model = final.get('model')
            # Prefer the completed response's authoritative text if provided.
            final_parts = [str(part.get('text') or '') for item in final.get('output') or []
                           for part in item.get('content') or [] if part.get('type') == 'output_text']
            if final_parts:
                parts = final_parts
        elif kind in ('response.failed', 'response.incomplete', 'error'):
            error = event.get('error') or (event.get('response') or {}).get('error') or {'type': kind}
    text = ''.join(parts)
    result = {'completed': completed, 'http_status': http_status, 'curl_exit': curl_exit,
              'response_model': response_model, 'text': text[:12000],
              'text_sha256': hashlib.sha256(text.encode()).hexdigest(), 'text_length': len(text)}
    if not completed or error or curl_exit or http_status != '200':
        result['error'] = legacy.describe_failure(payload, http_status, error, curl_exit)
        result.update(failure_metadata(error, payload, http_status, curl_exit))
    elif response_model != model:
        result['error'] = 'response_model_mismatch'
    elif len(text) > 12000:
        result['error'] = 'response_text_limit'
    elif not text.strip():
        result['error'] = 'empty_response'
    result['class'] = sample_class(result, model, expected_answer)
    return result


def probe(account, model, reasoning, expected_answer):
    started = time.monotonic()
    result = {'id': account['id'], 'model': model, 'protocol': 'native_codex_responses',
              'at': utcnow(), 'class': 'inconclusive', 'completed': False}
    credentials = account.get('credentials') or {}
    token = credentials.get('access_token')
    if not token:
        return {**result, 'error': 'missing_token', 'seconds': 0}
    body = {'model': model, 'instructions': '',
            'input': [{'role': 'user', 'content': [{'type': 'input_text', 'text': PROMPT}]}],
            'stream': True, 'store': False, 'tools': [], 'reasoning': {'effort': reasoning}}
    # Secrets travel on curl stdin, not argv; redirects and automatic retries
    # are intentionally disabled. This direct URL cannot invoke Sub2API BPS.
    config = ['url = "https://chatgpt.com/backend-api/codex/responses"',
              'request = "POST"', 'max-time = 100', 'silent', 'show-error',
              'header = "Content-Type: application/json"',
              'header = ' + json.dumps('Authorization: Bearer ' + token),
              'data = ' + json.dumps(json.dumps(body, ensure_ascii=False))]
    if credentials.get('chatgpt_account_id'):
        config.append('header = ' + json.dumps('ChatGPT-Account-Id: ' + credentials['chatgpt_account_id']))
    proxy = account.get('proxy')
    if proxy:
        config.append('proxy = ' + json.dumps(str(proxy.get('protocol') or 'http') + '://'
                                             + proxy['host'] + ':' + str(proxy['port'])))
        if proxy.get('username'):
            config.append('proxy-user = ' + json.dumps(proxy['username'] + ':' + (proxy.get('password') or '')))
    try:
        with tempfile.TemporaryDirectory(prefix='candy-headers-') as folder:
            header_path = Path(folder) / 'response.headers'
            response = subprocess.run(['curl', '--config', '-', '--dump-header', str(header_path),
                '--write-out', '\n__HTTP_STATUS__:%{http_code}'],
                input='\n'.join(config), capture_output=True, text=True, timeout=110, check=False)
            if header_path.exists():
                result.update(response_quota_headers(header_path.read_text(errors='replace'), result['at']))
        payload, marker, status = response.stdout.rpartition('\n__HTTP_STATUS__:')
        result.update(parse_response(payload if marker else response.stdout, status.strip() if marker else '',
                                     response.returncode, model, expected_answer))
    except subprocess.TimeoutExpired:
        result['error'] = 'timeout'
    except OSError:
        result['error'] = 'probe_process_failed'
    result['seconds'] = round(time.monotonic() - started, 2)
    return result


def current_state(account, groups):
    memberships = account.get('group_ids') or []
    if groups['bad'] in memberships:
        return 'degraded'
    if groups['good'] in memberships:
        return 'healthy'
    return None


def has_candy_classification(account, model, reasoning, expected_answer):
    prior = (account.get('extra') or {}).get('quality_candy') or {}
    return (prior.get('version') == 1 and prior.get('algorithm') == ALGORITHM
            and prior.get('prompt_sha256') == PROMPT_SHA256
            and prior.get('model') == model and prior.get('reasoning_effort') == reasoning
            and prior.get('expected_answer') == expected_answer and prior.get('total') == 4
            and isinstance(prior.get('correct'), int) and 0 <= prior['correct'] <= 4
            and prior.get('state') == ('healthy' if prior['correct'] >= 3 else 'degraded'))


def assess_account(account, groups, *, initial, model, reasoning, expected_answer,
                   run_id, probe_fn=probe, on_sample=None):
    previous = current_state(account, groups)
    result = {'id': account['id'], 'previous_state': previous,
              'baseline_policy': policy_snapshot(account),
              'baseline_recovery': recovery_snapshot(account), 'probe_started_at': utcnow(),
              'samples': [], 'run_id': run_id,
              'model': model, 'reasoning_effort': reasoning, 'expected_answer': expected_answer}
    bootstrap = previous is None or not has_candy_classification(account, model, reasoning, expected_answer)
    result['sampling_mode'] = 'initial' if initial else 'bootstrap' if bootstrap else 'periodic'
    target = 4 if initial or bootstrap else 1
    while len(result['samples']) < target:
        sample = probe_fn(account, model, reasoning, expected_answer)
        sample['number'] = len(result['samples']) + 1
        sample['class'] = sample_class(sample, model, expected_answer)
        result['samples'].append(sample)
        if on_sample:
            on_sample(result)
        if target == 1 and sample['class'] in ('healthy', 'degraded') and sample['class'] != previous:
            target = 4
    valid = all(s['class'] in ('healthy', 'degraded') for s in result['samples'])
    result.update(total=len(result['samples']), correct=sum(s['class'] == 'healthy' for s in result['samples']),
                  checked_at=utcnow(), state='inconclusive', action='preserve')
    if valid and result['total'] == 4:
        result['state'] = 'healthy' if result['correct'] >= 3 else 'degraded'
        result['action'] = 'reconcile'
    elif valid:
        result['state'] = result['samples'][0]['class']
        result['action'] = 'unchanged'
    return result


def reconcile(account, result, groups, template):
    if result.get('action') != 'reconcile' or result.get('state') not in ('healthy', 'degraded'):
        return {}
    extra = copy.deepcopy(account.get('extra') or {})
    degraded = result['state'] == 'degraded'
    extra['quality_candy'] = {key: result[key] for key in
        ('state', 'model', 'reasoning_effort', 'expected_answer', 'correct', 'total', 'checked_at', 'run_id')}
    extra['quality_candy'].update(version=1, algorithm=ALGORITHM, prompt_sha256=PROMPT_SHA256)
    # Supersede the old per-model heuristic so the template writer cannot
    # remove supported models based on historical knowledge-cutoff responses.
    extra.pop('quality_model_results', None)
    extra['openai_excel_bps'] = degraded
    extra['openai_excel_bps_models'] = list(BPS_MODELS)
    extra['openai_excel_bps_required_group_ids'] = [groups['bad']] if degraded else []
    extra['openai_excel_bps_required_models'] = list(BPS_MODELS) if degraded else []
    extra['openai_excel_bps_auto_disable_on_403'] = False
    extra['openai_excel_bps_auto_move_on_403'] = False
    memberships = [group for group in account.get('group_ids') or [] if group != groups['bad']]
    if groups['good'] not in memberships:
        memberships.append(groups['good'])
    if degraded:
        memberships.append(groups['bad'])
    patch = {}
    if extra != (account.get('extra') or {}):
        patch['extra'] = extra
    if sorted(memberships) != sorted(account.get('group_ids') or []):
        patch['group_ids'] = memberships
    # Only restore the two models removed by the retired quality heuristic;
    # plan templates are the authority for whether a model is supported.
    credentials = account.get('credentials') or {}
    mapping = dict(credentials.get('model_mapping') or {})
    desired = dict(mapping)
    for model in BPS_MODELS:
        if model in template.get('models', ()):
            desired[model] = (template.get('model_aliases') or {}).get(model, model)
    if mapping != desired:
        patch['credentials'] = {**credentials, 'model_mapping': desired}
    # Persist the existing account x group whitelist as visible audit evidence
    # alongside the fail-closed runtime guard. The bad group is unrestricted
    # within this account's supported models; every other group omits Sol/Astra.
    native_models = sorted(model for model in desired if model not in BPS_MODELS)
    allowed = ({str(group): native_models for group in memberships if group != groups['bad']}
               if degraded and native_models else {})
    if allowed != group_allowed_models(account):
        patch['group_allowed_models'] = allowed
    return patch


def validate_record(summary, max_age_minutes=180):
    if summary.get('algorithm') != ALGORITHM or summary.get('prompt_sha256') != PROMPT_SHA256:
        raise ValueError('unsupported candy evidence version or prompt')
    if not re.fullmatch(r'[A-Za-z0-9-]{1,80}', summary.get('run_id', '')):
        raise ValueError('invalid candy run identifier')
    age = (datetime.now(timezone.utc) - datetime.fromisoformat(summary['at'])).total_seconds()
    if age < -60 or age > max_age_minutes * 60:
        raise ValueError('candy evidence is outside the allowed age window')
    seen = set()
    for result in summary['results']:
        if result['id'] in seen:
            raise ValueError('duplicate account evidence')
        seen.add(result['id'])
        if any(result.get(key) != summary.get(key) for key in ('model', 'reasoning_effort', 'expected_answer', 'run_id')):
            raise ValueError('inconsistent candy evidence metadata')
        samples = result['samples']
        if len(samples) not in (1, 4) or result['total'] != len(samples):
            raise ValueError('invalid sequential sample count')
        if summary.get('initial') and len(samples) != 4:
            raise ValueError('initial classification requires four attempts')
        if [sample.get('number') for sample in samples] != list(range(1, len(samples) + 1)):
            raise ValueError('samples are not in sequential order')
        classes = [sample_class(s, summary['model'], summary['expected_answer']) for s in samples]
        if any(s.get('class') != cls for s, cls in zip(samples, classes)):
            raise ValueError('sample classification does not match evidence')
        mode = result.get('sampling_mode')
        if mode in ('initial', 'bootstrap') and len(samples) != 4:
            raise ValueError('unclassified account requires four attempts')
        if mode == 'periodic':
            first = classes[0]
            mismatch = first in ('healthy', 'degraded') and first != result['previous_state']
            if len(samples) != (4 if mismatch else 1):
                raise ValueError('periodic sample count does not follow the one-plus-three rule')
        correct = classes.count('healthy')
        state = ('inconclusive' if 'inconclusive' in classes else
                 ('healthy' if correct >= 3 else 'degraded') if len(samples) == 4 else classes[0])
        action = 'preserve' if state == 'inconclusive' else 'reconcile' if len(samples) == 4 else 'unchanged'
        if result['state'] != state or result['correct'] != correct or result['action'] != action:
            raise ValueError('account classification does not match evidence')


def apply_one(result, key, groups, templates, backup_dir):
    if result.get('action') != 'reconcile':
        return {'id': result['id'], 'change': [], 'skipped': result['action']}
    account_id = result['id']
    current = legacy.admin('GET', f'/admin/accounts/{account_id}', key)
    if current.get('platform') != 'openai' or current.get('type') != 'oauth':
        return {'id': account_id, 'change': [], 'skipped': 'not_openai_oauth'}
    plan = (current.get('credentials') or {}).get('plan_type')
    template = templates.get('prolite' if plan == 'self_serve_business_prolite' else plan, {})
    patch = reconcile(current, result, groups, template)
    # A replay after a partially applied batch is safe when the full desired
    # policy already reads back; retain the original rollback snapshot.
    if not patch:
        return {'id': account_id, 'change': [], 'skipped': 'already_applied'}
    # Never overwrite a policy edit made while the probes/build were running.
    live_policy = policy_snapshot(current)
    baseline = result['baseline_policy']
    same_policy = all(live_policy.get(field) == value for field, value in baseline.items())
    # A previous attempt can have safely removed the bad-group membership
    # before its second PUT failed. Only that exact intermediate state may
    # resume against the original evidence and rollback snapshot.
    transition = copy.deepcopy(baseline)
    transition['group_ids'] = sorted(set(baseline['group_ids']) - {groups['bad']} | {groups['good']})
    if 'group_allowed_models' in transition:
        transition['group_allowed_models'].pop(str(groups['bad']), None)
    safe_resume = (result['state'] == 'healthy' and groups['bad'] in baseline['group_ids']
                   and all(live_policy.get(field) == value for field, value in transition.items()))
    if not same_policy and not safe_resume:
        return {'id': account_id, 'change': [], 'skipped': 'concurrent_policy_change'}
    # The rollback snapshot can contain credentials; it remains owner-only.
    backup = backup_dir / f'account-{account_id}.before.json'
    if not backup.exists():
        legacy.save_private(backup, {'id': account_id, 'before': {
            field: group_allowed_models(current) if field == 'group_allowed_models' else current.get(field)
            for field in patch}})
    if result['state'] == 'healthy' and groups['bad'] in current.get('group_ids', []):
        target_groups = patch['group_ids']
        legacy.admin('PUT', f'/admin/accounts/{account_id}', key, {'group_ids': target_groups})
        unbound = legacy.admin('GET', f'/admin/accounts/{account_id}', key)
        if sorted(unbound.get('group_ids') or []) != sorted(target_groups):
            raise RuntimeError(f'account {account_id} bad-group removal readback mismatch')
        unbound_policy = policy_snapshot(unbound)
        if any(unbound_policy.get(field) != value for field, value in transition.items()):
            raise RuntimeError(f'account {account_id} policy changed after bad-group removal')
        # BindGroups is not transactional with extra updates. Removing group16
        # before disabling BPS prevents a transient or persistent native leak.
        remaining_patch = reconcile(unbound, result, groups, template)
        if remaining_patch:
            legacy.admin('PUT', f'/admin/accounts/{account_id}', key, remaining_patch)
    else:
        legacy.admin('PUT', f'/admin/accounts/{account_id}', key, patch)
    after = legacy.admin('GET', f'/admin/accounts/{account_id}', key)
    for field, expected in patch.items():
        actual = group_allowed_models(after) if field == 'group_allowed_models' else after.get(field)
        if field == 'extra':
            # Backend merges live runtime cooldown/ticket fields. Check this
            # operation's owned policy including intentional key deletions,
            # not transient business state that can legitimately advance.
            actual = {key: (actual or {})[key] for key in POLICY_KEYS if key in (actual or {})}
            expected = {key: expected[key] for key in POLICY_KEYS if key in expected}
        if (sorted(actual or []) != sorted(expected)) if field == 'group_ids' else (actual != expected):
            raise RuntimeError(f'account {account_id} {field} readback mismatch')
    for field in ('status', 'schedulable', 'proxy_id', 'concurrency', 'load_factor', 'priority'):
        if after.get(field) != current.get(field):
            raise RuntimeError(f'account {account_id} unexpected {field} change')
    return {'id': account_id, 'change': list(patch), 'state': result['state'],
            'correct': result['correct'], 'total': result['total']}


RECOVERY_MAX_AGE_SECONDS = 600
RECOVERY_GATE_FIELDS = ('status', 'schedulable', 'proxy_id', 'rate_limited_at',
                        'rate_limit_reset_at', 'overload_until',
                        'temp_unschedulable_until', 'temp_unschedulable_reason')


def timestamp(value):
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
        return parsed if parsed.tzinfo is not None else None
    except ValueError:
        return None


def recovery_snapshot(account):
    credentials = account.get('credentials') or {}
    identity = {key: credentials.get(key) for key in ('access_token', 'chatgpt_account_id')}
    fields = {key: account.get(key) for key in RECOVERY_GATE_FIELDS}
    fields['temp_unschedulable_reason'] = fields['temp_unschedulable_reason'] or None
    for key in ('rate_limited_at', 'rate_limit_reset_at', 'overload_until', 'temp_unschedulable_until'):
        parsed = timestamp(fields[key])
        if parsed is not None:
            fields[key] = parsed.astimezone(timezone.utc).isoformat(timespec='microseconds')
    return {'version': 1, **fields,
            'credential_sha256': hashlib.sha256(json.dumps(identity, sort_keys=True).encode()).hexdigest(),
            'access_token_sha256': hashlib.sha256(str(credentials.get('access_token') or '').encode()).hexdigest()}


def recovery_matches(account, baseline):
    actual = recovery_snapshot(account)
    # Admin GET can omit OAuth secrets. The native conditional API checks the
    # captured token SHA256 atomically; never treat a redacted token as a change.
    keys = set(baseline)
    if not (account.get('credentials') or {}).get('access_token'):
        keys -= {'credential_sha256', 'access_token_sha256'}
    return all(actual.get(key) == baseline[key] for key in keys)


def recover_one(result, key, backup_dir):
    """Clear only a proven stale native cooldown; never enable a paused account."""
    account_id = result['id']
    skipped = lambda reason: {'id': account_id, 'recovered': False, 'skipped': reason}
    baseline = result.get('baseline_recovery')
    if not isinstance(baseline, dict) or baseline.get('version') != 1:
        return skipped('no_recovery_baseline')
    if baseline.get('status') != 'active' or baseline.get('schedulable') is not True:
        return skipped('paused_or_inactive_before_probe')
    now = datetime.now(timezone.utc)
    reset = timestamp(baseline.get('rate_limit_reset_at'))
    if reset is None or reset <= now:
        return skipped('no_future_native_cooldown')
    samples = result.get('samples') or []
    if (len(samples) not in (1, 4) or result.get('total') != len(samples)
            or any(sample_class(sample, result['model'], result['expected_answer'])
                   not in ('healthy', 'degraded') for sample in samples)):
        return skipped('incomplete_probe_set')
    checked = timestamp(result.get('checked_at'))
    started = timestamp(result.get('probe_started_at'))
    if (checked is None or started is None or started > checked
            or not 0 <= (now - checked).total_seconds() <= RECOVERY_MAX_AGE_SECONDS):
        return skipped('stale_or_invalid_recovery_evidence')
    limited = timestamp(baseline.get('rate_limited_at'))
    if limited is None or limited > started:
        return skipped('cooldown_not_proven_before_probe')
    current = legacy.admin('GET', f'/admin/accounts/{account_id}', key)
    if current.get('platform') != 'openai' or current.get('type') != 'oauth':
        return skipped('not_openai_oauth')
    if not recovery_matches(current, baseline):
        return skipped('concurrent_gate_or_identity_change')
    if current.get('status') != 'active' or current.get('schedulable') is not True:
        return skipped('paused_or_inactive_now')
    # A native probe proves nothing about independently imposed restrictions.
    for field in ('overload_until', 'temp_unschedulable_until'):
        until = timestamp(current.get(field))
        if current.get(field) and (until is None or until > now):
            return skipped('other_restriction_present')
    legacy.save_private(backup_dir / f'account-{account_id}.recovery-before.json', current)
    fresh = legacy.admin('GET', f'/admin/accounts/{account_id}', key)
    if not recovery_matches(fresh, baseline):
        return skipped('concurrent_gate_or_identity_change')
    outcome = legacy.admin('POST', f'/admin/accounts/{account_id}/clear-native-rate-limit', key,
        {'observed_rate_limited_at': baseline['rate_limited_at'],
         'observed_rate_limit_reset_at': baseline['rate_limit_reset_at'],
         'observed_proxy_id': baseline['proxy_id'],
         'observed_access_token_sha256': baseline['access_token_sha256']})
    if outcome.get('id') != account_id:
        raise RuntimeError(f'account {account_id} unexpected recovery response identity')
    if outcome.get('cleared') is not True:
        return skipped('conditional_clear_not_applied')
    after = legacy.admin('GET', f'/admin/accounts/{account_id}', key)
    if after.get('rate_limit_reset_at') or after.get('rate_limited_at'):
        if any(recovery_snapshot(after)[field] != baseline[field]
               for field in ('rate_limited_at', 'rate_limit_reset_at')):
            return {'id': account_id, 'recovered': False, 'clear_applied': True,
                    'skipped': 'new_cooldown_after_clear'}
        raise RuntimeError(f'account {account_id} cooldown recovery readback mismatch')
    for field in ('status', 'schedulable', 'proxy_id', 'concurrency', 'load_factor', 'priority'):
        if after.get(field) != fresh.get(field):
            raise RuntimeError(f'account {account_id} unexpected recovery {field} change')
    for field in ('overload_until', 'temp_unschedulable_until', 'temp_unschedulable_reason'):
        if after.get(field) != fresh.get(field):
            raise RuntimeError(f'account {account_id} unexpected recovery {field} change')
    for field in ('model_rate_limits', 'antigravity_quota_scopes', 'openai_excel_bps_rate_limit_reset_at'):
        if (after.get('extra') or {}).get(field) != (fresh.get('extra') or {}).get(field):
            raise RuntimeError(f'account {account_id} unexpected recovery independent limit change')
    if policy_snapshot(after) != policy_snapshot(fresh):
        raise RuntimeError(f'account {account_id} unexpected recovery policy change')
    return {'id': account_id, 'recovered': True, 'verified_at': utcnow(),
            'previous_rate_limit_reset_at': baseline['rate_limit_reset_at']}


def persist_quota_one(result, key, backup_dir):
    """Reuse native response headers without making another upstream request."""
    account_id = result['id']
    skipped = lambda reason: {'id': account_id, 'updated': False, 'skipped': reason}
    baseline = result.get('baseline_recovery')
    if not isinstance(baseline, dict) or not baseline.get('access_token_sha256'):
        return skipped('no_identity_baseline')
    samples = [sample for sample in result.get('samples') or []
               if sample.get('quota_headers') and sample.get('curl_exit') == 0
               and sample.get('http_status') in ('200', '429')
               and timestamp(sample.get('quota_observed_at')) is not None]
    if not samples:
        return skipped('no_quota_response_headers')
    sample = max(samples, key=lambda item: timestamp(item['quota_observed_at']))
    observed = timestamp(sample['quota_observed_at'])
    if not 0 <= (datetime.now(timezone.utc) - observed).total_seconds() <= RECOVERY_MAX_AGE_SECONDS:
        return skipped('stale_quota_response_headers')
    current = legacy.admin('GET', f'/admin/accounts/{account_id}', key)
    if current.get('platform') != 'openai' or current.get('type') != 'oauth':
        return skipped('not_openai_oauth')
    legacy.save_private(backup_dir / f'account-{account_id}.quota-before.json', current)
    outcome = legacy.admin('POST', f'/admin/accounts/{account_id}/codex-usage-snapshot', key,
        {'observed_at': observed.isoformat(), 'headers': sample['quota_headers'],
         'observed_proxy_id': baseline['proxy_id'],
         'observed_access_token_sha256': baseline['access_token_sha256']})
    if outcome.get('id') != account_id:
        raise RuntimeError(f'account {account_id} unexpected quota response identity')
    if outcome.get('updated') is not True:
        return skipped('newer_snapshot_or_identity_changed')
    after = legacy.admin('GET', f'/admin/accounts/{account_id}', key)
    expected = outcome.get('quota') or {}
    if not expected or not expected.get('codex_usage_updated_at'):
        raise RuntimeError(f'account {account_id} quota writeback missing evidence')
    actual = after.get('extra') or {}
    if any(actual.get(field) != value for field, value in expected.items()):
        current_at = timestamp(actual.get('codex_usage_updated_at'))
        expected_at = timestamp(expected['codex_usage_updated_at'])
        if current_at is None or expected_at is None or current_at <= expected_at:
            raise RuntimeError(f'account {account_id} quota writeback readback mismatch')
        return {'id': account_id, 'updated': True, 'verified_at': utcnow(),
                'observed_at': observed.isoformat(), 'readback': 'newer_snapshot', 'quota': expected}
    return {'id': account_id, 'updated': True, 'verified_at': utcnow(),
            'observed_at': observed.isoformat(), 'readback': 'matched', 'quota': expected}


def compact(summary):
    results = summary['results']
    return {'run_id': summary['run_id'], 'accounts': len(results),
            'probes': sum(r['total'] for r in results),
            **{state: sum(r['state'] == state for r in results) for state in ('healthy', 'degraded', 'inconclusive')},
            'changed': sum(bool(c.get('change')) for c in summary['changes']),
            'apply_errors': [c['id'] for c in summary['changes'] if c.get('error')],
            'skipped_concurrent': [c['id'] for c in summary['changes'] if c.get('skipped') == 'concurrent_policy_change'],
            'recovered': sum(bool(r.get('recovered')) for r in summary.get('recoveries', [])),
            'recovery_errors': [r['id'] for r in summary.get('recoveries', []) if r.get('error')],
            'recovery_skipped': {reason: sum(r.get('skipped') == reason for r in summary.get('recoveries', []))
                                 for reason in sorted({r['skipped'] for r in summary.get('recoveries', []) if r.get('skipped')})},
            'quota_updated': sum(bool(q.get('updated')) for q in summary.get('quota_updates', [])),
            'quota_errors': [q['id'] for q in summary.get('quota_updates', []) if q.get('error')],
            'quota_skipped': {reason: sum(q.get('skipped') == reason for q in summary.get('quota_updates', []))
                             for reason in sorted({q['skipped'] for q in summary.get('quota_updates', []) if q.get('skipped')})}}


def run(*, state_dir=legacy.STATE_DIR, initial=False, apply=False, account_ids=(),
        model=DEFAULT_MODEL, reasoning=DEFAULT_REASONING, expected_answer=DEFAULT_ANSWER,
        apply_record=None, max_age_minutes=180, workers=4, route_mode="bps", pending_only=False):
    if route_mode == "borrow":
        import sys
        borrow_spec = importlib.util.spec_from_file_location(
            'oauth_gateway_borrow', Path(__file__).with_name('oauth_gateway_borrow.py'))
        borrow = importlib.util.module_from_spec(borrow_spec)
        sys.modules[borrow_spec.name] = borrow
        borrow.candy = sys.modules[__name__]
        borrow_spec.loader.exec_module(borrow)
        return borrow.run_borrow(state_dir=state_dir, initial=initial, apply=apply, account_ids=account_ids,
                          reasoning=reasoning, expected_answer=expected_answer,
                          apply_record=apply_record, max_age_minutes=max_age_minutes, workers=workers,
                          pending_only=pending_only)
    if pending_only:
        raise ValueError('pending-only requires borrow route mode')
    if route_mode != "bps":
        raise ValueError("unsupported route mode")
    if workers not in range(1, 5):
        raise ValueError('account concurrency must be between 1 and 4')
    if not re.fullmatch(r'\d+', expected_answer):
        raise ValueError('expected answer must be a nonnegative integer string')
    os.umask(0o077)
    state_dir = Path(state_dir)
    state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (state_dir / 'cycle.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        key = (state_dir / 'admin-key').read_text().strip()
        groups = legacy.quality_group_ids(legacy.admin('GET', '/admin/groups?page=1&page_size=100', key)['items'])
        if apply_record:
            summary = json.loads(Path(apply_record).read_text())
            validate_record(summary, max_age_minutes)
            if account_ids:
                summary['results'] = [r for r in summary['results'] if r['id'] in account_ids]
            if summary['quality_group_ids'] != groups:
                raise ValueError('quality group identity changed since probing')
            run_dir = state_dir / 'candy-runs' / summary['run_id']
        else:
            accounts = inventory()
            if account_ids:
                accounts = [a for a in accounts if a['id'] in account_ids]
            if not accounts:
                raise ValueError('empty OAuth inventory')
            run_id = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8]
            run_dir = state_dir / 'candy-runs' / run_id
            run_dir.mkdir(mode=0o700, parents=True, exist_ok=False)
            summary = {'at': utcnow(), 'run_id': run_id, 'algorithm': ALGORITHM,
                       'prompt_sha256': PROMPT_SHA256, 'expected_answer': expected_answer,
                       'model': model, 'reasoning_effort': reasoning, 'initial': initial,
                       'quality_group_ids': groups, 'results': [], 'changes': [], 'applied': False}
            legacy.save_private(run_dir / 'manifest.json', summary)
            def task(account):
                path = run_dir / f"account-{account['id']}.json"
                result = assess_account(account, groups, initial=initial, model=model, reasoning=reasoning,
                    expected_answer=expected_answer, run_id=run_id,
                    on_sample=lambda partial: legacy.save_private(path, partial))
                legacy.save_private(path, result)
                return result
            with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
                jobs = [pool.submit(task, account) for account in accounts]
                summary['results'] = [job.result() for job in jobs]
            summary['finished_at'] = utcnow()
            summary['accounts'] = len(summary['results'])
            summary['probes'] = sum(r['total'] for r in summary['results'])
            legacy.save_private(run_dir / 'result.json', summary)
        run_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        summary['changes'] = []
        summary['recoveries'] = []
        summary['quota_updates'] = []
        summary['applied'] = apply
        if apply:
            validate_record(summary, max_age_minutes)
            templates = json.loads((state_dir / 'account-defaults.json').read_text())['templates']
            for result in summary['results']:
                try:
                    summary['changes'].append(apply_one(result, key, groups, templates, run_dir))
                except Exception as exc:
                    # Exceptions can carry response bodies/credentials; only
                    # their type is persisted or printed in this outer guard.
                    safe = str(exc) if isinstance(exc, RuntimeError) and str(exc).startswith(
                        f"account {result['id']} ") else type(exc).__name__
                    summary['changes'].append({'id': result['id'], 'error': safe[:160]})
                legacy.save_private(run_dir / 'applied.json', summary)
        if apply and not apply_record:
            for result in summary['results']:
                try:
                    quota = persist_quota_one(result, key, run_dir)
                except Exception as exc:
                    safe = str(exc) if isinstance(exc, RuntimeError) and str(exc).startswith(
                        f"account {result['id']} ") else type(exc).__name__
                    quota = {'id': result['id'], 'updated': False, 'error': safe[:160]}
                summary['quota_updates'].append(quota)
                legacy.save_private(run_dir / 'applied.json', summary)
            changes = {change['id']: change for change in summary['changes']}
            for result in summary['results']:
                change = changes[result['id']]
                if change.get('error') or change.get('skipped') == 'concurrent_policy_change':
                    recovery = {'id': result['id'], 'recovered': False, 'skipped': 'quality_apply_not_verified'}
                else:
                    try:
                        recovery = recover_one(result, key, run_dir)
                    except Exception as exc:
                        safe = str(exc) if isinstance(exc, RuntimeError) and str(exc).startswith(
                            f"account {result['id']} ") else type(exc).__name__
                        recovery = {'id': result['id'], 'recovered': False, 'error': safe[:160]}
                summary['recoveries'].append(recovery)
                legacy.save_private(run_dir / 'applied.json', summary)
        summary['completed_at'] = utcnow()
        legacy.save_private(run_dir / ('applied.json' if apply else 'result.json'), summary)
        # Compatibility with the existing timer health collector. Probe errors
        # are explicit coverage results, not account mutations or timer faults.
        legacy.save_private(state_dir / 'last-run.json', summary)
        history_path = state_dir / 'candy-history.json'
        history = json.loads(history_path.read_text()) if history_path.exists() else []
        history.append({'at': summary['at'], **compact(summary)})
        legacy.save_private(history_path, history[-96:])
        output = {**compact(summary), 'record': str(run_dir / 'result.json')}
        print(json.dumps(output, sort_keys=True), flush=True)
        if output['apply_errors'] or output['skipped_concurrent'] or output['recovery_errors'] or output['quota_errors']:
            raise RuntimeError('one or more candy policy or recovery operations need reconciliation')
        return summary


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--route-mode', choices=['bps', 'borrow'], default='bps')
    parser.add_argument('--pending-only', action='store_true')
    parser.add_argument('--initial', action='store_true')
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--apply-record', type=Path)
    parser.add_argument('--state-dir', type=Path, default=legacy.STATE_DIR)
    parser.add_argument('--account-id', action='append', type=int, default=[])
    parser.add_argument('--model', default=DEFAULT_MODEL)
    parser.add_argument('--reasoning', choices=['low', 'medium', 'high', 'xhigh'], default=DEFAULT_REASONING)
    parser.add_argument('--expected-answer', default=DEFAULT_ANSWER)
    parser.add_argument('--max-age-minutes', type=int, default=180)
    parser.add_argument('--workers', type=int, default=4)
    options = vars(parser.parse_args())
    options['account_ids'] = set(options.pop('account_id'))
    run(**options)
