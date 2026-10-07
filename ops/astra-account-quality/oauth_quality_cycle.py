#!/usr/bin/env python3
"""Probe every live OpenAI OAuth account and reconcile per-model quality."""

import argparse
import concurrent.futures
import fcntl
import json
import os
from pathlib import Path
import re
import subprocess
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

BASE = 'https://api-dr.ritelt.com/api/v1'
KEY_PATH = Path('/srv/vm-la-production/rootfs/home/ritel/sub2api/quality-monitor/admin-key')
STATE_DIR = Path('/srv/vm-la-production/rootfs/home/ritel/sub2api/quality-monitor')
MODELS = ('gpt-6-sol', 'gpt-6-astra')
SOL_NAMES = ('gpt-6-sol',)
PROMPT = '不联网搜索、不调用工具，仅凭模型训练知识回答：你的知识截止日期是？你所知的最新Gemini模型？'
USER_AGENT = 'openclaw-sub2api-adminctl/1'
MAX_TEXT = 12000
NOTICE_STATE = STATE_DIR / 'notification-state.json'
HISTORY_PATH = STATE_DIR / 'history.json'
TELEGRAM_TOKEN = STATE_DIR / 'telegram-bot-token'
TELEGRAM_CHAT_ID = STATE_DIR / 'telegram-controller-id'
TELEGRAM_THREAD_ID = STATE_DIR / 'telegram-monitor-thread-id'
ERROR_STREAK_THRESHOLD = 3
HISTORY_LIMIT = 96
COOLDOWN_NOTICE_SECONDS = 6 * 3600


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def normalized(text):
    return re.sub(r'[\s*_`，,。]+', '', text.lower()).replace('．', '.')


def classify(text, completed, response_model, requested_model):
    if not completed or not text.strip():
        return 'error'
    if response_model != requested_model:
        return 'error'
    value = normalized(text)
    old_date = ('2024年6月' in value or '2024-06' in value or 'june2024' in value or '2024june' in value)
    old_gemini = 'gemini1.5' in value or 'gemini-1.5' in value
    return 'degraded' if old_date and old_gemini else 'good'


def describe_failure(payload, status, event_error, curl_exit):
    error = event_error
    if not error:
        try:
            parsed = json.loads(payload.strip())
            error = parsed.get('error',parsed) if isinstance(parsed,dict) else None
        except (ValueError,TypeError):
            error = None
    if isinstance(error,dict):
        code = str(error.get('type') or error.get('code') or 'upstream_error')[:80]
    else:
        code = 'upstream_error' if error else 'incomplete'
    if status and status != '200':
        return f'http_{status}:{code}'
    return f'curl_{curl_exit}:{code}' if curl_exit else code


def allowed_for_plan(plan):
    return plan in ('pro', 'prolite', 'plus', 'self_serve_business_prolite')


def quality_group_ids(groups):
    """Resolve stable IDs: environment > sibling JSON > good=15, bad=16.

    quality-groups.json uses "good" and "bad" keys. Environment overrides are
    OAUTH_QUALITY_GOOD_GROUP_ID and OAUTH_QUALITY_BAD_GROUP_ID. Missing keys use
    defaults; invalid/empty values fail closed. Display names are not used.
    """
    config_path = Path(__file__).with_name('quality-groups.json')
    try:
        config = json.loads(config_path.read_text())
    except FileNotFoundError:
        config = {}
    except (OSError, ValueError):
        raise RuntimeError(f'cannot read quality group config {config_path}: expected readable JSON') from None
    if not isinstance(config, dict):
        raise RuntimeError(f'quality group config {config_path} must be a JSON object')

    ids = {}
    for role, default in (('good', 15), ('bad', 16)):
        env_name = f'OAUTH_QUALITY_{role.upper()}_GROUP_ID'
        value = os.environ.get(env_name, config.get(role, default))
        if isinstance(value, str) and re.fullmatch(r'[0-9]+', value.strip()):
            try:
                value = int(value.strip())
            except ValueError:
                pass
        if type(value) is not int or value <= 0:
            raise RuntimeError(f'quality group {role} ID must be a positive integer ({env_name} or {config_path})')
        ids[role] = value

    if ids['good'] == ids['bad']:
        raise RuntimeError(f"quality group IDs must differ: good={ids['good']}, bad={ids['bad']}")
    for role, group_id in ids.items():
        matches = [g for g in groups if g.get('id') == group_id]
        if not matches:
            raise RuntimeError(f'quality group {role} ID {group_id} is missing from the groups list')
        if len(matches) != 1:
            raise RuntimeError(f'quality group {role} ID {group_id} appears more than once in the groups list')
        if matches[0].get('platform') != 'openai':
            raise RuntimeError(f"quality group {role} ID {group_id} must have platform 'openai'")
    return ids


def bps_routes_model(extra, model):
    if not isinstance(extra, dict) or extra.get('openai_excel_bps') is not True:
        return False
    if 'openai_excel_bps_models' not in extra:
        return True
    models = extra['openai_excel_bps_models']
    return isinstance(models, list) and model in models


def reconcile(account, outcomes, good_group, bad_group, template):
    """Return only changed fields; an error never changes a model's prior decision."""
    if any(outcomes.get(model) not in ('good', 'degraded') for model in MODELS):
        return {}
    before = account.get('extra') or {}
    saved = before.get('quality_model_results') or {}
    saved = dict(saved) if isinstance(saved, dict) else {}
    for model in MODELS:
        if outcomes.get(model) in ('good', 'degraded'):
            saved[model] = outcomes[model]
    groups = list(account.get('group_ids') or [])
    eligible = allowed_for_plan((account.get('credentials') or {}).get('plan_type'))
    effective_extra = dict(before)
    if saved:
        effective_extra['quality_model_results'] = saved
    if eligible and (bad_group in groups or any(saved.get(model) == 'degraded' for model in MODELS)):
        bps_models = effective_extra.get('openai_excel_bps_models')
        bps_models = list(bps_models) if isinstance(bps_models, list) else []
        for model in ('gpt-6-astra', 'gpt-6-sol'):
            if model not in bps_models:
                bps_models.append(model)
        effective_extra['openai_excel_bps'] = True
        effective_extra['openai_excel_bps_models'] = bps_models
    patch = {}
    if effective_extra != before:
        patch['extra'] = effective_extra

    mapping = dict((account.get('credentials') or {}).get('model_mapping') or {})
    desired = dict(mapping)
    for model in MODELS:
        state = outcomes.get(model)
        keys = SOL_NAMES if model == 'gpt-6-sol' else (model,)
        if state == 'degraded':
            for name in keys:
                if name == model and bps_routes_model(effective_extra, model) and eligible and name in template.get('models', ()):
                    desired[name] = (template.get('model_aliases') or {}).get(name, name)
                else:
                    desired.pop(name, None)
        elif state == 'good' and eligible:
            for name in keys:
                if name in template.get('models', ()):
                    desired[name] = (template.get('model_aliases') or {}).get(name, name)
    if desired != mapping:
        credentials = dict(account['credentials'])
        credentials['model_mapping'] = desired
        patch['credentials'] = credentials

    if any(saved.get(model) == 'degraded' for model in MODELS):
        target = list(groups)
        if bad_group not in target:
            target.append(bad_group)
    elif all(saved.get(model) == 'good' for model in MODELS):
        target = list(groups)
        if good_group not in target:
            target.append(good_group)
    else:
        target = groups
    if target != groups:
        patch['group_ids'] = target
    return patch


def quality_transitions(previous, current):
    return [{'model': model, 'from': previous.get(model), 'to': current[model]}
            for model in MODELS if current.get(model) in ('good', 'degraded')
            and current[model] != previous.get(model)]


def error_category(error):
    if not isinstance(error, str):
        return 'other'
    if error.startswith('http_429:') or error in ('usage_limit_reached', 'rate_limit_exceeded'):
        return 'rate_limited'
    if error.startswith('http_401:'):
        return 'credential_401'
    if error.startswith(('http_500:', 'http_502:', 'http_503:', 'http_504:')) or error in ('server_error', 'server_is_overloaded') or 'overloaded' in error:
        return 'upstream_unavailable'
    return 'other'


def cycle_coverage(summary):
    results = summary['results']
    counts = {model: {'good': 0, 'degraded': 0, 'rate_limited': 0,
                      'credential_401': 0, 'upstream_unavailable': 0, 'other': 0}
              for model in MODELS}
    for result in results:
        model = result['model']
        if model not in counts:
            continue
        category = result['class'] if result['class'] in ('good', 'degraded') else error_category(result.get('error'))
        counts[model][category] += 1
    complete = sum(all(any(r['id'] == account and r['model'] == model
                           and r['class'] in ('good', 'degraded') for r in results)
                       for model in MODELS) for account in {r['id'] for r in results})
    return {'account_pairs_completed': complete, 'accounts': summary['accounts'],
            'model_results': counts}


def update_notification_state(previous, summary):
    previous = previous if isinstance(previous, dict) else {}
    previous_credential = previous.get('credential_401') or {}
    previous_quality = previous.get('quality_streaks') or {}
    results = {(result['id'], result['model']): result for result in summary['results']}
    ids = sorted({account for account, _ in results})
    credential, quality = {}, {}
    events = []
    for account_id in ids:
        key = str(account_id)
        row = previous_credential.get(key) or {}
        has_401 = any(error_category(results.get((account_id, model), {}).get('error')) == 'credential_401'
                      for model in MODELS)
        both_complete = all(results.get((account_id, model), {}).get('class') in ('good', 'degraded')
                            for model in MODELS)
        count = int(row.get('count') or 0) + 1 if has_401 else 0
        alerted = row.get('alerted') is True
        if has_401 and count >= ERROR_STREAK_THRESHOLD and not alerted:
            events.append({'kind': 'credential_401', 'id': account_id})
            alerted = True
        elif both_complete and alerted:
            events.append({'kind': 'credential_recovered', 'id': account_id})
            alerted = False
        if count or alerted:
            credential[key] = {'count': min(count, 999), 'alerted': alerted}

        prior = previous_quality.get(key) or {}
        per_model = {}
        for model in MODELS:
            result = results.get((account_id, model)) or {}
            classification = result.get('class')
            last = prior.get(model) or {}
            streak = int(last.get('runs') or 0) + 1 if classification in ('good', 'degraded') and last.get('class') == classification else 1
            evidence = list(last.get('evidence') or [])
            if classification in ('good', 'degraded'):
                evidence.append({'at': summary['at'], 'class': classification,
                                 'response_model': result.get('response_model'),
                                 'text': str(result.get('text') or '')[:600]})
            per_model[model] = {'class': classification,
                                'runs': min(streak, 999) if classification in ('good', 'degraded') else 0,
                                'evidence': evidence[-3:]}
        quality[key] = per_model

    complete = cycle_coverage(summary)['account_pairs_completed']
    empty = int(previous.get('zero_success_streak') or 0) + 1 if complete == 0 else 0
    zero_alerted = previous.get('zero_success_alerted') is True
    if empty >= ERROR_STREAK_THRESHOLD and not zero_alerted:
        events.append({'kind': 'no_coverage'})
        zero_alerted = True
    elif complete and zero_alerted:
        events.append({'kind': 'coverage_recovered'})
        zero_alerted = False

    now = datetime.fromisoformat(summary['at']).timestamp()
    previous_cooldowns = previous.get('cooldown_notice_at') or {}
    cooldowns = {key: value for key, value in previous_cooldowns.items()
                 if isinstance(value, (int, float)) and now - value < COOLDOWN_NOTICE_SECONDS}
    for change in summary.get('changes') or []:
        if 'error' in change:
            continue
        for transition in change.get('transitions') or []:
            if transition['to'] == 'degraded':
                events.append({'kind': 'degraded', 'id': change['id'], 'model': transition['model']})
            elif transition['from'] == 'degraded' and transition['to'] == 'good':
                events.append({'kind': 'restored', 'id': change['id'], 'model': transition['model']})
        if change.get('recovered') and now - cooldowns.get(str(change['id']), 0) >= COOLDOWN_NOTICE_SECONDS:
            events.append({'kind': 'cooldown_cleared', 'id': change['id']})
            cooldowns[str(change['id'])] = now
    return ({'credential_401': credential, 'quality_streaks': quality,
             'zero_success_streak': min(empty, 999), 'zero_success_alerted': zero_alerted,
             'cooldown_notice_at': cooldowns, 'pending': list(previous.get('pending') or [])}, events)


def format_notification(events, summary):
    if not events:
        return ''
    at = datetime.fromisoformat(summary['at']).astimezone(ZoneInfo('Asia/Tokyo')).strftime('%m-%d %H:%M JST')
    lines = [f'[OAuth 质量检测] {at}']
    models = {'gpt-6-sol': '6 Sol（含 5.6 Sol/Terra）', 'gpt-6-astra': 'Astra'}
    descriptions = {
        'degraded': '判为降智，已移除对应模型',
        'restored': '恢复正常，已恢复对应模型',
        'cooldown_cleared': '成功测试后解除账号冷却（后续请求可能重新限流）',
        'credential_401': '连续 3 轮遇到 401，请检查或重授权',
        'credential_recovered': '此前连续 401，现两个模型均已成功',
        'no_coverage': '连续 3 轮无账号完成双模型测试，请检查额度或上游',
        'coverage_recovered': '双模型测试重新有有效结果',
    }
    for event in events:
        prefix = f"#{event['id']} " if 'id' in event else ''
        model = models[event['model']] + '：' if 'model' in event else ''
        lines.append(f"{prefix}{model}{descriptions[event['kind']]}")
    coverage = cycle_coverage(summary)
    lines.append(f"本轮有效账号 {coverage['account_pairs_completed']}/{coverage['accounts']}；限流与其他错误不触发账号配置修改。")
    return '\n'.join(lines)


def save_private(path, value):
    candidate = path.with_name(path.name + '.tmp')
    with candidate.open('w', encoding='utf-8') as handle:
        json.dump(value, handle, ensure_ascii=False, indent=2)
        handle.write('\n')
        handle.flush()
        os.fsync(handle.fileno())
    candidate.chmod(0o600)
    candidate.replace(path)


def send_telegram(message):
    token = TELEGRAM_TOKEN.read_text(encoding='utf-8').strip()
    chat = TELEGRAM_CHAT_ID.read_text(encoding='ascii').strip()
    thread = TELEGRAM_THREAD_ID.read_text(encoding='ascii').strip()
    if not token or not chat.isdigit() or not thread.isdigit() or int(thread) <= 0:
        raise RuntimeError('Telegram monitor topic is not configured')
    opener = urllib.request.build_opener(NoRedirect(), urllib.request.ProxyHandler({}))
    for chunk in split_message(message):
        request = urllib.request.Request(f'https://api.telegram.org/bot{token}/sendMessage',
            data=json.dumps({'chat_id': chat, 'message_thread_id': int(thread),
                             'text': chunk, 'disable_web_page_preview': True}, ensure_ascii=False).encode('utf-8'),
            headers={'Content-Type': 'application/json'}, method='POST')
        try:
            with opener.open(request, timeout=20) as response:
                payload = json.load(response)
        except (OSError, ValueError, urllib.error.URLError):
            raise RuntimeError('Telegram delivery failed') from None
        if payload.get('ok') is not True:
            raise RuntimeError('Telegram delivery failed')


def split_message(message, limit=3500):
    chunks = []
    current = ''
    for line in message.splitlines():
        for part in (line[i:i + limit] for i in range(0, len(line), limit)):
            candidate = part if not current else current + '\n' + part
            if len(candidate) > limit:
                chunks.append(current)
                current = part
            else:
                current = candidate
    if current:
        chunks.append(current)
    return chunks


def persist_and_deliver(summary):
    previous = json.loads(NOTICE_STATE.read_text()) if NOTICE_STATE.exists() else {}
    state, events = update_notification_state(previous, summary)
    message = format_notification(events, summary)
    if message:
        state['pending'].append(message)
    history = json.loads(HISTORY_PATH.read_text()) if HISTORY_PATH.exists() else []
    history.append({'at': summary['at'], **cycle_coverage(summary)})
    save_private(HISTORY_PATH, history[-HISTORY_LIMIT:])
    save_private(NOTICE_STATE, state)
    for pending in list(state['pending']):
        send_telegram(pending)
        state['pending'].pop(0)
        save_private(NOTICE_STATE, state)


def admin(method, path, key, body=None):
    headers = {'X-API-Key': key, 'Content-Type': 'application/json', 'Accept': 'application/json', 'User-Agent': USER_AGENT}
    request = urllib.request.Request(BASE + path, method=method, headers=headers,
        data=json.dumps(body, ensure_ascii=False).encode() if body is not None else None)
    try:
        with urllib.request.urlopen(request, timeout=25) as response:
            payload = json.load(response)
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f'Admin API {path} HTTP {exc.code}') from None
    if isinstance(payload, dict) and 'code' in payload:
        if payload['code'] != 0:
            raise RuntimeError(f'Admin API {path} returned a failure')
        return payload.get('data')
    return payload


def inventory():
    query = """
SELECT json_build_object('id',a.id,'type',a.type,'platform',a.platform,
       'credentials',a.credentials,'proxy',row_to_json(p))
FROM accounts a LEFT JOIN proxies p ON p.id=a.proxy_id
WHERE a.platform='openai' AND a.type='oauth' AND a.deleted_at IS NULL ORDER BY a.id
"""
    process = subprocess.run(['sudo','-n','docker','exec','sub2api-postgres','psql',
        '-U','sub2api','-d','sub2api','-At','-v','ON_ERROR_STOP=1','-c',query],
        capture_output=True, text=True, timeout=30, check=True)
    return [json.loads(row) for row in process.stdout.splitlines()]


def probe(account, model):
    credentials = account.get('credentials') or {}
    token = credentials.get('access_token')
    result = {'id':account['id'], 'model':model, 'class':'error', 'error':'not_completed'}
    if not token:
        result['error'] = 'missing_token'
        return result
    body = {'model':model,'instructions':'',
        'input':[{'role':'user','content':[{'type':'input_text','text':PROMPT}]}],
        'stream':True,'store':False,'tools':[],'reasoning':{'effort':'xhigh'}}
    config = ['url = "https://chatgpt.com/backend-api/codex/responses"', 'request = "POST"',
        'max-time = 100','silent','show-error','header = "Content-Type: application/json"',
        'header = ' + json.dumps('Authorization: Bearer ' + token),
        'data = ' + json.dumps(json.dumps(body,ensure_ascii=False))]
    if credentials.get('chatgpt_account_id'):
        config.append('header = ' + json.dumps('ChatGPT-Account-Id: ' + credentials['chatgpt_account_id']))
    proxy = account.get('proxy')
    if proxy:
        config.append('proxy = ' + json.dumps(str(proxy.get('protocol') or 'http')+'://'+proxy['host']+':'+str(proxy['port'])))
        if proxy.get('username'):
            config.append('proxy-user = ' + json.dumps(proxy['username']+':'+(proxy.get('password') or '')))
    started = time.monotonic()
    try:
        response = subprocess.run(['curl','--config','-','--write-out','\n__HTTP_STATUS__:%{http_code}'], input='\n'.join(config), capture_output=True,
            text=True, timeout=110, check=False)
        payload, marker, status = response.stdout.rpartition('\n__HTTP_STATUS__:')
        if not marker:
            payload, status = response.stdout, ''
        text_parts, completed, response_model, error = [], False, None, None
        for line in payload.splitlines():
            if not line.startswith('data:'):
                continue
            try:
                event = json.loads(line[5:])
            except ValueError:
                continue
            kind = event.get('type')
            if kind == 'response.output_text.delta':
                text_parts.append(event.get('delta') or '')
            elif kind == 'response.completed':
                completed = True
                final = event.get('response') or {}
                response_model = final.get('model')
                if not text_parts:
                    for item in final.get('output') or []:
                        text_parts.extend(part.get('text','') for part in item.get('content') or []
                                          if part.get('type') == 'output_text')
            elif kind in ('response.failed','error'):
                error = event.get('error') or (event.get('response') or {}).get('error')
        text = ''.join(text_parts)[:MAX_TEXT]
        result['class'] = classify(text, completed and response.returncode == 0, response_model, model)
        result['response_model'] = response_model
        result['text'] = text[:600]
        if result['class'] == 'error':
            result['error'] = describe_failure(payload,status,error,response.returncode)
            if response_model and response_model != model:
                result['error'] = 'response_model_mismatch'
        else:
            result.pop('error',None)
    except subprocess.TimeoutExpired:
        result['error'] = 'timeout'
    result['seconds'] = round(time.monotonic() - started, 2)
    return result


def apply_one(account_id, outcomes, key, group_ids, templates):
    if any(outcomes.get(model) not in ('good', 'degraded') for model in MODELS):
        return {'id':account_id,'change':[],'skipped':'incomplete_probe'}
    current = admin('GET',f'/admin/accounts/{account_id}',key)
    if current.get('platform') != 'openai' or current.get('type') != 'oauth':
        return {'id':account_id,'change':'skipped_non_oauth'}
    plan = (current.get('credentials') or {}).get('plan_type')
    template = templates.get({'self_serve_business_prolite':'prolite'}.get(plan,plan),{})
    prior_quality = dict((current.get('extra') or {}).get('quality_model_results') or {})
    patch = reconcile(current,outcomes,group_ids['good'],group_ids['bad'],template)
    if patch:
        admin('PUT',f'/admin/accounts/{account_id}',key,patch)
    after = admin('GET',f'/admin/accounts/{account_id}',key) if patch else current
    for field,value in patch.items():
        if after.get(field) != value:
            raise RuntimeError(f'account {account_id} {field} readback mismatch')
    for field in ('status','schedulable','proxy_id','concurrency','load_factor','priority'):
        if after.get(field) != current.get(field):
            raise RuntimeError(f'account {account_id} unexpected {field} change')
    # A successful request proves the account-level cooldown is stale only
    # when both selected models completed; partial/model-specific success is
    # deliberately insufficient.
    recovered = False
    if after.get('schedulable'):
        reset = after.get('rate_limit_reset_at')
        if reset:
            try:
                future = datetime.fromisoformat(reset.replace('Z','+00:00')) > datetime.now(timezone.utc)
            except ValueError:
                future = False
            if future:
                admin('POST',f'/admin/accounts/{account_id}/clear-rate-limit',key,{})
                cleared = admin('GET',f'/admin/accounts/{account_id}',key)
                recovered = not cleared.get('rate_limit_reset_at')
                if not recovered:
                    raise RuntimeError(f'account {account_id} cooldown readback mismatch')
    return {'id':account_id,'change':list(patch),'recovered':recovered,
            'transitions':quality_transitions(prior_quality,outcomes)}


def run(apply=False, account_ids=()):
    os.umask(0o077)
    STATE_DIR.mkdir(mode=0o700,parents=True,exist_ok=True)
    with (STATE_DIR/'cycle.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        accounts = inventory()
        if account_ids:
            accounts = [account for account in accounts if account['id'] in account_ids]
        if not accounts:
            raise RuntimeError('empty OAuth inventory')
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            jobs = {(a['id'],model):pool.submit(probe,a,model) for a in accounts for model in MODELS}
            results = [jobs[(a['id'],model)].result() for a in accounts for model in MODELS]
        summary = {'at':datetime.now(timezone.utc).isoformat(),'accounts':len(accounts),
                   'probes':len(results),'results':results,'applied':apply,'changes':[]}
        if apply:
            key = KEY_PATH.read_text().strip()
            config = json.loads((STATE_DIR/'account-defaults.json').read_text())
            groups = admin('GET','/admin/groups?page=1&page_size=100',key)['items']
            group_ids = quality_group_ids(groups)
            for a in accounts:
                outcomes = {r['model']:r['class'] for r in results if r['id']==a['id']}
                if any(value not in ('good','degraded') for value in outcomes.values()):
                    continue
                try:
                    change = apply_one(a['id'],outcomes,key,group_ids,config['templates'])
                    summary['changes'].append(change)
                except Exception as exc:
                    summary['changes'].append({'id':a['id'],'error':str(exc)[:160]})
        save_private(STATE_DIR/'last-run.json',summary)
        compact = {'accounts':len(accounts),'probes':len(results),'good':sum(r['class']=='good' for r in results),
                   'degraded':sum(r['class']=='degraded' for r in results),
                   'errors':sum(r['class']=='error' for r in results),
                   'changed':sum(bool(c.get('change')) for c in summary['changes']),
                   'recovered':sum(bool(c.get('recovered')) for c in summary['changes']),
                   'apply_errors':[c['id'] for c in summary['changes'] if 'error' in c]}
        print(json.dumps(compact,sort_keys=True))
        if apply:
            persist_and_deliver(summary)
        if compact['apply_errors']:
            raise RuntimeError('account reconciliation failed')
        return compact


if __name__=='__main__':
    args = argparse.ArgumentParser()
    args.add_argument('--apply',action='store_true')
    args.add_argument('--account-id',action='append',type=int,default=[])
    options = args.parse_args()
    run(options.apply, set(options.account_id))
