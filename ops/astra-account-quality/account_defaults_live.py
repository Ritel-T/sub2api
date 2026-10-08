"""Apply defaults once to newly discovered OpenAI OAuth accounts."""
import argparse
from datetime import datetime, timezone
import fcntl
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import subprocess
import urllib.parse
import time

ROOT = Path('/home/ritel/openclaw-stack/shared/do-sfo-monitor')
CLI = '/home/ritel/openclaw-stack/bin/sub2api-adminctl'
PLAN_ALIASES = {
    'self_serve_business_prolite': 'prolite',
}
INITIAL_QUALITY_MODE = 'astra_controls_sol_v2'
INITIAL_POLICY_KEYS = (
    'openai_gateway_borrow_models', 'quality_candy_models', 'quality_candy',
    'openai_excel_bps', 'openai_excel_bps_required_group_ids',
    'openai_excel_bps_required_models', 'openai_gateway_borrow_quality_mode',
    'openai_gateway_borrow_quality_pending', 'openai_gateway_borrow_initial_ready',
    'openai_gateway_borrow_initial_ready_fingerprint',
)

def normalize_plan(plan):
    return PLAN_ALIASES.get(plan, plan) if isinstance(plan, str) else plan

def api_once(method, path, body=None):
    env = dict(os.environ)
    env.pop('SUB2API_ADMIN_QUERY_JSON', None)
    env.pop('SUB2API_ADMIN_BODY_JSON', None)
    parsed = urllib.parse.urlsplit(path)
    if parsed.query:
        env['SUB2API_ADMIN_QUERY_JSON'] = json.dumps(dict(urllib.parse.parse_qsl(parsed.query)))
        path = parsed.path
    if body is not None:
        env['SUB2API_ADMIN_BODY_JSON'] = json.dumps(body)
    p = subprocess.run([CLI, 'raw-request', '--method', method, '--path', path], env=env, capture_output=True, text=True, timeout=90)
    try:
        value = json.loads(p.stdout)
    except ValueError:
        raise RuntimeError('Invalid API response: ' + path) from None
    if p.returncode or value.get('ok') is not True:
        raise RuntimeError('API failed: ' + path)
    return value['data']

def api(method, path, body=None):
    # Only retry reads; a failed write response may already have committed.
    attempts = 3 if method == 'GET' else 1
    for attempt in range(attempts):
        try:
            return api_once(method, path, body)
        except (RuntimeError, subprocess.TimeoutExpired):
            if attempt + 1 == attempts:
                raise
            time.sleep(2 * (attempt + 1))

def listing(path):
    result = []
    page = 1
    while True:
        data = api('GET', f'{path}?page={page}&page_size=100')
        result.extend(data['items'])
        if len(result) >= data['total']:
            return result
        if not data['items']:
            raise RuntimeError('Incomplete pagination')
        page += 1

def save(path, value):
    temp = path.with_suffix('.tmp')
    with temp.open('w') as f:
        json.dump(value, f, indent=2)
        f.flush()
        os.fsync(f.fileno())
    temp.replace(path)

def desired_model_mapping(template, exclusions):
    aliases = template.get('model_aliases', {})
    return {model: aliases.get(model, model) for model in template['models'] if model not in exclusions}

def bps_routes_model(extra, model):
    if not isinstance(extra, dict) or extra.get('openai_excel_bps') is not True:
        return False
    if 'openai_excel_bps_models' not in extra:
        return True
    models = extra['openai_excel_bps_models']
    return isinstance(models, list) and model in models

def model_exclusions_for(config, account_id, extra=None):
    exclusions = set(config.get('model_exclusions', {}).get(str(account_id), []))
    extra = extra or {}
    candy = extra.get('quality_candy') or {}
    candy_classified = (isinstance(candy, dict) and candy.get('version') == 1
                        and candy.get('state') in ('healthy', 'degraded'))
    results = extra.get('quality_model_results') or {}
    if not isinstance(results, dict):
        results = {}
    for model in ('gpt-6-sol', 'gpt-6.1-sol', 'gpt-6-astra'):
        # Candy classification restores the account's supported models globally.
        # Native/BPS availability is enforced separately for each group; old
        # knowledge-cutoff exclusions must not remove the BPS group entry.
        if candy_classified:
            exclusions.discard(model)
            continue
        if bps_routes_model(extra, model):
            exclusions.discard(model)
            continue
        status = results.get(model)
        if status == 'degraded':
            exclusions.add(model)
        elif status == 'good':
            exclusions.discard(model)
    return exclusions

def patch_for(account, template, group_ids, exclusions=()):
    credentials = dict(account['credentials'])
    credentials['model_mapping'] = desired_model_mapping(template, exclusions)
    patch = {'credentials': credentials, 'group_ids': group_ids}
    for key in ('concurrency', 'load_factor', 'priority'):
        if key in template:
            patch[key] = template[key]
    return patch

def new_account_group_ids(config, groups, existing_group_ids):
    legacy = sorted(g['id'] for g in groups
                    if g['platform'] == 'openai'
                    and g['id'] in (2,3,4,5,6,7,8,9,10,12,14))
    assert legacy
    # Only the new-account path reads these settings. Completed accounts retain
    # their binding order and priorities during model-only reconciliation.
    if config.get('native_borrow_enabled') is not True:
        return sorted(set(legacy) | set(existing_group_ids))
    additional = config.get('native_borrow_group_ids')
    if (not isinstance(additional, list) or not additional
            or any(type(group_id) is not int or group_id <= 0 for group_id in additional)
            or len(set(additional)) != len(additional)):
        raise RuntimeError('Invalid native_borrow_group_ids')
    live_groups = {group['id']: group for group in groups}
    for group_id in additional:
        group = live_groups.get(group_id)
        if (not group or group.get('platform') != 'openai'
                or group.get('status') != 'active'):
            raise RuntimeError('Native borrow group missing, inactive or not OpenAI: ' + str(group_id))
    return list(dict.fromkeys(legacy + list(existing_group_ids) + additional))

def dedicated_proxy_country_codes(template):
    if 'dedicated_proxy_country_codes' not in template:
        return {'JP'} if template.get('dedicated_jp_proxy') else set()
    codes = template['dedicated_proxy_country_codes']
    if not isinstance(codes, list) or not codes:
        raise RuntimeError('dedicated_proxy_country_codes must be a nonempty country-code list')
    normalized = set()
    for code in codes:
        if not isinstance(code, str):
            raise RuntimeError('Invalid dedicated proxy country code')
        code = code.strip().upper()
        if len(code) != 2 or not code.isascii() or not code.isalpha():
            raise RuntimeError('Invalid dedicated proxy country code')
        normalized.add(code)
    return normalized

def tested_proxy_exit(result):
    if result.get('success') is not True or not isinstance(result.get('ip_address'), str):
        return None
    try:
        address = ipaddress.ip_address(result['ip_address'])
    except ValueError:
        return None
    return str(address), str(result.get('country_code') or '').strip().upper()

def choose_dedicated_proxy(account, proxies, country_codes, tests):
    fresh = listing('/admin/accounts')
    used = {a.get('proxy_id') for a in fresh
            if a['id'] != account['id'] and a.get('proxy_id') is not None}

    def probe(proxy_id):
        if proxy_id not in tests:
            tests[proxy_id] = api('POST', f'/admin/proxies/{proxy_id}/test')
        return tested_proxy_exit(tests[proxy_id])

    used_ips = set()
    for proxy_id in sorted(used):
        exit_info = probe(proxy_id)
        if exit_info is None:
            raise RuntimeError(f'Cannot confirm occupied proxy exit: {proxy_id}')
        used_ips.add(exit_info[0])
    candidates = sorted(
        [p for p in proxies if p['status'] == 'active' and p['id'] not in used
         and str(p.get('country_code') or '').strip().upper() in country_codes],
        key=lambda p: (p['id'] != account.get('proxy_id'), p['id']))
    for proxy in candidates[:5]:
        try:
            exit_info = probe(proxy['id'])
        except (RuntimeError, subprocess.TimeoutExpired):
            continue
        if exit_info is not None and exit_info[1] in country_codes and exit_info[0] not in used_ips:
            return proxy['id']
    raise RuntimeError('No tested spare proxy in configured countries with a unique exit')

def initial_quality_pending(account):
    extra = account.get('extra') or {}
    mapping = (account.get('credentials') or {}).get('model_mapping')
    return (account.get('platform') == 'openai' and account.get('type') == 'oauth'
            and extra.get('openai_gateway_borrow_quality_mode') == INITIAL_QUALITY_MODE
            and extra.get('openai_gateway_borrow_quality_pending') is True
            and isinstance(mapping, dict) and 'gpt-6-astra' in mapping)

def initial_config(account):
    # This exact projection is also compared atomically by the server. Keep a
    # null mapping as null; it is not equivalent to an explicit empty mapping.
    return {
        'concurrency': account.get('concurrency'),
        'load_factor': account.get('load_factor'),
        'priority': account.get('priority'),
        'group_ids': sorted(set(account.get('group_ids') or [])),
        'model_mapping': (account.get('credentials') or {}).get('model_mapping'),
    }

def initial_observation(account):
    credentials = account.get('credentials') or {}
    identity = {key: credentials.get(key) for key in ('access_token', 'chatgpt_account_id')}
    return {
        'expected_proxy_id': account.get('proxy_id'),
        'credential_sha256': hashlib.sha256(json.dumps(identity, sort_keys=True).encode()).hexdigest(),
        'expected_config': initial_config(account),
    }

def provider_owner_sha256(account):
    owner = (account.get('credentials') or {}).get('chatgpt_account_id')
    if not isinstance(owner, str) or not owner:
        return None
    return hashlib.sha256(json.dumps(owner).encode()).hexdigest()

def initial_ready_record(account):
    record = initial_observation(account)
    record['provider_owner_sha256'] = provider_owner_sha256(account)
    return record

def current_ready_observation(account, record):
    baseline = {key: record[key] for key in ('expected_proxy_id', 'credential_sha256', 'expected_config')}
    current = initial_observation(account)
    if current == baseline:
        return baseline
    credentials = account.get('credentials') or {}
    token = credentials.get('access_token')
    owner_sha = provider_owner_sha256(account)
    if (owner_sha is not None and record.get('provider_owner_sha256') == owner_sha
            and isinstance(token, str) and token
            and current['expected_proxy_id'] == baseline['expected_proxy_id']
            and current['expected_config'] == baseline['expected_config']):
        # A normal access-token refresh on the same provider account is allowed
        # after fresh GET; proxy, model mapping and all defaults must still match.
        return current
    raise RuntimeError('Initial defaults identity or configuration changed')

def coordinate_initial_ready(account, state, state_path):
    record = state['initial_ready_pending'][str(account['id'])]
    observation = current_ready_observation(account, record)
    if record['credential_sha256'] != observation['credential_sha256']:
        record.update(observation)
        save(state_path, state)
    return confirm_initial_ready(account, observation)

def initial_fingerprint(account):
    observation = initial_observation(account)
    value = {'credential_sha256': observation['credential_sha256'],
             'proxy_id': observation['expected_proxy_id'], 'config': observation['expected_config']}
    # Match Go encoding/json.Marshal: sorted map keys, UTF-8 strings, compact
    # separators and HTML/line-separator escaping. Fields here are integers,
    # nulls, a sorted ID list, and string model mappings.
    encoded = json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(',', ':'))
    for char, escaped in (('&', '\\u0026'), ('<', '\\u003c'), ('>', '\\u003e'),
                          ('\u2028', '\\u2028'), ('\u2029', '\\u2029')):
        encoded = encoded.replace(char, escaped)
    return hashlib.sha256(encoded.encode()).hexdigest()

def initial_ready_matches(account):
    extra = account.get('extra') or {}
    return (extra.get('openai_gateway_borrow_initial_ready') is True
            and extra.get('openai_gateway_borrow_initial_ready_fingerprint') == initial_fingerprint(account))

def confirm_initial_ready(account, observation):
    # A saved observation means defaults already passed independent GET
    # verification. Never repeat a configuration PUT while coordinating ready.
    if initial_observation(account) != observation:
        raise RuntimeError('Initial defaults identity or configuration changed')
    extra = account.get('extra') or {}
    if (extra.get('openai_gateway_borrow_quality_mode') == INITIAL_QUALITY_MODE
            and initial_ready_matches(account)):
        return account
    if not initial_quality_pending(account):
        raise RuntimeError('Initial quality policy changed before readiness')
    body = dict(observation)
    body.update(observed_at=datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z'),
                expected_policy={key: extra.get(key) for key in INITIAL_POLICY_KEYS})
    try:
        api('POST', f"/admin/accounts/{account['id']}/gateway-borrow-initial-ready", body)
    except (RuntimeError, subprocess.TimeoutExpired):
        # The response may be unknown after a successful commit. Read first;
        # do not retry the write or repeat the preceding defaults PUT.
        pass
    after = api('GET', f"/admin/accounts/{account['id']}")
    after_extra = after.get('extra') or {}
    if (initial_observation(after) != observation
            or after_extra.get('openai_gateway_borrow_quality_mode') != INITIAL_QUALITY_MODE
            or not initial_ready_matches(after)):
        raise RuntimeError('Initial defaults readiness not confirmed')
    return after

def completed_defaults_verified(account, template, config, groups, proxies, accounts):
    # Completed markers predate ready coordination. Recheck the existing
    # configuration before sending the new marker, without reapplying defaults.
    mapping = desired_model_mapping(template, model_exclusions_for(config, account['id'], account.get('extra')))
    if (account.get('credentials') or {}).get('model_mapping') != mapping:
        return False
    if any(account.get(key) != template[key]
           for key in ('concurrency', 'load_factor', 'priority') if key in template):
        return False
    required = new_account_group_ids(config, groups, [])
    if not set(required).issubset(account.get('group_ids') or []):
        return False
    countries = dedicated_proxy_country_codes(template)
    if countries:
        proxy_id = account.get('proxy_id')
        proxy = next((p for p in proxies if p['id'] == proxy_id), None)
        if (not proxy or proxy.get('status') != 'active'
                or str(proxy.get('country_code') or '').strip().upper() not in countries
                or any(a['id'] != account['id'] and a.get('proxy_id') == proxy_id for a in accounts)):
            return False
    return True

def defaults_patch_matches(account, patch):
    for field, value in patch.items():
        actual = account.get(field)
        if field == 'group_ids':
            if sorted(actual or []) != sorted(value):
                return False
        elif actual != value:
            return False
    return True

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--initialize', action='store_true')
    args = parser.parse_args()
    os.umask(0o077)
    with (ROOT / 'account-defaults.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        accounts = listing('/admin/accounts')
        targets = [a for a in accounts if a['platform']=='openai' and a['type']=='oauth']
        state_path = ROOT / 'account-defaults-state.json'
        config_path = ROOT / 'account-defaults.json'
        if args.initialize:
            if state_path.exists() or config_path.exists():
                raise RuntimeError('Already initialized')
            templates = {}
            for plan in ('pro','prolite','plus','free','go'):
                candidates = [a for a in targets if a['credentials'].get('plan_type')==plan and 5 <= len(a['credentials'].get('model_mapping',{})) <= 10]
                if not candidates:
                    raise RuntimeError('No validated template representative: '+plan)
                representative = candidates[0]['id']
                a = api('GET', f'/admin/accounts/{representative}')
                assert a['credentials'].get('plan_type') == plan
                models = list(a['credentials']['model_mapping'])
                assert models
                templates[plan] = {'models':models, 'dedicated_jp_proxy':plan in ('pro','prolite')}
                if plan in ('pro','prolite'):
                    templates[plan].update(concurrency=5,load_factor=5,priority=4)
                elif plan=='plus':
                    templates[plan].update(concurrency=3,load_factor=3,priority=3)
            save(config_path, {'templates':templates})
            save(state_path, {'completed':[a['id'] for a in targets]})
            print(json.dumps({'initialized':len(targets)}))
            return
        config = json.loads(config_path.read_text())
        state = json.loads(state_path.read_text())
        ready_pending = state.setdefault('initial_ready_pending', {})
        failures = []
        groups = proxies = None
        # Reconcile model drift for existing accounts without reapplying defaults.
        for item in targets:
            id = item['id']
            if id not in state['completed']:
                continue
            try:
                if str(id) in ready_pending or initial_quality_pending(item):
                    # A pending initial classification is coordinated below. Do not
                    # turn a missing ready marker into a second defaults writer.
                    continue
                plan = normalize_plan(item['credentials'].get('plan_type'))
                template = config['templates'].get(plan)
                if not template:
                    continue
                desired = desired_model_mapping(template, model_exclusions_for(config, id, item.get('extra')))
                if item['credentials'].get('model_mapping') == desired:
                    continue
                live = api('GET', f'/admin/accounts/{id}')
                if normalize_plan(live['credentials'].get('plan_type')) != plan:
                    continue
                desired = desired_model_mapping(template, model_exclusions_for(config, id, live.get('extra')))
                if live['credentials'].get('model_mapping') == desired:
                    continue
                credentials = dict(live['credentials'])
                credentials['model_mapping'] = desired
                try:
                    api('PUT', f'/admin/accounts/{id}', {'credentials':credentials})
                except (RuntimeError, subprocess.TimeoutExpired):
                    # A committed write can lose its response. Resolve it with
                    # independent GET; never resend the PUT in this attempt.
                    pass
                after = api('GET', f'/admin/accounts/{id}')
                assert after['credentials'] == credentials, 'Model repair readback mismatch'
                for field in ('schedulable','concurrency','load_factor','priority','proxy_id','group_ids'):
                    assert after.get(field) == live.get(field), 'Unrelated setting changed'
                print(json.dumps({'models_repaired':id,'plan':plan,'models':len(desired)}),flush=True)
            except Exception as exc:
                failures.append(id)
                print(json.dumps({'models_repair_failed':id,'error':str(exc)[:160]}),flush=True)
        for item in targets:
            id = item['id']
            key = str(id)
            if id not in state['completed'] or (key not in ready_pending and not initial_quality_pending(item)):
                continue
            try:
                live = api('GET', f'/admin/accounts/{id}')
                if key not in ready_pending:
                    if initial_ready_matches(live):
                        continue
                    plan = normalize_plan(live['credentials'].get('plan_type'))
                    template = config['templates'].get(plan)
                    if not template:
                        raise RuntimeError('Initial defaults template unavailable')
                    if groups is None:
                        groups = listing('/admin/groups')
                        proxies = listing('/admin/proxies')
                    if not completed_defaults_verified(live, template, config, groups, proxies, accounts):
                        raise RuntimeError('Completed initial defaults not verified')
                    ready_pending[key] = initial_ready_record(live)
                    save(state_path, state)
                coordinate_initial_ready(live, state, state_path)
                ready_pending.pop(key)
                save(state_path, state)
                print(json.dumps({'initial_ready':id,'action':'coordinated'}),flush=True)
            except Exception as exc:
                failures.append(id)
                print(json.dumps({'pending':id,'error':str(exc)[:160]}),flush=True)
        pending = [a for a in targets if a['id'] not in state['completed']]
        unsupported = state.setdefault('unsupported', {})
        pending_ids = {str(a['id']) for a in pending}
        state_changed = False
        for account_id in list(unsupported):
            if account_id not in pending_ids:
                unsupported.pop(account_id, None)
                state_changed = True
        if not pending:
            if state_changed:
                save(state_path, state)
            print(json.dumps({'pending':0}))
            if failures:
                print(json.dumps({'failed_accounts':sorted(set(failures))}),flush=True)
                raise SystemExit(1)
            return
        # Legacy TEST memberships remain; borrowing uses shared business pools.
        if groups is None:
            groups = listing('/admin/groups')
            proxies = listing('/admin/proxies')
        proxy_tests = {}
        new_unsupported = []
        for item in pending:
            id = item['id']
            try:
                a = api('GET', f'/admin/accounts/{id}')
                raw_plan = a['credentials'].get('plan_type')
                plan = normalize_plan(raw_plan)
                template = config['templates'].get(plan)
                if not template:
                    key = str(id)
                    previous = unsupported.get(key)
                    if not isinstance(previous, dict) or previous.get('plan_type') != raw_plan:
                        unsupported[key] = {
                            'plan_type': raw_plan,
                            'first_seen_at': int(time.time()),
                        }
                        state_changed = True
                        new_unsupported.append(id)
                        print(json.dumps({
                            'unsupported': id,
                            'plan_type': raw_plan,
                            'action': 'deferred',
                        }), flush=True)
                    continue
                key = str(id)
                if key in ready_pending:
                    after = coordinate_initial_ready(a, state, state_path)
                else:
                    patch = patch_for(a, template, new_account_group_ids(config, groups, a.get('group_ids', [])),
                                      model_exclusions_for(config, id, a.get('extra')))
                    country_codes = dedicated_proxy_country_codes(template)
                    if country_codes:
                        patch['proxy_id'] = choose_dedicated_proxy(a, proxies, country_codes, proxy_tests)
                    if initial_quality_pending(a):
                        # The previous cycle may have lost the response or
                        # readback. Its fresh GET above must precede any write;
                        # an already matching patch does not need another PUT.
                        if not defaults_patch_matches(a, patch):
                            try:
                                api('PUT', f'/admin/accounts/{id}', patch)
                            except (RuntimeError, subprocess.TimeoutExpired):
                                pass
                    else:
                        api('PUT', f'/admin/accounts/{id}', patch)
                    after = api('GET', f'/admin/accounts/{id}')
                    assert defaults_patch_matches(after, patch), 'Readback mismatch'
                    assert after['schedulable']==a['schedulable']
                    if initial_quality_pending(after):
                        ready_pending[key] = initial_ready_record(after)
                        save(state_path, state)
                        after = coordinate_initial_ready(after, state, state_path)
                if unsupported.pop(str(id), None) is not None:
                    state_changed = True
                state['completed'].append(id)
                ready_pending.pop(key, None)
                save(state_path,state)
                print(json.dumps({'configured':id,'plan':plan,'proxy':after.get('proxy_id')}),flush=True)
            except Exception as exc:
                failures.append(id)
                print(json.dumps({'pending':id,'error':str(exc)[:160]}),flush=True)
        if state_changed:
            save(state_path, state)
        if failures or new_unsupported:
            print(json.dumps({'failed_accounts':sorted(set(failures)),
                              'new_unsupported_accounts':sorted(set(new_unsupported))}),flush=True)
            raise SystemExit(1)

if __name__=='__main__':
    main()
