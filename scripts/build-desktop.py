#!/usr/bin/env python3
"""Build desktop installers with a unique Linux package/executable name."""
import argparse
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--platform', required=True,
                        choices=['darwin/arm64', 'darwin/amd64', 'windows/amd64', 'linux/amd64'])
    args = parser.parse_args()
    path = ROOT / 'mygo.json'
    original = path.read_bytes()
    try:
        if args.platform.startswith('linux/'):
            config = json.loads(original)
            # mygo turns an entirely Chinese name into the generic slug "app".
            # Keep the launcher label Chinese and give Debian/paths a unique slug.
            config['name'] = '象棋残局 (Xiangqi)'
            path.write_text(json.dumps(config, ensure_ascii=False, indent=2) + '\n', encoding='utf-8', newline='\n')
        subprocess.run(['go', 'tool', 'mygo', 'build', '-platform', args.platform], cwd=ROOT, check=True)
    finally:
        path.write_bytes(original)


if __name__ == '__main__':
    main()
