#!/usr/bin/env python3
"""Build prepared helper using a native C++17 compiler (or explicit cross compiler)."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import os
from pathlib import Path
import platform
import shlex
import subprocess

ROOT = Path(__file__).resolve().parents[1]
COMMIT = '4c17cee11f888ae1d48a9494f2e2239f019f0a1f'


def main():
    host_os = {'Darwin': 'darwin', 'Windows': 'windows', 'Linux': 'linux'}.get(platform.system())
    host_arch = {'arm64': 'arm64', 'aarch64': 'arm64', 'AMD64': 'amd64', 'x86_64': 'amd64'}.get(platform.machine())
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--os', choices=['darwin', 'windows', 'linux'], default=host_os)
    parser.add_argument('--arch', choices=['arm64', 'amd64'], default=host_arch)
    parser.add_argument('--cxx', default=os.environ.get('CXX', 'clang++' if host_os == 'darwin' else 'g++'))
    parser.add_argument('--cxxflags', default='', help='Additional compiler/target/sysroot flags')
    parser.add_argument('--ldflags', default='', help='Additional linker flags')
    parser.add_argument('--jobs', type=int, default=min(4, os.cpu_count() or 1))
    args = parser.parse_args()
    if not args.os or not args.arch or args.jobs < 1:
        parser.error('Specify supported --os/--arch and positive --jobs')
    source = ROOT / 'build/engine'
    if not (source / 'prepared-commit.txt').exists() or (source / 'prepared-commit.txt').read_text().strip() != COMMIT:
        parser.error('Run scripts/prepare-engine.py first')
    third = ROOT / 'engine-worker/third_party/nlohmann/json.hpp'
    if hashlib.sha256(third.read_bytes()).hexdigest() != '9bea4c8066ef4a1c206b2be5a36302f8926f7fdc6087af5d20b417d0cf103ea6':
        parser.error('Vendored nlohmann 3.11.3 header checksum mismatch')
    output = ROOT / f'resources/{args.os}-{args.arch}/bin' / ('xiangqi-engine.exe' if args.os == 'windows' else 'xiangqi-engine')
    objdir = ROOT / f'build/engine-worker/{args.os}-{args.arch}'
    objdir.mkdir(parents=True, exist_ok=True)
    output.parent.mkdir(parents=True, exist_ok=True)
    flags = ['-std=c++17', '-O3', '-DNDEBUG', '-DIS_64BIT', '-DZSTD_DISABLE_ASM', '-pthread', '-I', str(source), '-I', str(ROOT / 'engine-worker/third_party')]
    if args.arch == 'arm64':
        flags += ['-DUSE_NEON=8', '-DUSE_POPCNT']
    else:
        flags += ['-DUSE_SSE2', '-msse2']  # Baseline x86-64; no AVX2 or POPCNT requirement.
    if args.os == 'darwin':
        flags += ['-arch', 'arm64' if args.arch == 'arm64' else 'x86_64', '-mmacosx-version-min=14.0']
    flags += shlex.split(args.cxxflags)
    files = sorted(p for p in source.rglob('*.cpp') if p.name != 'main.cpp' and 'universal' not in p.relative_to(source).parts)
    files += [ROOT / 'engine-worker/worker.cpp', ROOT / 'engine-worker/RulesExtension.cpp']
    def compile_one(item):
        index, path = item
        obj = objdir / f'{index}-{path.stem}.o'
        subprocess.run([args.cxx, *flags, '-c', str(path), '-o', str(obj)], check=True)
        return obj
    with ThreadPoolExecutor(max_workers=args.jobs) as pool:
        objects = list(pool.map(compile_one, enumerate(files)))
    temporary = output.with_suffix('.tmp.exe' if args.os == 'windows' else '.tmp')
    link_flags = shlex.split(args.ldflags)
    if args.os == 'windows':
        link_flags += ['-static', '-ladvapi32']  # MinGW-w64; package needs no compiler DLLs.
    subprocess.run([args.cxx, *flags, *map(str, objects), *link_flags, '-o', str(temporary)], check=True)
    temporary.replace(output)
    print(f'Built {output.relative_to(ROOT)} ({len(files)} translation units)')


if __name__ == '__main__':
    main()
