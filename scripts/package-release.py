#!/usr/bin/env python3
"""Verify mygo's bundled resources and collect platform release assets."""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import zipfile
from urllib.parse import unquote, urlsplit

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


def update_assets(build, config, target, required=False):
    """Keep names used by signed manifests; installer renaming must not break URLs."""
    path = build / f'update-{target}.json'
    if not path.exists():
        if required:
            raise SystemExit(f'Missing signed update manifest: {path}; configure MYGO_UPDATER_PRIVATE_KEY')
        return []
    manifest = json.loads(require_file(path).read_text(encoding='utf-8'))
    if manifest['version'] != config['version']:
        raise SystemExit(f'Update manifest version mismatch: {path}')
    base = f'https://github.com/{config["updates"]["github"]}/releases/download/v{config["version"]}/'
    files = [path]
    for entry, suffix in [(manifest, f'-{target}.tar.gz'),
                          *((delta, f'-{target}.delta') for delta in manifest.get('deltas', []))]:
        url = entry['url']
        parsed = urlsplit(url)
        name = unquote(parsed.path.rsplit('/', 1)[-1])
        if (not url.startswith(base) or unquote(url[len(base):]) != name or parsed.query or parsed.fragment or
                not name.endswith(suffix) or '/' in name or '\\' in name):
            raise SystemExit(f'Unexpected update asset URL: {url}')
        asset = require_file(build / name)
        if asset.stat().st_size != entry['size']:
            raise SystemExit(f'Update asset size mismatch: {asset}')
        try:
            signature = base64.b64decode(entry['signature'], validate=True)
        except (ValueError, TypeError):
            signature = b''
        if len(signature) != 64:
            raise SystemExit(f'Missing or invalid update signature: {asset}')
        files.append(asset)
    return files


def release_notes(version):
    lines = (ROOT / 'CHANGELOG.md').read_text(encoding='utf-8').splitlines()
    notes = []
    inside = False
    for line in lines:
        if line.startswith('## '):
            if inside:
                break
            inside = line == f'## {version}'
        elif inside:
            notes.append(line)
    text = '\n'.join(notes).strip()
    if not text:
        raise SystemExit(f'Missing release notes for {version} in CHANGELOG.md')
    return text


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--os', choices=['darwin', 'windows', 'linux'])
    parser.add_argument('--arch', choices=['arm64', 'amd64'])
    parser.add_argument('--check-version', metavar='TAG', help='Only validate tag against mygo.json')
    parser.add_argument('--require-updates', action='store_true', help='Fail without signed update artifacts')
    parser.add_argument('--release-notes', type=Path, help='Write current changelog section and installation notes')
    args = parser.parse_args()
    config = json.loads((ROOT / 'mygo.json').read_text(encoding='utf-8'))
    version = config['version']
    if args.release_notes:
        notes = release_notes(version)
        installation = (ROOT / '.github/release-notes.md').read_text(encoding='utf-8')
        args.release_notes.write_text(notes + '\n\n---\n\n' + installation, encoding='utf-8')
        return
    if args.check_version:
        if args.check_version != f'v{version}':
            parser.error(f'Tag {args.check_version!r} must match mygo.json version v{version}')
        release_notes(version)
        print(f'Validated release version {args.check_version}')
        return
    if not args.os or not args.arch:
        parser.error('--os and --arch are required for packaging')

    build = ROOT / config['out'] / f'{args.os}-{args.arch}'
    updates = update_assets(build, config, f'{args.os}-{args.arch}', args.require_updates)
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
        require_file(bundle / 'Xiangqi.exe')
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

    def copy_asset(source, name):
        target = dist / name
        if target in assets:
            return
        shutil.copyfile(source, target)
        assets.append(target)

    def collect(source, suffix):
        copy_asset(source, prefix + suffix)

    if args.os == 'darwin':
        collect(exactly_one(build.glob('*.dmg')), '.dmg')
    elif args.os == 'windows':
        installer = exactly_one(build.glob('* Setup *.exe'))
        collect(installer, '-setup.exe')
        archive = dist / (prefix + '.zip')
        with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED, compresslevel=9) as output:
            for path in sorted(build.rglob('*')):
                if path.is_file() and path != installer and path not in updates:
                    output.write(path, arcname=path.relative_to(build).as_posix())
        assets.append(archive)
    else:
        collect(exactly_one(build.glob('*.deb')), '.deb')
        archive = next((p for p in updates if p.name.endswith('.tar.gz')), None)
        collect(archive or exactly_one(build.glob('*.tar.gz')), '.tar.gz')

    for path in updates:
        copy_asset(path, path.name)

    checksums = dist / (prefix + '.sha256')
    checksums.write_text(''.join(f'{digest(path)}  {path.name}\n' for path in assets), encoding='utf-8', newline='\n')
    for path in assets:
        print(f'{path.relative_to(ROOT)} ({path.stat().st_size:,} bytes)')


if __name__ == '__main__':
    main()
