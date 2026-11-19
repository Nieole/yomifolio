"""Select daily CI work; uncertain history or unknown paths require full validation."""

import argparse
import json
from pathlib import Path
import subprocess


CATEGORIES = ("frontend", "backend", "generated", "security", "docs", "workflows", "mihon")


def classify(paths):
    selected = set()
    for path in paths:
        if path.startswith(".github/workflows/") or path.startswith("scripts/ci_"):
            selected.update(CATEGORIES)
        elif path.endswith(".md") or path.startswith(("docs/", "images/", ".github/ISSUE_TEMPLATE/")):
            selected.add("docs")
        elif path.startswith("extensions/mihon/"):
            selected.add("mihon")
        elif path == "web/public/icons/icon-192.png":
            selected.update(("frontend", "backend", "mihon"))
        elif path == "web/src/api/generated.ts":
            selected.update(("frontend", "backend", "generated", "docs"))
        elif path.startswith("web/") and path.endswith(".go"):
            selected.update(("frontend", "backend", "generated", "security", "docs"))
        elif path.startswith("web/") or path == ".node-version":
            selected.update(("frontend", "backend", "docs"))
        elif (path.startswith(("internal/", "cmd/", "sql/"))
              or path in ("go.mod", "go.sum", "sqlc.yaml", ".golangci.yml")):
            selected.update(("frontend", "backend", "generated", "security", "docs"))
            if path.startswith("internal/api/"):
                selected.add("mihon")
        elif path == "scripts/check-doc-style.sh":
            selected.add("docs")
        else:
            selected.update(CATEGORIES)
    return {name: name in selected for name in CATEGORIES}


def git(*args):
    return subprocess.check_output(["git", *args], stderr=subprocess.PIPE)


def changed_paths(event, event_name):
    if event_name == "pull_request":
        base = event["pull_request"]["base"]["sha"]
        head = event["pull_request"]["head"]["sha"]
    elif event_name == "push":
        base, head = event["before"], event["after"]
    else:
        raise ValueError("event requires full validation")
    for revision in (base, head):
        if not revision or set(revision) == {"0"}:
            raise ValueError("no usable comparison revision")
        # Event revisions must be object IDs, never git flags or revision expressions.
        if len(revision) not in (40, 64) or any(c not in "0123456789abcdef" for c in revision):
            raise ValueError("invalid comparison revision")
    if event_name == "pull_request":
        base = git("merge-base", base, head).decode().strip()
    output = git("diff", "--name-only", "--no-renames", "-z", base, head, "--")
    return output.decode("utf-8").rstrip("\0").split("\0") if output else []


def select(event, event_name, full=False):
    if full:
        return dict.fromkeys(CATEGORIES, True)
    try:
        return classify(changed_paths(event, event_name))
    except (KeyError, ValueError, UnicodeError, subprocess.CalledProcessError, OSError) as error:
        print(f"Conservative full validation: {type(error).__name__}")
        return dict.fromkeys(CATEGORIES, True)


def main():
    import os

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--event", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--full", action="store_true")
    args = parser.parse_args()
    event = json.loads(Path(args.event).read_text(encoding="utf-8"))
    selection = select(event, os.environ.get("GITHUB_EVENT_NAME", ""), args.full)
    lines = "".join(f"{key}={str(value).lower()}\n" for key, value in selection.items())
    with Path(args.output).open("a", encoding="utf-8") as output:
        output.write(lines)
    print(lines, end="")


if __name__ == "__main__":
    main()
