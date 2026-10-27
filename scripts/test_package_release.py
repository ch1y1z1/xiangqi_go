"""Release checks: signed manifests must resolve to the exact uploaded assets."""
import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location('package_release', Path(__file__).with_name('package-release.py'))
package = importlib.util.module_from_spec(spec)
spec.loader.exec_module(package)


class UpdatePackagingTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.build = self.root / 'build/windows-amd64'
        self.build.mkdir(parents=True)
        self.config = {'name': '象棋残局', 'version': '0.1.1', 'out': 'build',
                       'updates': {'github': 'ch1y1z1/xiangqi_go'}}
        self.archive = self.build / 'xiangqi-0.1.1-windows-amd64.tar.gz'
        self.archive.write_bytes(b'archive contents')
        self.manifest = {
            'version': '0.1.1', 'size': self.archive.stat().st_size,
            'url': 'https://github.com/ch1y1z1/xiangqi_go/releases/download/v0.1.1/' + self.archive.name,
            'signature': base64.b64encode(bytes(64)).decode(),
        }
        self.path = self.build / 'update-windows-amd64.json'
        self.write_manifest()

    def write_manifest(self):
        self.path.write_text(json.dumps(self.manifest), encoding='utf-8')

    def test_release_requires_manifest(self):
        self.path.unlink()
        self.assertEqual(package.update_assets(self.build, self.config, 'windows-amd64'), [])
        with self.assertRaisesRegex(SystemExit, 'Missing signed update manifest'):
            package.update_assets(self.build, self.config, 'windows-amd64', required=True)

    def test_rejects_broken_updates_before_upload(self):
        for key, bad in [('version', '0.1.0'), ('size', 99), ('signature', ''),
                         ('url', self.manifest['url'].replace('/v0.1.1/', '/v0.1.0/')),
                         ('url', self.manifest['url'].replace('/xiangqi-', '/nested/xiangqi-')),
                         ('url', self.manifest['url'].replace('windows-amd64', 'darwin-arm64'))]:
            with self.subTest(key=key, bad=bad):
                good = self.manifest[key]
                self.manifest[key] = bad
                self.write_manifest()
                with self.assertRaises(SystemExit):
                    package.update_assets(self.build, self.config, 'windows-amd64', required=True)
                self.manifest[key] = good

    def test_windows_zip_excludes_update_archive_and_preserves_download_name(self):
        (self.root / 'mygo.json').write_text(json.dumps(self.config), encoding='utf-8')
        resources = ['LICENSE', 'Pikafish-Copying.txt', 'Pikafish-AUTHORS',
                     'Pikafish-Network-LICENSE.md', 'Pikafish-Network-Upstream-README.md',
                     'icon.png', 'pikafish.nnue', 'Xiangqi.exe', 'Xiangqi Setup 0.1.1.exe', 'bin/xiangqi-engine.exe']
        for name in resources:
            path = self.build / name
            path.parent.mkdir(exist_ok=True)
            path.write_bytes(b'test resource')
        network_hash = package.digest(self.build / 'pikafish.nnue')
        reply = SimpleNamespace(stdout=json.dumps({'type': 'result', 'result': {'networkSHA256': network_hash}}))
        with patch.object(package, 'ROOT', self.root), patch.object(package, 'NETWORK_SHA256', network_hash), \
                patch.object(package.subprocess, 'run', return_value=reply), \
                patch('sys.argv', ['package-release.py', '--os', 'windows', '--arch', 'amd64', '--require-updates']):
            package.main()
        dist = self.root / 'dist'
        self.assertEqual((dist / self.archive.name).read_bytes(), self.archive.read_bytes())
        self.assertEqual((dist / self.path.name).read_bytes(), self.path.read_bytes())
        with zipfile.ZipFile(dist / 'xiangqi-0.1.1-windows-amd64.zip') as archive:
            self.assertIn('Xiangqi.exe', archive.namelist())
            self.assertIn('pikafish.nnue', archive.namelist())
            self.assertNotIn(self.archive.name, archive.namelist())
            self.assertNotIn(self.path.name, archive.namelist())
        for line in (dist / 'xiangqi-0.1.1-windows-amd64.sha256').read_text().splitlines():
            expected, name = line.split('  ')
            self.assertEqual(expected, hashlib.sha256((dist / name).read_bytes()).hexdigest())


if __name__ == '__main__':
    unittest.main()
