"""Offline orchestration/contract checks; these do not claim a compiled release."""
import argparse
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).resolve().parents[1]/filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


build = load('local_builder_test', 'build-local.py')
package = load('runtime_package_test', 'package-runtime.py')
COMMIT = 'a'*40


class LocalBuildTests(unittest.TestCase):
    def test_origins_reject_credentials_and_non_origins(self):
        self.assertEqual(build.origin('https://account.ritel.si'), 'https://account.ritel.si')
        for value in ['http://account.ritel.si', 'https://a:b@example.com', 'https://example.com/',
                      'https://example.com?q=x', 'https://example.com#x', 'https://example.com\n']:
            with self.subTest(value=value), self.assertRaises(argparse.ArgumentTypeError):
                build.origin(value)

    def test_symlink_resource_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            (directory/'file').write_text('data')
            (directory/'alias').symlink_to(directory/'file')
            with self.assertRaisesRegex(ValueError, 'symlinks'):
                build.files(directory)

    def test_frontend_precedes_embed_and_build_and_date_is_fixed(self):
        with tempfile.TemporaryDirectory() as temporary:
            parent = Path(temporary)
            root, output = parent/'source', parent/'artifact'
            (root/'frontend').mkdir(parents=True)
            (root/'backend/resources').mkdir(parents=True)
            (root/'backend/internal/web').mkdir(parents=True)
            (root/'frontend/package.json').write_text('{"packageManager":"pnpm@9.15.9"}')
            for name in ['frontend/pnpm-lock.yaml', 'backend/go.mod', 'backend/go.sum', 'backend/resources/pricing.json']:
                (root/name).write_text('fixture')
            (root/'backend/go.mod').write_text('module fixture\n\ngo 1.27.3\n')
            commands = []

            def capture(args):
                if args[:3] == ['git', 'rev-parse', 'HEAD']:
                    return COMMIT
                if args[:3] == ['git', 'rev-parse', 'HEAD^{tree}']:
                    return 'b'*40
                if args[:2] == ['git', 'status']:
                    return ''
                if args[:2] == ['git', 'show']:
                    return '1700000000'
                if args == ['go-fixture', 'version']:
                    return 'go1.27.3'
                if args[:3] == ['go-fixture', 'version', '-m']:
                    return COMMIT+' vcs.modified=false'
                if args == ['pnpm-fixture', '--version']:
                    return '9.15.9'
                if args == ['node', '--version']:
                    return 'v24.0.0'
                if args == ['uname', '-m']:
                    return 'x86_64'
                if args == [str(output/'sub2api'), '--version']:
                    return '2.17.9.1-ritel '+COMMIT
                raise AssertionError(args)

            def run(args, **kwargs):
                commands.append((args, kwargs))
                if args[0] == 'pnpm-fixture':
                    dist = root/'frontend/dist'
                    (dist/'assets').mkdir(parents=True)
                    (dist/'index.html').write_text('<script src="/assets/app.js"></script>')
                    (dist/'assets/app.js').write_text('fixture')
                elif args[1] == 'build':
                    self.assertEqual(build.files(root/'backend/internal/web/dist'), build.files(root/'frontend/dist'))
                    (output/'sub2api').write_bytes(b'fixture executable')

            argv = ['build-local.py', '--ritel', '--version', '2.17.9.1-ritel', '--go', 'go-fixture',
                    '--pnpm', 'pnpm-fixture', '--offline', '--output', str(output)]
            with patch.object(build, 'ROOT', root), patch.object(build, 'capture', side_effect=capture), \
                    patch.object(build.subprocess, 'run', side_effect=run), patch('sys.argv', argv), \
                    patch.dict(build.os.environ, {}, clear=True):
                build.main()
            result = json.loads((output/'manifest.json').read_text())
            self.assertEqual([command[0][0] for command in commands], ['pnpm-fixture', 'go-fixture', 'go-fixture'])
            self.assertEqual(result['build_date'], '2023-11-14T22:13:20Z')
            self.assertEqual(result['source']['commit'], COMMIT)
            self.assertEqual(result['public_origins']['console'], 'https://dash.ritel.si')
            environment = commands[-1][1]['env']
            self.assertEqual(environment['CGO_ENABLED'], '0')
            self.assertEqual(environment['GOPROXY'], 'off')
            self.assertIn('-trimpath', commands[-1][0])
            self.assertEqual(result['binary']['sha256'], build.sha(output/'sub2api'))

    def test_dirty_tree_fails_before_output_or_tool_execution(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)/'artifact'
            before = {'commit': COMMIT, 'tree': 'b'*40, 'clean': False, 'epoch': 1700000000}
            with patch.object(build, 'source', return_value=before), \
                    patch('sys.argv', ['build-local.py', '--version', '1.0', '--output', str(output)]), \
                    patch.object(build.subprocess, 'run') as run, self.assertRaises(SystemExit):
                build.main()
            run.assert_not_called()
            self.assertFalse(output.exists())


class RuntimeContractTests(unittest.TestCase):
    def setUp(self):
        self.value = {'version': '2.17.9.1-ritel', 'source': {'commit': COMMIT},
                      'binary': {'sha256': 'c'*64}, 'platform': 'linux/amd64'}
        self.base = {'Config': {'User': '1000:1000', 'Env': ['PATH=/bin'],
                                'Healthcheck': {'Test': ['CMD', 'health']}, 'Labels': {'keep': 'yes'}},
                     'RootFS': {'Layers': ['runtime']}}
        self.candidate = copy.deepcopy(self.base)
        self.candidate.update(Os='linux', Architecture='amd64')
        self.candidate['RootFS']['Layers'] += ['binary', 'resources']
        self.candidate['Config']['Labels'].update({'org.opencontainers.image.version': self.value['version'],
                                                  'org.opencontainers.image.revision': COMMIT,
                                                  'io.ritel.binary.sha256': 'c'*64})

    def test_exact_runtime_config_and_two_artifact_layers(self):
        package.image_contract(self.base, self.candidate, self.value)

    def test_config_or_base_layer_drift_is_rejected(self):
        for key in ['User', 'Env', 'Healthcheck', 'Labels']:
            changed = copy.deepcopy(self.candidate)
            changed['Config'].pop(key)
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, 'Config'):
                package.image_contract(self.base, changed, self.value)
        changed = copy.deepcopy(self.candidate)
        changed['RootFS']['Layers'] = ['old-business', 'runtime', 'binary', 'resources']
        with self.assertRaisesRegex(ValueError, 'layers'):
            package.image_contract(self.base, changed, self.value)

    def test_payload_mutation_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            (directory/'sub2api').write_bytes(b'binary')
            (directory/'resources').mkdir()
            (directory/'resources/pricing.json').write_text('{}')
            value = dict(self.value, format=1,
                         source={'commit': COMMIT, 'clean': True},
                         binary={'path': 'sub2api', 'bytes': 6, 'sha256': build.sha(directory/'sub2api')},
                         resources=build.files(directory/'resources'))
            path = directory/'manifest.json'
            path.write_text(json.dumps(value))
            package.manifest(path)
            (directory/'sub2api').write_bytes(b'changed')
            with self.assertRaisesRegex(ValueError, 'verification'):
                package.manifest(path)


if __name__ == '__main__':
    unittest.main()
