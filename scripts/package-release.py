#!/usr/bin/env python3
"""Verify mygo's bundled resources and collect platform release assets."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]
NETWORK_SHA256 = '7d13d73569a9b571ba0eb20cf1596247bc2a42738967e61afef6482b231e900e'


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def require_file(path):
    if not path.is_file() or path.stat().st_size == 0:
        raise SystemExit(f'Missing or empty release file: {path}')
    return path


def exactly_one(paths):
    paths = list(paths)
    if len(paths) != 1:
        raise SystemExit(f'Expected exactly one installer/archive, found {paths}')
    return require_file(paths[0])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--os', choices=['darwin', 'windows', 'linux'])
    parser.add_argument('--arch', choices=['arm64', 'amd64'])
    parser.add_argument('--check-version', metavar='TAG', help='Only validate tag against mygo.json')
    args = parser.parse_args()
    config = json.loads((ROOT / 'mygo.json').read_text(encoding='utf-8'))
    version = config['version']
    if args.check_version:
        if args.check_version != f'v{version}':
            parser.error(f'Tag {args.check_version!r} must match mygo.json version v{version}')
        print(f'Validated release version {args.check_version}')
        return
    if not args.os or not args.arch:
        parser.error('--os and --arch are required for packaging')

    build = ROOT / config['out'] / f'{args.os}-{args.arch}'
    bundle = build / f'{config["name"]}.app' if args.os == 'darwin' else build
    resources = bundle / 'Contents/Resources' if args.os == 'darwin' else bundle
    worker = require_file(resources / 'bin' / ('xiangqi-engine.exe' if args.os == 'windows' else 'xiangqi-engine'))
    network = require_file(resources / 'pikafish.nnue')
    if digest(network) != NETWORK_SHA256:
        raise SystemExit('Bundled NNUE checksum mismatch')
    for name in ['LICENSE', 'Pikafish-Copying.txt', 'Pikafish-AUTHORS', 'Pikafish-Network-LICENSE.md',
                 'Pikafish-Network-Upstream-README.md', 'icon.png']:
        require_file(resources / name)
    if args.os == 'darwin':
        require_file(bundle / 'Contents/MacOS' / config['name'])
        subprocess.run(['codesign', '--verify', '--deep', '--strict', str(bundle)], check=True)
    elif args.os == 'windows':
        require_file(bundle / f'{config["name"]}.exe')
    else:
        # mygo derives the Linux executable's slug from the display name.
        executables = [p for p in bundle.iterdir() if p.is_file() and not p.suffix and p.stat().st_mode & 0o111]
        exactly_one(executables)

    # Smoke-test the copied helper from its installed location as well as the
    # source helper tested by Go. This catches wrong resource paths/permissions.
    hello = {'id': 'package-check', 'version': 1, 'op': 'hello', 'networkPath': str(network.resolve())}
    result = subprocess.run([str(worker)], input=json.dumps(hello) + '\n', text=True,
                            capture_output=True, check=True, timeout=30)
    replies = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
    if len(replies) != 1 or replies[0].get('type') != 'result' or replies[0].get('result', {}).get('networkSHA256') != NETWORK_SHA256:
        raise SystemExit('Bundled engine failed protocol/model identity check')

    dist = ROOT / 'dist'
    dist.mkdir(exist_ok=True)
    platform_name = 'macos' if args.os == 'darwin' else args.os
    prefix = f'xiangqi-{version}-{platform_name}-{args.arch}'
    assets = []

    def collect(source, suffix):
        target = dist / (prefix + suffix)
        shutil.copyfile(source, target)
        assets.append(target)

    if args.os == 'darwin':
        collect(exactly_one(build.glob('*.dmg')), '.dmg')
    elif args.os == 'windows':
        installer = exactly_one(build.glob('* Setup *.exe'))
        collect(installer, '-setup.exe')
        archive = dist / (prefix + '.zip')
        with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED, compresslevel=9) as output:
            for path in sorted(build.rglob('*')):
                if path.is_file() and path != installer:
                    output.write(path, arcname=path.relative_to(build).as_posix())
        assets.append(archive)
    else:
        collect(exactly_one(build.glob('*.deb')), '.deb')
        collect(exactly_one(build.glob('*.tar.gz')), '.tar.gz')

    checksums = dist / (prefix + '.sha256')
    checksums.write_text(''.join(f'{digest(path)}  {path.name}\n' for path in assets), encoding='utf-8')
    for path in assets:
        print(f'{path.relative_to(ROOT)} ({path.stat().st_size:,} bytes)')


if __name__ == '__main__':
    main()
