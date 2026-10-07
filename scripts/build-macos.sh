#!/usr/bin/env bash
set -euo pipefail

task_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$task_root"
if [[ "$(uname -s)" != Darwin ]]; then
  echo 'Run this script on macOS with Xcode Command Line Tools installed.' >&2
  exit 1
fi
case "$(uname -m)" in
  arm64) task_arch=arm64 ;;
  x86_64) task_arch=amd64 ;;
  *) echo 'Unsupported Mac architecture.' >&2; exit 1 ;;
esac
task_helper="resources/darwin-$task_arch/bin/xiangqi-engine"
if [[ ! -f resources/pikafish.nnue || ! -x "$task_helper" || engine-worker/worker.cpp -nt "$task_helper" || engine-worker/RulesExtension.cpp -nt "$task_helper" || scripts/prepare-engine.py -nt "$task_helper" || scripts/build-engine.py -nt "$task_helper" ]]; then
  python3 scripts/prepare-engine.py
  python3 scripts/build-engine.py --os darwin --arch "$task_arch"
fi
go tool mygo build -platform "darwin/$task_arch" -skip-dmg
echo "App: $task_root/build/darwin-$task_arch/象棋残局.app"
