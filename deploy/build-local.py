#!/usr/bin/env python3
"""Build a traceable Linux artifact locally; never connects to production."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def files(directory):
    result = {}
    for path in sorted(directory.rglob('*')):
        if path.is_symlink():
            raise ValueError('symlinks are not release inputs')
        if path.is_file():
            result[path.relative_to(directory).as_posix()] = sha(path)
    return result


def capture(args):
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()


def origin(value):
    if value == '':
        return value
    parsed = urlsplit(value)
    if (parsed.scheme != 'https' or not parsed.netloc or parsed.username
            or parsed.password or parsed.path or parsed.query or parsed.fragment
            or re.search(r'\s', value)):
        raise argparse.ArgumentTypeError('expected an HTTPS origin without a path or credentials')
    return value


def source():
    commit = capture(['git', 'rev-parse', 'HEAD'])
    if not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('a Git commit is required')
    return {'commit': commit, 'tree': capture(['git', 'rev-parse', 'HEAD^{tree}']),
            'clean': not capture(['git', 'status', '--porcelain=v1']),
            'epoch': int(capture(['git', 'show', '-s', '--format=%ct', 'HEAD']))}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ritel', action='store_true', help='use the public account.ritel.si / dash.ritel.si origins')
    parser.add_argument('--account-origin', type=origin)
    parser.add_argument('--console-origin', type=origin)
    parser.add_argument('--go', default=os.environ.get('GO', 'go'))
    parser.add_argument('--pnpm', default='pnpm')
    parser.add_argument('--arch', choices=('amd64', 'arm64'), default='amd64')
    parser.add_argument('--version')
    parser.add_argument('--output', type=Path, help='new output directory; defaults to the local cache')
    parser.add_argument('--offline', action='store_true', help='require dependencies already in local caches')
    parser.add_argument('--plan', action='store_true', help='show the plan without building')
    args = parser.parse_args()
    before = source()
    account = args.account_origin if args.account_origin is not None else ('https://account.ritel.si' if args.ritel else '')
    console = args.console_origin if args.console_origin is not None else ('https://dash.ritel.si' if args.ritel else '')
    if bool(account) != bool(console):
        parser.error('account and console origins must be supplied together')
    version = args.version or capture(['sh', 'backend/scripts/resolve-version.sh'])
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9.+_-]*', version):
        parser.error('version contains unsupported characters')
    epoch = int(os.environ.get('SOURCE_DATE_EPOCH', before['epoch']))
    if epoch < 0:
        parser.error('SOURCE_DATE_EPOCH must be nonnegative')
    date = datetime.datetime.fromtimestamp(epoch, datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
    output = (args.output or Path(os.environ.get('XDG_CACHE_HOME', str(Path.home()/'.cache')))/'sub2api-build'/before['commit']/args.arch).expanduser().absolute()
    if ROOT == output or ROOT in output.parents:
        parser.error('keep release artifacts outside the source tree')
    if output.is_symlink():
        parser.error('output must not be a symlink')
    frontend = [args.pnpm, '--dir', 'frontend', 'run', 'build', '--outDir', 'dist']
    flags = f'-s -w -X main.Version={version} -X main.Commit={before["commit"]} -X main.Date={date} -X main.BuildType=release'
    backend = [args.go, 'build', '-mod=readonly', '-tags', 'embed', '-trimpath', '-ldflags', flags, '-o', str(output/'sub2api'), './cmd/server']
    if args.plan:
        print(json.dumps({'source': before, 'version': version, 'date': date, 'output': str(output),
                          'public_origins': {'account': account, 'console': console},
                          'steps': [frontend, backend]}, indent=2))
        return
    if not before['clean']:
        parser.error('release builds require a clean, committed source tree')
    # Vite reads ignored local env files as well as the process environment.
    # Keep release inputs explicit; their hidden values are never printed.
    if any((ROOT/'frontend'/name).exists() for name in ('.env', '.env.local', '.env.production', '.env.production.local')):
        parser.error('release builds require explicit origins and no active frontend .env files')
    if output.exists():
        parser.error('output already exists; choose a new output directory')
    output.mkdir(parents=True, mode=0o700)
    env = dict({key: value for key, value in os.environ.items() if not key.startswith('VITE_')},
               CGO_ENABLED='0', GOOS='linux', GOARCH=args.arch,
               GOTOOLCHAIN='local', SOURCE_DATE_EPOCH=str(epoch),
               VITE_RITEL_ACCOUNT_ORIGIN=account, VITE_RITEL_CONSOLE_ORIGIN=console)
    if args.offline:
        env.update(GOPROXY='off', GOSUMDB='off')
    tools = {'go': capture([args.go, 'version']), 'node': capture(['node', '--version']),
             'pnpm': capture([args.pnpm, '--version'])}
    expected_go = re.search(r'^go ([0-9.]+)$', (ROOT/'backend/go.mod').read_text(), re.MULTILINE)
    if not expected_go or not re.search(r'\bgo'+re.escape(expected_go[1])+r'\b', tools['go']):
        parser.error('Go must match the toolchain version in backend/go.mod')
    expected_pnpm = json.loads((ROOT/'frontend/package.json').read_text())['packageManager'].split('@', 1)[1]
    if tools['pnpm'] != expected_pnpm:
        parser.error('pnpm must match frontend/package.json packageManager exactly')

    def run(name, command, cwd):
        print('START '+name, flush=True)
        with (output/(name+'.log')).open('w') as log:
            subprocess.run(command, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
        print('PASS '+name, flush=True)

    # The explicit outDir avoids the conditional Vite output used by account mode.
    run('frontend', frontend, ROOT)
    dist = ROOT/'frontend/dist'
    manifest = files(dist)
    if 'index.html' not in manifest or not any(p.startswith('assets/') and p.endswith('.js') for p in manifest):
        raise ValueError('frontend output is incomplete')
    embedded = ROOT/'backend/internal/web/dist'
    if embedded.is_symlink() or embedded.resolve() != embedded:
        raise ValueError('unsafe generated embed directory')
    if embedded.exists():
        shutil.rmtree(embedded)
    shutil.copytree(dist, embedded)
    if files(embedded) != manifest:
        raise ValueError('frontend embed copy differs')
    run('web-embed-test', [args.go, 'test', '-mod=readonly', '-tags', 'embed', './internal/web'], ROOT/'backend')
    run('backend', backend, ROOT/'backend')
    info = capture([args.go, 'version', '-m', str(output/'sub2api')])
    if before['commit'] not in info or 'vcs.modified=false' not in info:
        raise ValueError('binary VCS provenance differs')
    (output/'go-build-info.txt').write_text(info+'\n')
    if args.arch == 'amd64' and capture(['uname', '-m']) == 'x86_64':
        version_output = capture([str(output/'sub2api'), '--version'])
        if version not in version_output or before['commit'][:9] not in version_output:
            raise ValueError('built binary version differs')
        (output/'version.txt').write_text(version_output+'\n')
    shutil.copytree(ROOT/'backend/resources', output/'resources')
    if source() != before or files(embedded) != manifest:
        raise ValueError('source or embedded assets changed during the build')
    inputs = {name: sha(ROOT/name) for name in ('frontend/package.json', 'frontend/pnpm-lock.yaml', 'backend/go.mod', 'backend/go.sum')}
    result = {'format': 1, 'source': before, 'version': version, 'build_date': date,
              'source_date_epoch': epoch, 'platform': 'linux/'+args.arch, 'tools': tools,
              'public_origins': {'account': account, 'console': console}, 'inputs': inputs,
              'binary': {'path': 'sub2api', 'bytes': (output/'sub2api').stat().st_size, 'sha256': sha(output/'sub2api')},
              'frontend': manifest, 'resources': files(output/'resources')}
    (output/'manifest.json').write_text(json.dumps(result, sort_keys=True, indent=2)+'\n')
    print(json.dumps({'manifest': str(output/'manifest.json'), 'sha256': sha(output/'manifest.json'),
                      'binary_sha256': result['binary']['sha256'], 'binary_bytes': result['binary']['bytes']}))


if __name__ == '__main__':
    main()
