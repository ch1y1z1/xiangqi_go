#!/usr/bin/env python3
"""Fetch pinned Pikafish, apply checked local-memory/rules patches, verify NNUE."""
import argparse
import hashlib
from pathlib import Path
import shutil
import subprocess
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
COMMIT = '4c17cee11f888ae1d48a9494f2e2239f019f0a1f'
SHA256 = '7d13d73569a9b571ba0eb20cf1596247bc2a42738967e61afef6482b231e900e'
URL = 'https://github.com/official-pikafish/Pikafish/releases/download/Pikafish-2026-09-06/Pikafish.2026-09-06.7z'


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def replace_once(path, before, after):
    text = path.read_text()
    if text.count(before) != 1:
        raise SystemExit(f'Patch requires review: {path.name}')
    path.write_text(text.replace(before, after), encoding='utf-8')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--network', type=Path, help='Use an existing compatible NNUE (SHA checked)')
    parser.add_argument('--offline', action='store_true', help='Require existing pinned checkout and NNUE; never download')
    args = parser.parse_args()
    upstream = ROOT / 'vendor-engine/Pikafish'
    if not (upstream / '.git').exists():
        if args.offline:
            parser.error('Offline preparation requires vendor-engine/Pikafish')
        upstream.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(['git', 'clone', '--no-checkout', 'https://github.com/official-pikafish/Pikafish.git', str(upstream)], check=True)
    if subprocess.check_output(['git', '-C', str(upstream), 'status', '--porcelain'], text=True).strip():
        parser.error('Refusing to overwrite a modified Pikafish checkout')
    if subprocess.run(['git', '-C', str(upstream), 'cat-file', '-e', COMMIT], capture_output=True).returncode:
        if args.offline:
            parser.error('Pinned commit is unavailable offline')
        subprocess.run(['git', '-C', str(upstream), 'fetch', 'origin', COMMIT], check=True)
    subprocess.run(['git', '-C', str(upstream), 'checkout', '--detach', COMMIT], check=True)
    destination = ROOT / 'build/engine'
    if destination.exists():
        shutil.rmtree(destination)
    shutil.copytree(upstream / 'src', destination)
    replace_once(destination / 'position.h', '#include <utility>', '#include <utility>\n#include <vector>')
    replace_once(destination / 'position.h', '    u16   chased(Color c);',
                 '    u16   chased(Color c);\n    std::vector<Move> safe_captures(Color color);')
    # Disable Unix socket/shared memory support entirely. Windows also selects the
    # existing local allocator, without changing unrelated Windows platform code.
    shared = destination / 'shm.h'
    original = shared.read_text()
    start = original.index('#if (defined(__linux__)')
    end = original.index('    #define USE_UNIX_SHM', start)
    shared.write_text(original[:start] + '#if 0 // Xiangqi desktop: process-local memory only.\n' + original[end:])
    replace_once(shared, '''        SharedMemoryBackend<T> shm_backend(shm_name, value);

        if (shm_backend.is_valid())
        {
            backend = std::move(shm_backend);
        }
        else
        {
            backend = SharedMemoryBackendFallback<T>(shm_name, value);
        }''', '        backend = SharedMemoryBackendFallback<T>(shm_name, value);')
    resource = ROOT / 'resources/pikafish.nnue'
    resource.parent.mkdir(exist_ok=True)
    candidate = args.network.resolve() if args.network else resource
    if not candidate.exists():
        if args.offline or args.network:
            parser.error(f'Missing NNUE: {candidate}')
        extractor = shutil.which('7z') or shutil.which('7zz')
        if not extractor:
            parser.error('Install 7-Zip or supply --network /path/to/pikafish.nnue')
        archive = ROOT / 'build/Pikafish.2026-09-06.7z'
        if not archive.exists():
            partial = archive.with_suffix('.download')
            print('Downloading pinned official release...', flush=True)
            urllib.request.urlretrieve(URL, partial)
            partial.replace(archive)
        extract = ROOT / 'build/network-download'
        subprocess.run([extractor, 'x', str(archive), '-y', '-r', 'pikafish.nnue', f'-o{extract}'], check=True)
        candidates = list(extract.rglob('pikafish.nnue'))
        if len(candidates) != 1:
            parser.error('Release must contain exactly one pikafish.nnue')
        candidate = candidates[0]
    if digest(candidate) != SHA256:
        parser.error('NNUE checksum mismatch; expected the pinned Pikafish-2026-09-06 model')
    if candidate.resolve() != resource.resolve():
        temporary = resource.with_suffix('.nnue.tmp')
        shutil.copyfile(candidate, temporary)
        temporary.replace(resource)
    for name in ['Copying.txt', 'AUTHORS']:
        shutil.copyfile(upstream / name, ROOT / 'resources' / ('Pikafish-' + name))
    (destination / 'prepared-commit.txt').write_text(COMMIT + '\n')
    print(f'Prepared {COMMIT}; verified NNUE {SHA256}', flush=True)


if __name__ == '__main__':
    main()
