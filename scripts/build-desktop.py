#!/usr/bin/env python3
"""Build desktop installers with portable Windows/Linux executable names."""
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
        elif args.platform.startswith('windows/'):
            # NSIS interprets mygo's BOM-less script using the runner's ANSI
            # code page. ASCII paths avoid mismatched Chinese file names.
            config = json.loads(original)
            config['name'] = 'Xiangqi'
            path.write_text(json.dumps(config, ensure_ascii=False, indent=2) + '\n', encoding='utf-8', newline='\n')
        command = ['go', 'tool', 'mygo', 'build', '-platform', args.platform]
        result = subprocess.run(command, cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                text=True, encoding='utf-8', errors='replace')
        print(result.stdout, end='', flush=True)
        if result.returncode and args.platform.startswith('darwin/') and 'hdiutil detach' in result.stdout:
            # Hosted Macs occasionally keep the temporary DMG volume busy.
            # mygo creates a new staging directory on retry; compilation and
            # other packaging failures still fail immediately.
            print('Retrying transient macOS disk image detach failure', flush=True)
            subprocess.run(command, cwd=ROOT, check=True)
        else:
            result.check_returncode()
    finally:
        path.write_bytes(original)


if __name__ == '__main__':
    main()
