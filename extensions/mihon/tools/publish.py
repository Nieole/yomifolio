"""Publish validated files via Git data API, preserving the branch tree (no force update)."""
import argparse
import base64
import json
import os
import tempfile
import urllib.error
import urllib.request
from pathlib import Path

from repository import PACKAGE, apk_name, generate, inspect, validate_history

REPO = "Nieole/yomifolio"
BRANCH = "mihon-repo"


class Github:
    def __init__(self):
        self.token = os.environ["GH_TOKEN"]

    def call(self, path, data=None, method=None, missing=False):
        request = urllib.request.Request(f"https://api.github.com/repos/{REPO}/{path}",
            data=None if data is None else json.dumps(data).encode(), method=method,
            headers={"Authorization": f"Bearer {self.token}",
                     "Accept": "application/vnd.github+json", "Content-Type": "application/json",
                     "X-GitHub-Api-Version": "2022-11-28"})
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            if error.code == 404 and missing:
                return None
            # Do not print request headers or payloads.
            raise RuntimeError(f"GitHub API failed: HTTP {error.code} ({path})") from None


def publish(apk, icon, tools):
    if os.environ.get("GITHUB_REPOSITORY") != REPO:
        raise ValueError("Publication is restricted to the upstream repository")
    api = Github()
    ref = api.call(f"git/ref/heads/{BRANCH}", missing=True)
    parent = ref["object"]["sha"] if ref else None
    tree_sha = None
    entries = {}
    if parent:
        commit = api.call(f"git/commits/{parent}")
        tree_sha = commit["tree"]["sha"]
        tree = api.call(f"git/trees/{tree_sha}?recursive=1")
        if tree.get("truncated"):
            raise ValueError("Cannot validate a truncated repository tree")
        entries = {item["path"]: item for item in tree["tree"]}

    def read(path):
        entry = entries[path]
        if entry["type"] != "blob" or entry["mode"] != "100644":
            raise ValueError("Expected a regular published file")
        blob = api.call(f'git/blobs/{entry["sha"]}')
        if blob["encoding"] != "base64":
            raise ValueError("Unexpected Git blob encoding")
        return base64.b64decode(blob["content"])

    facts = inspect(apk, tools)
    records = json.loads(read("releases.json")) if "releases.json" in entries else []
    if not records and any(p in entries for p in ("index.json", "index.min.json", "repo.json")):
        raise ValueError("Existing repository lacks verifiable signed history; manual review required")
    if records:
        meta = json.loads(read("repo.json"))["meta"]
        if meta["signingKeyFingerprint"] != facts["certificate"]:
            raise ValueError("Immutable repository certificate changed")
        latest = max(records, key=lambda r: r["code"])
        for path in ("index.json", "index.min.json"):
            index = json.loads(read(path))
            if len(index) != 1 or any(index[0][k] != latest[k] for k in ("pkg", "code", "version")):
                raise ValueError("Existing index/history mismatch")
            if index[0]["apk"] != apk_name(latest):
                raise ValueError("Existing index APK mismatch")
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)

        def read_apk(record):
            path = root / apk_name(record)
            path.write_bytes(read("apk/" + path.name))
            return path

        validate_history(facts, records, read_apk, tools)
        output = root / "output"
        generate(output, apk, icon, facts, records)
        files = {p.relative_to(output).as_posix(): p.read_bytes()
                 for p in output.rglob("*") if p.is_file()}
        # Never overwrite any existing APK, even if absent from history.
        for path, data in files.items():
            if path.startswith("apk/") and path in entries and read(path) != data:
                raise ValueError("APK filename collision; bump versionCode")
        # All build, certificate, hash and history validation is complete BEFORE any API writes.
        changes = []
        for path, data in files.items():
            if path in entries and read(path) == data:
                continue
            blob = api.call("git/blobs", {"content": base64.b64encode(data).decode(), "encoding": "base64"})
            changes.append({"path": path, "mode": "100644", "type": "blob", "sha": blob["sha"]})
        if not changes:
            print("Already published: identical signed artifact and metadata")
            return
        tree_data = {"tree": changes}
        if tree_sha:
            tree_data["base_tree"] = tree_sha
        tree = api.call("git/trees", tree_data)
        commit = api.call("git/commits", {"message": f'Mihon extension {facts["version"]} ({facts["code"]})',
                          "tree": tree["sha"], "parents": [parent] if parent else []})
        if parent:
            # Non-fast-forward races fail; never overwrite unrelated concurrent work.
            api.call(f"git/refs/heads/{BRANCH}", {"sha": commit["sha"], "force": False}, method="PATCH")
        else:
            api.call("git/refs", {"ref": f"refs/heads/{BRANCH}", "sha": commit["sha"]})
        print(f"Published signed APK and index to {BRANCH}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    for name in ("apk", "icon", "tools"):
        parser.add_argument("--" + name, type=Path, required=True)
    args = parser.parse_args()
    publish(args.apk.resolve(), args.icon.resolve(), args.tools.resolve())
