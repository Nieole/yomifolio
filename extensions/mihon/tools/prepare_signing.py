"""Decode private signing material only into RUNNER_TEMP; never print its contents."""
import base64
import os
from pathlib import Path


def main():
    names = ("MIHON_KEYSTORE_BASE64", "MIHON_KEYSTORE_PASSWORD", "MIHON_KEY_ALIAS", "MIHON_KEY_PASSWORD")
    if not all(os.environ.get(name) for name in names):
        raise ValueError("All four MIHON signing secrets are required")
    path = Path(os.environ["RUNNER_TEMP"]) / "mihon-release.keystore"
    data = base64.b64decode(os.environ["MIHON_KEYSTORE_BASE64"], validate=True)
    if not data:
        raise ValueError("Empty keystore")
    with path.open("xb") as stream:
        os.chmod(path, 0o600)
        stream.write(data)
    with open(os.environ["GITHUB_ENV"], "a", encoding="utf-8") as stream:
        stream.write(f"MIHON_KEYSTORE_PATH={path}\n")


if __name__ == "__main__":
    main()
