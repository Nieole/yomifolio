"""Publication must retain legal bytes, gate legal delivery and defer only provenance risks."""

import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

import release_build_metadata as metadata
import release_notices as notices

LEGAL = b'Original notice\r\n'
DEFERRED = (
    {'id': 'avif-libyuv-wasi-build-provenance', 'risk': 'prebuilt codec source mapping unverified',
     'evidence': 'avif-build-source-mapping'},
    {'id': 'reproducible-build-attestation', 'risk': 'no attestation record',
     'evidence': 'reproducible-build'},
)


def make_release(root, *, blockers=(), gaps=(), missing_evidence=(), review_version='v1.10.0',
                 metadata_overrides=None):
    """Isolated release fixture: complete legal bytes, recorded blocking evidence, three targets."""
    (root / 'LICENSES').mkdir(parents=True, exist_ok=True)
    for name in notices.ROOT_FILES:
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(LEGAL)
    (root / 'LICENSES/example').write_bytes(LEGAL)
    (root / 'DEPENDENCY_MANIFEST.md').write_text('[text](LICENSES/example)\n', encoding='utf-8')
    (root / 'review.txt').write_bytes(b'review fixture, not actual release evidence')
    record = {'file': 'review.txt', 'sha256': notices.digest(b'review fixture, not actual release evidence')}
    evidence = {category: ([record] if category not in missing_evidence else [])
                for category in notices.BLOCKING_EVIDENCE}
    evidence.update({category: [] for category in ('avif-build-source-mapping', 'reproducible-build')})
    (root / 'LICENSES/release-review.json').write_text(json.dumps({
        'version': review_version, 'blockers': list(blockers),
        'deferred': [dict(item) for item in DEFERRED], 'evidence': evidence,
    }), encoding='utf-8')
    (root / 'LICENSES/manifest.json').write_text(json.dumps({
        'gaps': list(gaps),
        'files': [{'file': 'LICENSES/example', 'bytes': len(LEGAL), 'sha256': notices.digest(LEGAL)}],
        'inputs': {'go.mod': 'input-hash'},
    }), encoding='utf-8')
    for target in notices.TARGETS:
        name = f'yomifolio-v1.10.0-{target}' + ('.exe' if target.startswith('windows') else '')
        (root / name).write_bytes(b'binary fixture')
        data = {
            'version': 'v1.10.0', 'target': target,
            'binary_sha256': notices.digest(b'binary fixture'),
            'go_env': {'GOVERSION': 'go1.27.4', 'CGO_ENABLED': '1'},
            'go_version_m': 'go1.27.4',
            'avif_inputs': {'lib/avif.wasm.gz': '92ffd604d276359c2df829effc615cd9f286c36ece5761644929e4396eb97360'},
            'compiler': {'output': 'compiler version'},
            'dynamic_dependencies': {'output': 'native runtime list'},
            'mpl_unmodified': True, 'inputs': {'go.mod': 'input-hash'},
        }
        data.update(metadata_overrides or {})
        (root / (name + '.metadata.json')).write_text(json.dumps(data), encoding='utf-8')


class ArchiveTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.archive = self.root / 'notices.zip'
        for name in notices.ROOT_FILES:
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(LEGAL)
        (self.root / 'LICENSES').mkdir()
        (self.root / 'LICENSES/example').write_bytes(b'Legal bytes\r\n')
        (self.root / 'DEPENDENCY_MANIFEST.md').write_text('[text](LICENSES/example)\n', encoding='utf-8')

    def test_create_extract_preserves_full_collection_bytes(self):
        notices.create(self.archive, self.root, 'v1.10.0')
        index = notices.verify(self.archive, self.root, 'v1.10.0')
        self.assertEqual(index['files']['LICENSES/example'], notices.digest(b'Legal bytes\r\n'))
        with zipfile.ZipFile(self.archive) as archive:
            self.assertEqual(archive.read('LICENSES/example'), b'Legal bytes\r\n')

    def test_wrong_version_and_changed_bytes_fail(self):
        notices.create(self.archive, self.root, 'v1.10.0')
        with self.assertRaisesRegex(AssertionError, 'version'):
            notices.verify(self.archive, self.root, 'v1.9.0')
        (self.root / 'LICENSES/example').write_bytes(b'Legal bytes\n')
        with self.assertRaises(AssertionError):
            notices.verify(self.archive, self.root, 'v1.10.0')

    def test_dangling_reference_fails(self):
        (self.root / 'DEPENDENCY_MANIFEST.md').write_text('[missing](LICENSES/missing)\n', encoding='utf-8')
        with self.assertRaisesRegex(AssertionError, 'Dangling reference'):
            notices.create(self.archive, self.root, 'v1.10.0')

    def test_added_unsafe_or_duplicate_member_fails_before_extraction(self):
        for member in ('../escape', 'LICENSE'):
            notices.create(self.archive, self.root, 'v1.10.0')
            with zipfile.ZipFile(self.archive, 'a') as archive:
                archive.writestr(member, b'bad')
            with self.assertRaises(AssertionError):
                notices.verify(self.archive, self.root, 'v1.10.0')

    def test_missing_member_fails(self):
        notices.create(self.archive, self.root, 'v1.10.0')
        with zipfile.ZipFile(self.archive) as archive:
            files = {name: archive.read(name) for name in archive.namelist() if name != 'NOTICE'}
        with zipfile.ZipFile(self.archive, 'w') as archive:
            for name, data in files.items():
                archive.writestr(name, data)
        with self.assertRaises(AssertionError):
            notices.verify(self.archive, self.root, 'v1.10.0')


class GateTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def gate(self):
        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr):
            report = notices.gate(self.root, self.root, 'v1.10.0')
        return report, stderr.getvalue()

    def test_unverified_provenance_only_passes_with_deferred_warning(self):
        make_release(self.root)
        report = notices.evaluate(self.root, self.root, 'v1.10.0')
        self.assertEqual(report['blockers'], [])
        self.assertEqual(report['status'], 'passed-with-deferred-risks')
        self.assertEqual([item['id'] for item in report['deferred']], [item['id'] for item in DEFERRED])
        self.assertFalse(any(item['evidence_recorded'] for item in report['deferred']))
        report, stderr = self.gate()
        self.assertEqual(report['status'], 'passed-with-deferred-risks')
        self.assertIn('Deferred non-blocking risk [avif-libyuv-wasi-build-provenance]', stderr)
        self.assertIn('no review evidence recorded', stderr)

    def test_recorded_deferred_evidence_keeps_gate_passing_without_claiming_verification(self):
        make_release(self.root)
        review = json.loads((self.root / 'LICENSES/release-review.json').read_text(encoding='utf-8'))
        review['evidence']['avif-build-source-mapping'] = [
            {'file': 'review.txt', 'sha256': notices.digest((self.root / 'review.txt').read_bytes())}]
        (self.root / 'LICENSES/release-review.json').write_text(json.dumps(review), encoding='utf-8')
        report, stderr = self.gate()
        self.assertEqual(report['status'], 'passed-with-deferred-risks')
        self.assertTrue(report['deferred'][0]['evidence_recorded'])
        self.assertIn('review pending', stderr)

    def test_deferred_evidence_changes_do_not_change_blockers(self):
        label = 'Eight NPM package locations lack applicable full legal texts'
        make_release(self.root, blockers=(label,))
        before = notices.evaluate(self.root, self.root, 'v1.10.0')['blockers']
        review = json.loads((self.root / 'LICENSES/release-review.json').read_text(encoding='utf-8'))
        review['evidence']['reproducible-build'] = [
            {'file': 'review.txt', 'sha256': notices.digest((self.root / 'review.txt').read_bytes())}]
        (self.root / 'LICENSES/release-review.json').write_text(json.dumps(review), encoding='utf-8')
        after = notices.evaluate(self.root, self.root, 'v1.10.0')
        self.assertEqual(after['blockers'], [label])
        self.assertEqual(before, after['blockers'])
        self.assertTrue(any(item['evidence_recorded'] for item in after['deferred']))

    def test_missing_applicable_legal_text_blocks(self):
        make_release(self.root)
        (self.root / 'LICENSES/example').unlink()
        (self.root / 'NOTICE').unlink()
        report = notices.evaluate(self.root, self.root, 'v1.10.0')
        self.assertIn('Missing legal text: LICENSES/example', report['blockers'])
        self.assertIn('Missing legal text: NOTICE', report['blockers'])
        with self.assertRaisesRegex(AssertionError, 'Missing legal text: NOTICE'):
            self.gate()

    def test_changed_legal_bytes_block(self):
        make_release(self.root)
        (self.root / 'LICENSES/example').write_bytes(b'Rewritten notice\r\n')
        report = notices.evaluate(self.root, self.root, 'v1.10.0')
        self.assertIn('Legal text differs from inventory: LICENSES/example', report['blockers'])

    def test_missing_mpl_source_evidence_or_comparison_blocks(self):
        make_release(self.root, missing_evidence=('mpl-source',))
        self.assertIn('Missing review evidence: mpl-source',
                      notices.evaluate(self.root, self.root, 'v1.10.0')['blockers'])
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            make_release(root, metadata_overrides={'mpl_unmodified': False})
            report = notices.evaluate(root, root, 'v1.10.0')
            self.assertTrue(any('MPL source differs' in item for item in report['blockers']), report['blockers'])

    def test_inventory_gaps_and_mismatches_block(self):
        gaps = ('@humanfs/types 0.15.0', '@napi-rs/wasm-runtime 1.2.6',
                '@rolldown/binding-win32-x64-msvc 1.2.19', '@tailwindcss/oxide-wasm32-wasi 4.3.5',
                'nested @napi-rs/wasm-runtime 1.2.2', 'nested @tybys/wasm-util 0.10.3',
                'natural-compare 1.4.0', 'saxes 6.0.0')
        make_release(self.root, gaps=gaps)
        self.assertIn('Unresolved collection gaps: 8',
                      notices.evaluate(self.root, self.root, 'v1.10.0')['blockers'])
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            make_release(root, metadata_overrides={'inputs': {'go.mod': 'other-hash'}})
            report = notices.evaluate(root, root, 'v1.10.0')
            self.assertTrue(any('Dependency metadata differs from notices' in item
                                for item in report['blockers']), report['blockers'])

    def test_broken_sha_and_version_mismatch_block(self):
        make_release(self.root)
        (self.root / 'yomifolio-v1.10.0-linux-amd64').write_bytes(b'tampered')
        (self.root / 'review.txt').write_bytes(b'stale review')
        report = notices.evaluate(self.root, self.root, 'v1.10.0')
        self.assertTrue(any('Binary metadata mismatch' in item for item in report['blockers']))
        self.assertIn('Review evidence changed: review.txt', report['blockers'])
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            make_release(root, review_version='v1.9.0', metadata_overrides={'version': 'v1.9.0'})
            report = notices.evaluate(root, root, 'v1.10.0')
            self.assertIn('Review is not for release v1.10.0: v1.9.0', report['blockers'])
            self.assertTrue(any('Wrong metadata target/version' in item for item in report['blockers']))

    def test_no_status_flag_can_substitute_for_evidence(self):
        make_release(self.root, missing_evidence=notices.BLOCKING_EVIDENCE)
        report = notices.evaluate(self.root, self.root, 'v1.10.0')
        self.assertEqual(report['blockers'],
                         [f'Missing review evidence: {category}' for category in notices.BLOCKING_EVIDENCE])
        self.assertEqual(report['status'], 'blocked')

    def test_repository_stays_blocked_on_legal_gaps_not_provenance(self):
        report = notices.evaluate(notices.ROOT, Path('unused'), 'v1.10.0')
        self.assertEqual(report['status'], 'blocked')
        self.assertTrue(any('Eight NPM package locations' in item for item in report['blockers']))
        self.assertTrue(any('Unresolved collection gaps: 8' in item for item in report['blockers']))
        self.assertFalse(any('libyuv' in item or 'provenance' in item for item in report['blockers']))
        self.assertTrue(any('avif' in item['id'] for item in report['deferred']))
        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr), self.assertRaisesRegex(AssertionError, 'Eight NPM'):
            notices.gate(notices.ROOT, Path('unused'), 'v1.10.0')
        self.assertIn('Deferred non-blocking risk [avif-libyuv-wasi-build-provenance]', stderr.getvalue())

    def test_workflow_download_verify_and_gate_precede_publication(self):
        source = (notices.ROOT / '.github/workflows/release.yml').read_text(encoding='utf-8')
        publish = source.split('\n  publish:\n')[1]
        self.assertIn('needs: [resolve, package, notices]', publish)
        self.assertLess(publish.index('release_notices.py verify'), publish.index('Generate SHA256'))
        self.assertLess(publish.index('release_notices.py gate'), publish.index('action-gh-release'))
        self.assertIn('sha256sum yomifolio-*', publish)
        self.assertIn('path: downloaded-notices', source)
        self.assertIn('--artifacts downloaded-notices', source)
        package = (notices.ROOT / '.github/workflows/package.yml').read_text(encoding='utf-8')
        self.assertIn('release_build_metadata.py', package)
        self.assertIn('build/${{ steps.binary.outputs.name }}.metadata.json', package)
        self.assertNotIn('./cmd/mcp', package)


class BuildSourceTests(unittest.TestCase):
    def test_json_stream(self):
        self.assertEqual(metadata.json_stream(' {"a": 1}\n{"b": 2}\n'), [{'a': 1}, {'b': 2}])

    def test_exact_mpl_source_and_local_modifications(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            module = root / 'module'
            module.mkdir()
            (module / 'lru.go').write_bytes(b'covered source\n')
            archive = root / 'source.zip'
            with zipfile.ZipFile(archive, 'w') as output:
                output.writestr('example@v1/lru.go', b'covered source\n')
            mpl = {'Path': 'example', 'Version': 'v1', 'Dir': str(module)}
            download = {'Zip': str(archive)}
            self.assertEqual(set(metadata.verify_mpl_source(mpl, download)), {'lru.go'})
            for filename in ('lru.go', 'added.go'):
                (module / filename).write_bytes(b'local modification')
                with self.assertRaisesRegex(AssertionError, 'differs'):
                    metadata.verify_mpl_source(mpl, download)


if __name__ == '__main__':
    unittest.main()
