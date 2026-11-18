"""Standard-library Mihon legacy index builder; never accepts debug APKs for publication."""
import argparse
import hashlib
import json
import re
import shutil
import subprocess
from pathlib import Path

PACKAGE = "eu.kanade.tachiyomi.extension.all.mangamanager"
WEBSITE = "https://github.com/Nieole/yomifolio"


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True).stdout


def inspect(apk, tools):
    suffix = ".exe" if (tools / "aapt.exe").exists() else ""
    badging = run(str(tools / ("aapt" + suffix)), "dump", "badging", str(apk))
    manifest = run(str(tools / ("aapt" + suffix)), "dump", "xmltree", str(apk), "AndroidManifest.xml")
    signer = tools / ("apksigner.bat" if suffix else "apksigner")
    cert = run(str(signer), "verify", "--verbose", "--print-certs", str(apk))
    return parse_facts(badging, manifest, cert, apk.read_bytes())


def parse_facts(badging, manifest, cert, data):
    package = re.search(r"package: name='([^']+)' versionCode='(\d+)' versionName='([^']+)'", badging)
    label = re.search(r'android:label\([^)]*\)="([^"]+)"', manifest)
    nsfw = re.search(r'"tachiyomi.extension.nsfw"[^\n]*\n\s*A: android:value\([^)]*\)=\(type 0x10\)0x([01])\b', manifest)
    fingerprints = re.findall(r"Signer #\d+ certificate SHA-256 digest: ([0-9a-fA-F]{64})", cert)
    if not package or not label or not nsfw or len(fingerprints) != 1:
        raise ValueError("Missing/ambiguous APK metadata or certificate")
    pkg, code, version = package.groups()
    # The official legacy client parses version.substringBeforeLast('.').toDouble().
    if pkg != PACKAGE or not re.fullmatch(r"1\.4\.\d+", version) or int(code) < 1:
        raise ValueError("Unexpected package, API version or versionCode")
    if '"tachiyomi.extension"' not in manifest or '".MangaManagerFactory"' not in manifest:
        raise ValueError("Not the expected extension manifest")
    debug = "application-debuggable" in badging or "android debug" in cert.lower()
    return dict(name=label[1], pkg=pkg, code=int(code), version=version,
                nsfw=int(nsfw[1]), certificate=fingerprints[0].lower(),
                sha256=sha256(data), debug=debug)


def release_record(facts):
    if facts["debug"]:
        raise ValueError("Debug APK/certificate is forbidden in the public repository")
    return {key: facts[key] for key in ("pkg", "code", "version", "certificate", "sha256")}


def validate_history(facts, records, read_apk, tools):
    """Reinspect every historic APK, not just a claimed index fingerprint/hash."""
    current = release_record(facts)
    seen = set()
    for record in records:
        if record["pkg"] != PACKAGE or record["code"] in seen:
            raise ValueError("Unexpected/duplicate history entry")
        seen.add(record["code"])
        old_apk = read_apk(record)
        actual = release_record(inspect(old_apk, tools))
        if actual != record:
            raise ValueError("Published artifact does not match immutable history")
        if actual["certificate"] != current["certificate"]:
            raise ValueError("Signing certificate changed; retain the original private key")
        if actual["code"] > current["code"]:
            raise ValueError("versionCode downgrade forbidden")
        if actual["code"] == current["code"] and actual != current:
            raise ValueError("versionCode already published with different bytes; bump version")
    return current


def apk_name(record):
    return f'{PACKAGE}-v{record["code"]}.apk'


def generate(output, apk, icon, facts, records):
    record = release_record(facts)
    output.mkdir(parents=True, exist_ok=True)
    for directory in ("apk", "icon"):
        (output / directory).mkdir(exist_ok=True)
    shutil.copyfile(apk, output / "apk" / apk_name(record))
    shutil.copyfile(icon, output / "icon" / f"{PACKAGE}.png")
    # sources is explicitly null: nullable without a default in official Kotlin serializer.
    entry = {key: facts[key] for key in ("name", "pkg", "code", "version", "nsfw")}
    entry.update(apk=apk_name(record), lang="all", sources=None)
    repo = {"index_v2": None, "meta": {"name": "Yomifolio / Manga Manager",
            "shortName": "Yomifolio", "website": WEBSITE,
            "signingKeyFingerprint": record["certificate"]}}
    history = sorted({r["code"]: r for r in records + [record]}.values(), key=lambda r: r["code"])
    for name, obj in (("index.json", [entry]), ("index.min.json", [entry]),
                      ("repo.json", repo), ("releases.json", history)):
        (output / name).write_text(json.dumps(obj, ensure_ascii=False,
            indent=None if name == "index.min.json" else 2) + "\n", encoding="utf-8")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--apk", type=Path, required=True)
    parser.add_argument("--tools", type=Path, required=True)
    parser.add_argument("--inspect-only", action="store_true")
    args = parser.parse_args()
    facts = inspect(args.apk.resolve(), args.tools.resolve())
    if not args.inspect_only:
        release_record(facts)
    print(json.dumps(facts, indent=2))


if __name__ == "__main__":
    main()
