"""Create and round-trip verify same-version legal-text archives; gate legal delivery, not provenance."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
ROOT_FILES = ('LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES.md', 'DEPENDENCY_MANIFEST.md',
              'images/README.md')
TARGETS = ('linux-amd64', 'windows-amd64', 'darwin-arm64')
# Human legal review categories: reported as residual risks, never accepted as clearance.
REVIEW_CATEGORIES = ('npm-legal-texts', 'native-runtime-notices', 'mpl-source')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def payload(root):
    paths = list(ROOT_FILES) + [p.relative_to(root).as_posix()
                               for p in sorted((root / 'LICENSES').rglob('*')) if p.is_file()]
    return {name: (root / name).read_bytes() for name in paths}


def verify(archive, root, version):
    expected = payload(root)
    with zipfile.ZipFile(archive) as source, tempfile.TemporaryDirectory() as tmp:
        names = source.namelist()
        assert len(names) == len(set(names)), 'Duplicate archive paths'
        assert set(names) == set(expected) | {'RELEASE-NOTICES.json'}, 'Unexpected archive contents'
        # Only extract names from the trusted expected set, after checking the entire member set.
        for name in names:
            assert not Path(name).is_absolute() and '..' not in Path(name).parts, 'Unsafe path'
        source.extractall(tmp)
        extracted = Path(tmp)
        index = json.loads((extracted / 'RELEASE-NOTICES.json').read_text(encoding='utf-8'))
        assert index['version'] == version, 'Wrong notice version'
        assert index['files'] == {name: digest(data) for name, data in expected.items()}, 'Wrong index'
        for name, data in expected.items():
            assert (extracted / name).read_bytes() == data, f'Changed bytes: {name}'
        for name in ROOT_FILES:
            if not name.endswith('.md'):
                continue
            for target in re.findall(r'\]\(([^)]+)\)', (extracted / name).read_text(encoding='utf-8')):
                if '://' in target or target.startswith('#'):
                    continue
                target = target.split('#', 1)[0]
                resolved = (extracted / name).parent / target
                assert resolved.resolve().is_relative_to(extracted.resolve()) and resolved.exists(), \
                    f'Dangling reference: {name}: {target}'
    return index


def create(archive, root, version):
    assert re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._+-]*', version), 'Unsafe version'
    files = payload(root)
    archive.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as output:
        for name, data in files.items():
            output.writestr(name, data)
        output.writestr('RELEASE-NOTICES.json', json.dumps({
            'version': version,
            'scope': 'Legal texts and source-access instructions, NOT a source archive or legal clearance',
            'files': {name: digest(data) for name, data in files.items()},
        }, indent=2) + '\n')
    verify(archive, root, version)


def evidence_failures(root, review, category):
    """Return (recorded count, failures) for content-bound evidence in one category."""
    records = review['evidence'].get(category, [])
    failures = []
    for record in records:
        path = (root / record['file']).resolve()
        if not path.is_relative_to(root.resolve()):
            failures.append(f'Review evidence outside repository: {record["file"]}')
        elif not path.is_file():
            failures.append(f'Missing review evidence file: {record["file"]}')
        elif digest(path.read_bytes()) != record['sha256']:
            failures.append(f'Review evidence changed: {record["file"]}')
    return len(records), failures


def legal_text_failures(root, manifest):
    """Applicable legal bytes must exist and match the recorded inventory; this is not a review."""
    failures = []
    for name in ROOT_FILES:
        if not (root / name).is_file():
            failures.append(f'Missing legal text: {name}')
    for item in manifest['files']:
        path = (root / item['file']).resolve()
        if not path.is_relative_to(root.resolve()) or not path.is_file():
            failures.append(f'Missing legal text: {item["file"]}')
        elif digest(path.read_bytes()) != item['sha256']:
            failures.append(f'Legal text differs from inventory: {item["file"]}')
    return failures


def check_artifact(artifacts, manifest, version, target, commit):
    """Check one target's actual binary, metadata and notice inventory correspondence."""
    name = f'yomifolio-{version}-{target}' + ('.exe' if target.startswith('windows') else '')
    binary = artifacts / name
    metadata = json.loads((artifacts / (name + '.metadata.json')).read_text(encoding='utf-8'))
    assert metadata['version'] == version and metadata['target'] == target, 'Wrong metadata target/version'
    if commit is not None:
        assert metadata['commit'] == commit, 'Build metadata is for a different source commit'
    assert binary.is_file(), 'Missing release binary'
    assert metadata['binary_sha256'] == digest(binary.read_bytes()), 'Binary metadata mismatch'
    assert metadata['go_env']['GOVERSION'] == 'go1.27.4', 'Unexpected release Go runtime'
    assert 'go1.27.4' in metadata['go_version_m'], 'Binary runtime differs from metadata'
    assert metadata['avif_inputs']['lib/avif.wasm.gz'] == \
        '92ffd604d276359c2df829effc615cd9f286c36ece5761644929e4396eb97360', 'Unexpected AVIF payload'
    # Read the binary's own build settings: `go env` on the runner is not what built the artifact.
    for setting in ('CGO_ENABLED=1', f'GOOS={target.split("-")[0]}',
                    f'GOARCH={target.split("-")[1]}'):
        assert re.search(rf'(?m)^\s*build\s+{re.escape(setting)}$', metadata['go_version_m']), \
            f'Binary build settings lack {setting}'
    assert metadata['compiler']['output'].strip(), 'Missing compiler identification'
    assert metadata['dynamic_dependencies']['output'].strip(), 'Missing native dependency inventory'
    assert metadata['mpl_unmodified'], 'MPL source differs from exact module archive'
    for relative, expected in manifest['inputs'].items():
        assert metadata['inputs'][relative] == expected, 'Dependency metadata differs from notices'


def artifact_failures(artifacts, manifest, version, commit):
    failures = []
    for target in TARGETS:
        name = f'yomifolio-{version}-{target}' + ('.exe' if target.startswith('windows') else '')
        try:
            check_artifact(artifacts, manifest, version, target, commit)
        except (AssertionError, OSError, ValueError, KeyError, TypeError) as error:
            failures.append(f'{name}: {error}')
    return failures


def evaluate(root, artifacts, version, commit=None):
    """Fail closed on integrity and delivery defects; report legal and provenance gaps as risks."""
    review = json.loads((root / 'LICENSES/release-review.json').read_text(encoding='utf-8'))
    manifest = json.loads((root / 'LICENSES/manifest.json').read_text(encoding='utf-8'))
    blockers = []
    if review['version'] != version:
        blockers.append(f'Review is not for release {version}: {review["version"]}')
    blockers += legal_text_failures(root, manifest)
    residual = [{'id': item['id'], 'risk': item['risk']}
                for item in review.get('residual_risks', [])]
    for gap in manifest['gaps']:
        residual.append({'id': 'unresolved-collection-gap',
                         'risk': f'No applicable full legal text recorded: {gap}'})
    adopted = {}
    for category in REVIEW_CATEGORIES:
        recorded, failures = evidence_failures(root, review, category)
        # Tampered evidence is an integrity defect even where the review itself is a residual risk.
        blockers += failures
        adopted[category] = [record['file'] for record in review['evidence'].get(category, [])]
        if not recorded:
            residual.append({'id': f'missing-review-{category}',
                             'risk': f'No recorded review evidence for {category}'})
    deferred = []
    for item in review.get('deferred', []):
        recorded, failures = evidence_failures(root, review, item['evidence'])
        blockers += failures
        deferred.append({'id': item['id'], 'risk': item['risk'], 'evidence_recorded': bool(recorded)})
    blockers += artifact_failures(artifacts, manifest, version, commit)
    if blockers:
        status = 'blocked'
    elif deferred:
        status = 'passed-with-deferred-risks'
    elif residual:
        status = 'passed-with-residual-risks'
    else:
        status = 'passed'
    return {'version': version, 'status': status, 'blockers': blockers, 'residual': residual,
            'deferred': deferred, 'adopted_evidence': adopted}


def gate(root, artifacts, version, commit=None):
    """Fail closed on integrity and delivery defects; report legal and provenance gaps as risks."""
    report = evaluate(root, artifacts, version, commit)
    for category, files in report['adopted_evidence'].items():
        print(f'Review evidence adopted [{category}]: ' + (', '.join(files) or 'none'), file=sys.stderr)
    for item in report['residual']:
        print(f"Residual non-blocking risk [{item['id']}]: {item['risk']}", file=sys.stderr)
    for item in report['deferred']:
        state = 'evidence recorded, review pending' if item['evidence_recorded'] else 'no review evidence recorded'
        print(f"Deferred non-blocking risk [{item['id']}] ({state}): {item['risk']}", file=sys.stderr)
    assert not report['blockers'], 'Unresolved release blockers: ' + '; '.join(report['blockers'])
    print(f"Release gate: {report['status']}; residual legal risks: {len(report['residual'])}; "
          f'deferred provenance risks: ' + (', '.join(item['id'] for item in report['deferred']) or 'none'))
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('create', 'verify', 'gate'))
    parser.add_argument('--version', required=True)
    parser.add_argument('--artifacts', type=Path, default=Path('release-artifacts'))
    args = parser.parse_args()
    if args.action == 'gate':
        commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        gate(ROOT, args.artifacts, args.version, commit)
        print('Notice gate passed on delivery and integrity only; residual legal risks are listed above, '
              'not cleared.')
    else:
        archive = args.artifacts / f'yomifolio-{args.version}-notices.zip'
        if args.action == 'create':
            subprocess.run([sys.executable, str(ROOT / 'scripts/check-license-notices.py')], check=True)
            create(archive, ROOT, args.version)
        else:
            verify(archive, ROOT, args.version)
        print(f'Notice {args.action} passed (not legal clearance).')


if __name__ == '__main__':
    main()
