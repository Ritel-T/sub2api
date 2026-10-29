#!/usr/bin/env python3
"""Package a verified local artifact on a fixed runtime, without compilation."""
import argparse
import copy
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('local_build', ROOT/'deploy/build-local.py')
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)


def run(args):
    return subprocess.check_output(args, text=True).strip()


def inspect(name):
    return json.loads(run(['docker', 'image', 'inspect', name]))[0]


def manifest(path):
    path = path.resolve()
    value = json.loads(path.read_text())
    binary = value['binary']
    if (value['format'] != 1 or value['source']['clean'] is not True
            or not re.fullmatch(r'[0-9a-f]{40}', value['source']['commit'])
            or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9.+_-]*', value['version'])
            or binary['path'] != 'sub2api'
            or (path.parent/'sub2api').is_symlink()
            or build.sha(path.parent/'sub2api') != binary['sha256']
            or (path.parent/'sub2api').stat().st_size != binary['bytes']
            or build.files(path.parent/'resources') != value['resources']):
        raise ValueError('artifact manifest or payload verification failed')
    return value


def image_contract(base, candidate, value):
    old, new = copy.deepcopy(base['Config']), copy.deepcopy(candidate['Config'])
    old_labels = old.pop('Labels', None) or {}
    new_labels = new.pop('Labels', None) or {}
    # Docker legacy builders may include their intermediate image bookkeeping.
    old.pop('Image', None)
    new.pop('Image', None)
    expected = dict(old_labels, **{'org.opencontainers.image.version': value['version'],
                                   'org.opencontainers.image.revision': value['source']['commit'],
                                   'io.ritel.binary.sha256': value['binary']['sha256']})
    if old != new or new_labels != expected:
        raise ValueError('runtime image Config changed')
    layers = base['RootFS']['Layers']
    if candidate['RootFS']['Layers'][:-2] != layers or len(candidate['RootFS']['Layers']) != len(layers)+2:
        raise ValueError('image must contain only runtime + binary + resource layers')
    if candidate['Os']+'/'+candidate['Architecture'] != value['platform']:
        raise ValueError('candidate platform differs from artifact')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', required=True, type=Path)
    parser.add_argument('--runtime-image', required=True, help='immutable sha256 image ID or repository@sha256 digest')
    parser.add_argument('--tag', required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'(?:sha256:|[A-Za-z0-9][A-Za-z0-9._:/-]*@sha256:)[0-9a-f]{64}', args.runtime_image):
        parser.error('runtime-image must be immutable')
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._:/-]*', args.tag):
        parser.error('unsupported image tag')
    value = manifest(args.manifest)
    base = inspect(args.runtime_image)
    if (base['Os']+'/'+base['Architecture'] != value['platform']
            or len(base['RootFS']['Layers']) > 4):
        parser.error('expected a small, independently verified runtime baseline')
    probe = ['docker', 'run', '--rm', '--pull=never', '--network=none', '--read-only',
             '--cap-drop=ALL', '--security-opt=no-new-privileges:true', '--pids-limit=32',
             '--memory=128m', '--user=1000:1000', '--entrypoint=/bin/sh']
    # A missing visible binary alone is not proof about historical layers:
    # creation/approval of the runtime baseline is a separate release gate.
    run(probe+[args.runtime_image, '-ec',
               'test ! -e /app/sub2api; test -x /app/docker-entrypoint.sh; pg_dump --version; psql --version'])
    with tempfile.TemporaryDirectory(prefix='sub2api-package-') as temporary:
        context = Path(temporary)
        shutil.copyfile(args.manifest.resolve().parent/'sub2api', context/'sub2api')
        (context/'sub2api').chmod(0o755)
        shutil.copytree(args.manifest.resolve().parent/'resources', context/'resources')
        shutil.copyfile(ROOT/'deploy/Dockerfile.runtime', context/'Dockerfile')
        (context/'.dockerignore').write_text('*\n!Dockerfile\n!sub2api\n!resources/\n!resources/**\n')
        if build.sha(context/'sub2api') != value['binary']['sha256'] or build.files(context/'resources') != value['resources']:
            raise ValueError('packaging context differs from artifact')
        subprocess.run(['docker', 'build', '--network=none', '--pull=false', '--no-cache',
                        '--build-arg', 'RUNTIME_IMAGE='+args.runtime_image,
                        '--build-arg', 'VERSION='+value['version'],
                        '--build-arg', 'COMMIT='+value['source']['commit'],
                        '--build-arg', 'BINARY_SHA256='+value['binary']['sha256'],
                        '-t', args.tag, str(context)], check=True)
    candidate = inspect(args.tag)
    image_contract(base, candidate, value)
    observed = run(probe+[candidate['Id'], '-ec', 'test "$(stat -c %u:%g:%a /app/sub2api)" = 1000:1000:755; sha256sum /app/sub2api'])
    if observed.split()[0] != value['binary']['sha256']:
        raise ValueError('packaged binary hash differs')
    result = {'format': 1, 'image': candidate['Id'], 'runtime_image': base['Id'],
              'manifest_sha256': build.sha(args.manifest), 'commit': value['source']['commit'],
              'binary_sha256': value['binary']['sha256'], 'bytes': candidate['Size'],
              'runtime_layers': len(base['RootFS']['Layers']), 'layers': len(candidate['RootFS']['Layers']),
              'runtime_config_preserved': True, 'previous_application_image_used': False}
    receipt = args.manifest.resolve().parent/('image-'+candidate['Id'][7:19]+'.json')
    with receipt.open('x') as stream:
        stream.write(json.dumps(result, sort_keys=True, indent=2)+'\n')
    print(json.dumps(result))


if __name__ == '__main__':
    main()
