"""Record actual native release inputs for review without dumping runner environment."""

import argparse
import hashlib
import json
from pathlib import Path
import shlex
import subprocess
import zipfile

from release_notices import digest

ROOT = Path(__file__).resolve().parents[1]


def run(*command):
    return subprocess.check_output(command, cwd=ROOT).decode('utf-8', errors='replace')


def json_stream(text):
    decoder = json.JSONDecoder()
    result = []
    while text.strip():
        text = text.lstrip()
        value, end = decoder.raw_decode(text)
        result.append(value)
        text = text[end:]
    return result


def verify_mpl_source(mpl, download):
    prefix = mpl['Path'] + '@' + mpl['Version'] + '/'
    with zipfile.ZipFile(download['Zip']) as archive:
        files = {name[len(prefix):]: digest(archive.read(name)) for name in archive.namelist()
                 if name.startswith(prefix) and not name.endswith('/')}
    actual = {p.relative_to(mpl['Dir']).as_posix(): digest(p.read_bytes())
              for p in Path(mpl['Dir']).rglob('*') if p.is_file()}
    assert files and actual == files, 'MPL local module source differs from exact source archive'
    return files


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=Path)
    parser.add_argument('--version', required=True)
    args = parser.parse_args()
    env = json.loads(run('go', 'env', '-json', 'GOOS', 'GOARCH', 'GOVERSION', 'CGO_ENABLED',
                         'CC', 'CXX', 'CGO_CFLAGS', 'CGO_CPPFLAGS', 'CGO_CXXFLAGS', 'CGO_LDFLAGS'))
    modules = json_stream(run('go', 'list', '-m', '-json', 'all'))
    # Compare every covered source byte used locally with the exact versioned archive.
    mpl = next(m for m in modules if m['Path'] == 'github.com/hashicorp/golang-lru/v2')
    assert mpl['Version'] == 'v2.0.7' and 'Replace' not in mpl, 'Unexpected MPL version/replacement'
    download = json.loads(run('go', 'mod', 'download', '-json', mpl['Path'] + '@' + mpl['Version']))
    files = verify_mpl_source(mpl, download)
    avif = next(m for m in modules if m['Path'] == 'github.com/gen2brain/avif')
    assert avif['Version'] == 'v0.6.0' and 'Replace' not in avif, 'Unexpected AVIF source'
    wasm = {name: digest((Path(avif['Dir']) / name).read_bytes())
            for name in ('lib/avif.wasm.gz', 'lib/avif.wasm', 'lib/Makefile', 'lib/toolchain.cmake', 'lib/wasi.meson')}
    compiler = shlex.split(env['CC'])
    if env['GOOS'] == 'darwin':
        dynamic = ['otool', '-L', str(args.binary)]
    elif env['GOOS'] == 'windows':
        dynamic = ['objdump', '-p', str(args.binary)]
    else:
        dynamic = ['ldd', str(args.binary)]
    manifest = json.loads((ROOT / 'LICENSES/manifest.json').read_text(encoding='utf-8'))
    goroot = Path(run('go', 'env', 'GOROOT').strip())
    runtime = {}
    for name in ('LICENSE', 'PATENTS'):
        data = (goroot / name).read_bytes()
        relative = f'LICENSES/go-runtime/{env["GOVERSION"]}/{name}'
        assert data == (ROOT / relative).read_bytes(), 'Actual Go runtime legal text differs'
        runtime[relative] = digest(data)
    result = {
        'version': args.version, 'commit': run('git', 'rev-parse', 'HEAD').strip(),
        'target': env['GOOS'] + '-' + env['GOARCH'], 'binary_sha256': digest(args.binary.read_bytes()),
        'go_env': env, 'go_version_m': run('go', 'version', '-m', str(args.binary)),
        'modules': modules, 'packages': json_stream(run('go', 'list', '-deps', '-json', './cmd/server')),
        'compiler': {'command': compiler + ['--version'], 'output': run(*compiler, '--version')},
        'dynamic_dependencies': {'command': dynamic, 'output': run(*dynamic)},
        'runtime_notices': runtime, 'avif_inputs': wasm,
        'wasi_provenance': 'Unresolved prebuilt toolchain/source mapping; recipe is not payload provenance',
        'mpl_unmodified': True, 'mpl_source': {k: download[k] for k in ('Path', 'Version', 'Sum', 'GoModSum')},
        'mpl_source_zip_sha256': digest(Path(download['Zip']).read_bytes()),
        'mpl_source_files': files,
        'inputs': {name: hashlib.sha256((ROOT / name).read_bytes().replace(b'\r\n', b'\n')).hexdigest()
                   for name in manifest['inputs']},
        'frontend_files': {p.relative_to(ROOT / 'web/dist').as_posix(): digest(p.read_bytes())
                           for p in sorted((ROOT / 'web/dist').rglob('*')) if p.is_file()},
        'review_required': 'Native compiler support libraries, dynamic runtimes, Web emission and WASI notices need review',
    }
    Path(str(args.binary) + '.metadata.json').write_text(json.dumps(result, indent=2) + '\n', encoding='utf-8')


if __name__ == '__main__':
    main()
