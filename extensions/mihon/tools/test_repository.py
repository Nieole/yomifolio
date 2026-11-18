import base64
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import prepare_signing
import publish
import repository as repo

CERT = "ab" * 32
BADGING = f"package: name='{repo.PACKAGE}' versionCode='1' versionName='1.4.1'"
MANIFEST = '''A: android:label(0x01010001)="Tachiyomi: Manga Manager"
A: android:name(1)="tachiyomi.extension"
A: android:value(1)=".MangaManagerFactory"
A: android:name(1)="tachiyomi.extension.nsfw"
A: android:value(1)=(type 0x10)0x0
'''
SIGNER = f"Signer #1 certificate SHA-256 digest: {CERT}\n"


def facts(**changes):
    result = repo.parse_facts(BADGING, MANIFEST, SIGNER, b"signed apk fixture")
    result.update(changes)
    return result


class FactsTests(unittest.TestCase):
    def test_actual_metadata_and_hash(self):
        value = facts()
        self.assertEqual(value["certificate"], CERT)
        self.assertEqual(value["sha256"], repo.sha256(b"signed apk fixture"))
        self.assertEqual((value["code"], value["version"], value["nsfw"]), (1, "1.4.1", 0))

    def test_debuggable_rejected(self):
        value = repo.parse_facts(BADGING + "\napplication-debuggable", MANIFEST, SIGNER, b"debug")
        with self.assertRaises(ValueError):
            repo.release_record(value)

    def test_debug_key_rejected_even_without_debuggable(self):
        value = repo.parse_facts(BADGING, MANIFEST, SIGNER + "Signer #1 certificate DN: CN=Android Debug", b"x")
        with self.assertRaises(ValueError):
            repo.release_record(value)

    def test_missing_or_multiple_signers_rejected(self):
        for cert in ("", SIGNER + SIGNER.replace("#1", "#2")):
            with self.subTest(cert=cert), self.assertRaises(ValueError):
                repo.parse_facts(BADGING, MANIFEST, cert, b"x")

    def test_missing_nsfw_rejected(self):
        with self.assertRaises(ValueError):
            repo.parse_facts(BADGING, "", SIGNER, b"x")

    def test_four_part_version_incompatible_with_official_legacy_parser(self):
        with self.assertRaises(ValueError):
            repo.parse_facts(BADGING.replace("1.4.1", "1.4.2.1"), MANIFEST, SIGNER, b"x")

    def test_wrong_package_rejected(self):
        with self.assertRaises(ValueError):
            repo.parse_facts(BADGING.replace(repo.PACKAGE, "other"), MANIFEST, SIGNER, b"x")

    def test_nsfw_read_from_manifest(self):
        self.assertEqual(repo.parse_facts(BADGING, MANIFEST.replace("0x0\n", "0x1\n"), SIGNER, b"x")["nsfw"], 1)


class HistoryTests(unittest.TestCase):
    def validate(self, new, old=None, record=None):
        old = old or facts()
        record = record or repo.release_record(old)
        with patch.object(repo, "inspect", return_value=old):
            return repo.validate_history(new, [record], lambda r: Path("old.apk"), Path("tools"))

    def test_first_release(self):
        self.assertEqual(repo.validate_history(facts(), [], None, None), repo.release_record(facts()))

    def test_identical_retry(self):
        self.validate(facts())

    def test_same_code_changed_hash(self):
        with self.assertRaises(ValueError):
            self.validate(facts(sha256="cd" * 32))

    def test_certificate_change(self):
        with self.assertRaises(ValueError):
            self.validate(facts(code=2, certificate="cd" * 32))

    def test_downgrade(self):
        with self.assertRaises(ValueError):
            self.validate(facts(), old=facts(code=2))

    def test_bump_same_certificate(self):
        self.validate(facts(code=2, version="1.4.2", sha256="cd" * 32))

    def test_tampered_published_artifact(self):
        record = repo.release_record(facts())
        record["sha256"] = "cd" * 32
        with self.assertRaises(ValueError):
            self.validate(facts(code=2), record=record)

    def test_historic_debug_forbidden(self):
        with self.assertRaises(ValueError):
            self.validate(facts(code=2), old=facts(debug=True))

    def test_duplicate_history_rejected(self):
        record = repo.release_record(facts())
        with patch.object(repo, "inspect", return_value=facts()), self.assertRaises(ValueError):
            repo.validate_history(facts(code=2), [record, record], lambda r: Path("old"), Path("tools"))


class GenerationTests(unittest.TestCase):
    def test_official_legacy_schema_and_icon_copy(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            apk, icon, output = root / "release.apk", root / "icon.png", root / "out"
            apk.write_bytes(b"signed apk fixture")
            icon.write_bytes(b"unchanged branding")
            repo.generate(output, apk, icon, facts(), [])
            index = json.loads((output / "index.min.json").read_text())
            self.assertEqual(index, json.loads((output / "index.json").read_text()))
            self.assertEqual(set(index[0]), {"name", "pkg", "apk", "lang", "code", "version", "nsfw", "sources"})
            self.assertIsNone(index[0]["sources"])
            self.assertEqual((output / "icon" / f"{repo.PACKAGE}.png").read_bytes(), icon.read_bytes())
            self.assertEqual((output / "apk" / index[0]["apk"]).read_bytes(), apk.read_bytes())
            metadata = json.loads((output / "repo.json").read_text())
            self.assertIsNone(metadata["index_v2"])
            self.assertEqual(metadata["meta"]["signingKeyFingerprint"], CERT)

    def test_debug_generation_fails_before_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "out"
            with self.assertRaises(ValueError):
                repo.generate(output, Path("debug.apk"), Path("icon.png"), facts(debug=True), [])
            self.assertFalse(output.exists())

    def test_invalid_signature_subprocess_failure_propagates(self):
        with patch.object(repo.subprocess, "run", side_effect=RuntimeError("invalid signature")):
            with self.assertRaises(RuntimeError):
                repo.inspect(Path("release.apk"), Path("tools"))


class SigningTests(unittest.TestCase):
    @patch.dict(os.environ, {}, clear=True)
    def test_missing_secrets_fail_without_creating_keystore(self):
        with self.assertRaises(ValueError):
            prepare_signing.main()

    def test_decode_uses_temporary_path_and_does_not_export_secret_values(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            env = {"RUNNER_TEMP": str(root), "GITHUB_ENV": str(root / "env"),
                   "MIHON_KEYSTORE_BASE64": base64.b64encode(b"test fixture, not a real key").decode(),
                   "MIHON_KEYSTORE_PASSWORD": "fixture-password", "MIHON_KEY_ALIAS": "fixture-alias",
                   "MIHON_KEY_PASSWORD": "fixture-key-password"}
            with patch.dict(os.environ, env, clear=True):
                prepare_signing.main()
            self.assertEqual((root / "mihon-release.keystore").read_bytes(), b"test fixture, not a real key")
            exported = (root / "env").read_text()
            self.assertTrue(exported.startswith("MIHON_KEYSTORE_PATH="))
            self.assertNotIn("fixture-password", exported)
            self.assertNotIn(env["MIHON_KEYSTORE_BASE64"], exported)


class PublishTests(unittest.TestCase):
    @patch.dict(os.environ, {"GITHUB_REPOSITORY": publish.REPO})
    def test_no_api_writes_before_failed_validation(self):
        calls = []

        class FakeGithub:
            def call(self, path, data=None, **kwargs):
                calls.append((path, data))
                return None

        with patch.object(publish, "Github", FakeGithub), patch.object(publish, "inspect", return_value=facts(debug=True)):
            with self.assertRaises(ValueError):
                publish.publish(Path("release.apk"), Path("icon.png"), Path("tools"))
        self.assertTrue(all(data is None for _, data in calls))

    @patch.dict(os.environ, {"GITHUB_REPOSITORY": publish.REPO})
    def test_preserves_base_tree_and_never_forces_ref(self):
        calls = []
        record = repo.release_record(facts())
        contents = {
            "releases.json": json.dumps([record]).encode(),
            "repo.json": json.dumps({"meta": {"signingKeyFingerprint": CERT}}).encode(),
            "index.json": json.dumps([dict(record, apk=repo.apk_name(record))]).encode(),
            "index.min.json": json.dumps([dict(record, apk=repo.apk_name(record))]).encode(),
            "apk/" + repo.apk_name(record): b"signed apk fixture",
            "unrelated.txt": b"must survive",
        }
        blobs = {str(i): value for i, value in enumerate(contents.values())}
        entries = [{"path": name, "type": "blob", "mode": "100644", "sha": str(i)}
                   for i, name in enumerate(contents)]

        class FakeGithub:
            def call(self, path, data=None, **kwargs):
                calls.append((path, data, kwargs))
                if path == "git/ref/heads/mihon-repo":
                    return {"object": {"sha": "old-commit"}}
                if path == "git/commits/old-commit":
                    return {"tree": {"sha": "old-tree"}}
                if path == "git/trees/old-tree?recursive=1":
                    return {"tree": entries, "truncated": False}
                if path.startswith("git/blobs/"):
                    return {"encoding": "base64", "content": base64.b64encode(blobs[path.rsplit('/', 1)[1]]).decode()}
                if path == "git/blobs":
                    return {"sha": "new-blob"}
                if path == "git/trees":
                    return {"sha": "new-tree"}
                if path == "git/commits":
                    return {"sha": "new-commit"}
                if path == "git/refs/heads/mihon-repo":
                    return {}
                raise AssertionError(path)

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            apk, icon = root / "release.apk", root / "icon.png"
            apk.write_bytes(b"new signed release")
            icon.write_bytes(b"icon")
            new = facts(code=2, version="1.4.2", sha256=repo.sha256(apk.read_bytes()))
            with patch.object(publish, "Github", FakeGithub), patch.object(publish, "inspect", return_value=new), patch.object(repo, "inspect", return_value=facts()), patch("builtins.print"):
                publish.publish(apk, icon, root)
        tree = next(data for path, data, _ in calls if path == "git/trees")
        self.assertEqual(tree["base_tree"], "old-tree")
        self.assertNotIn("unrelated.txt", [entry["path"] for entry in tree["tree"]])
        commit = next(data for path, data, _ in calls if path == "git/commits")
        self.assertEqual(commit["parents"], ["old-commit"])
        path, data, kwargs = calls[-1]
        self.assertEqual(path, "git/refs/heads/mihon-repo")
        self.assertEqual(data, {"sha": "new-commit", "force": False})
        self.assertEqual(kwargs["method"], "PATCH")

    @patch.dict(os.environ, {"GITHUB_REPOSITORY": "fork/repo"})
    def test_fork_forbidden_before_token_access(self):
        with self.assertRaises(ValueError):
            publish.publish(Path("release.apk"), Path("icon.png"), Path("tools"))


if __name__ == "__main__":
    unittest.main()
