"""Check notice snapshot integrity, not legal compliance or release readiness."""

import hashlib
import json
from pathlib import Path
import sys


def main():
    root = Path(__file__).resolve().parent.parent
    manifest = json.loads((root / 'LICENSES/manifest.json').read_text(encoding='utf-8'))
    errors = []
    for item in manifest['files']:
        relative = item['file']
        target = (root / relative).resolve()
        if not target.is_relative_to(root) or not target.is_file():
            errors.append(f'{relative}: missing or outside the repository')
            continue
        data = target.read_bytes()
        if len(data) != item['bytes'] or hashlib.sha256(data).hexdigest() != item['sha256']:
            errors.append(f'{relative}: notice text differs from the recorded source')
    for relative, expected in manifest.get('inputs', {}).items():
        target = (root / relative).resolve()
        if not target.is_relative_to(root) or not target.is_file():
            errors.append(f'{relative}: missing manifest input')
        # Dependency inputs use canonical LF; legal texts above remain byte-exact.
        elif hashlib.sha256(target.read_bytes().replace(b'\r\n', b'\n')).hexdigest() != expected:
            errors.append(f'{relative}: dependencies changed; review and refresh notices')
    if errors:
        print('\n'.join(errors), file=sys.stderr)
        return 1
    print(f"Notice integrity: {len(manifest['files'])} files verified.")
    print(f"Recorded collection gaps: {len(manifest['gaps'])}; see THIRD_PARTY_NOTICES.md.")
    print('This check does not certify compliance or clear release limitations.')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
