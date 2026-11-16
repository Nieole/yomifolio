"""Regression tests for the offline notice-integrity checker."""

import hashlib
import json
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import sys
import tempfile
import unittest


class NoticeIntegrityTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / 'scripts').mkdir()
        (self.root / 'LICENSES').mkdir()
        self.script = self.root / 'scripts/check-license-notices.py'
        shutil.copyfile(Path(__file__).with_name('check-license-notices.py'), self.script)
        self.legal = self.root / 'LICENSES/example.txt'
        self.legal.write_bytes(b'Original legal text\n')
        self.input = self.root / 'go.mod'
        self.input.write_bytes(b'module example\n')
        self.manifest = {
            'files': [{
                'file': 'LICENSES/example.txt',
                'bytes': self.legal.stat().st_size,
                'sha256': hashlib.sha256(self.legal.read_bytes()).hexdigest(),
            }],
            'inputs': {'go.mod': hashlib.sha256(self.input.read_bytes()).hexdigest()},
            'gaps': ['Example unresolved provenance'],
        }

    def run_check(self):
        (self.root / 'LICENSES/manifest.json').write_text(json.dumps(self.manifest), encoding='utf-8')
        return subprocess.run([sys.executable, str(self.script)], capture_output=True, text=True)

    def test_exact_snapshot_passes_without_claiming_compliance(self):
        result = self.run_check()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('1 files verified', result.stdout)
        self.assertIn('Recorded collection gaps: 1', result.stdout)
        self.assertIn('does not certify compliance', result.stdout)

    def test_changed_legal_text_fails(self):
        self.legal.write_bytes(b'Altered legal text\n')
        result = self.run_check()
        self.assertEqual(result.returncode, 1)
        self.assertIn('differs from the recorded source', result.stderr)

    def test_missing_legal_text_fails(self):
        self.legal.unlink()
        result = self.run_check()
        self.assertEqual(result.returncode, 1)
        self.assertIn('missing or outside', result.stderr)

    def test_changed_dependency_input_fails(self):
        self.input.write_bytes(b'module changed\n')
        result = self.run_check()
        self.assertEqual(result.returncode, 1)
        self.assertIn('dependencies changed', result.stderr)

    def test_outside_manifest_path_fails(self):
        self.manifest['files'][0]['file'] = '../outside.txt'
        result = self.run_check()
        self.assertEqual(result.returncode, 1)
        self.assertIn('outside the repository', result.stderr)

    def test_outside_input_path_fails(self):
        self.manifest['inputs'] = {'../outside.mod': 'not-a-hash'}
        result = self.run_check()
        self.assertEqual(result.returncode, 1)
        self.assertIn('missing manifest input', result.stderr)


class RepositoryManifestTests(unittest.TestCase):
    def test_real_manifest_paths_use_portable_posix_separators(self):
        root = Path(__file__).resolve().parent.parent
        manifest = json.loads((root / 'LICENSES/manifest.json').read_text(encoding='utf-8'))
        paths = [item['file'] for item in manifest['files']] + list(manifest['inputs'])
        self.assertIn('web/package-lock.json', manifest['inputs'])
        for relative in paths:
            with self.subTest(path=relative):
                self.assertNotIn('\\', relative)
                path = PurePosixPath(relative)
                self.assertFalse(path.is_absolute())
                self.assertNotIn('..', path.parts)
                self.assertTrue(root.joinpath(*path.parts).is_file())


if __name__ == '__main__':
    unittest.main()
