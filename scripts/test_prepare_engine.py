"""Offline regression tests for pinned engine source preparation."""
import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('prepare_engine', Path(__file__).with_name('prepare-engine.py'))
prepare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare)


class CheckoutSourceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.origin = self.root / 'origin'
        self.origin.mkdir()
        self.git(self.origin, 'init', '-q')
        self.git(self.origin, 'config', 'user.name', 'CI test')
        self.git(self.origin, 'config', 'user.email', 'ci@example.test')
        self.git(self.origin, 'config', 'core.autocrlf', 'false')
        (self.origin / 'source.cpp').write_text('pinned\n', encoding='utf-8')
        self.git(self.origin, 'add', '.')
        self.git(self.origin, 'commit', '-qm', 'pinned source')
        self.commit = self.git(self.origin, 'rev-parse', 'HEAD').strip()
        (self.origin / 'source.cpp').write_text('newer upstream\n', encoding='utf-8')
        self.git(self.origin, 'commit', '-qam', 'newer source')
        self.checkout = self.root / 'vendor/Pikafish'
        self.patches = patch.multiple(prepare, SOURCE_URL=str(self.origin), COMMIT=self.commit)
        self.patches.start()
        self.addCleanup(self.patches.stop)

    def git(self, directory, *args):
        return subprocess.check_output(['git', '-C', str(directory), *args], text=True, stderr=subprocess.DEVNULL)

    def test_fresh_clone_checks_out_pinned_commit_and_can_be_reused_offline(self):
        prepare.checkout_source(self.checkout, offline=False)
        self.assertEqual(self.git(self.checkout, 'rev-parse', 'HEAD').strip(), self.commit)
        self.assertEqual((self.checkout / 'source.cpp').read_text(), 'pinned\n')
        prepare.checkout_source(self.checkout, offline=True)
        self.assertEqual(self.git(self.checkout, 'status', '--porcelain'), '')

    def test_existing_local_changes_are_preserved(self):
        prepare.checkout_source(self.checkout, offline=False)
        (self.checkout / 'source.cpp').write_text('local change\n', encoding='utf-8')
        with self.assertRaisesRegex(SystemExit, 'modified Pikafish checkout'):
            prepare.checkout_source(self.checkout, offline=False)
        self.assertEqual((self.checkout / 'source.cpp').read_text(), 'local change\n')

    def test_offline_without_checkout_does_not_clone(self):
        with self.assertRaisesRegex(SystemExit, 'Offline preparation requires'):
            prepare.checkout_source(self.checkout, offline=True)
        self.assertFalse(self.checkout.exists())


if __name__ == '__main__':
    unittest.main()
