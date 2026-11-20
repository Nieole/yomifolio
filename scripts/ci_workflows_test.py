"""Exercise the workflow's actual gates and tag resolver, not copied decision logic."""

import copy
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import textwrap
import unittest


ROOT = Path(__file__).resolve().parents[1]
WORKFLOWS = ROOT / ".github" / "workflows"


def workflow(name):
    return (WORKFLOWS / name).read_text(encoding="utf-8")


def job(source, name):
    jobs = source.split("\njobs:\n", 1)[1]
    match = re.search(rf"(?m)^  {re.escape(name)}:\n(.*?)(?=^  [\w-]+:\n|\Z)", jobs, re.S)
    if match is None:
        raise AssertionError(f"Job not found: {name}")
    return match.group(1)


def script(source, step):
    block = source.split(f"      - name: {step}\n", 1)[1]
    block = re.split(r"(?m)^      - ", block, maxsplit=1)[0]
    match = re.search(r"(?m)^        run: \|\n((?:          [^\n]*\n|\n)+)", block)
    if match is None:
        raise AssertionError(f"Multiline script not found: {step}")
    return textwrap.dedent(match.group(1)).strip()


def gate_code(filename, job_name, step):
    body = script(job(workflow(filename), job_name), step)
    match = re.search(r"python3 - <<'(\w+)'\n(.*?)\n\1", body, re.S)
    if match is None:
        raise AssertionError("Python gate not found")
    return match.group(2)


class DocumentationTests(unittest.TestCase):
    def test_ci_guide_does_not_shadow_project_readme(self):
        self.assertTrue((ROOT / "README.md").is_file())
        shadowing = [path.name for path in (ROOT / ".github").iterdir()
                     if path.is_file() and path.stem.casefold() == "readme"]
        self.assertEqual(shadowing, [], "GitHub prefers .github README over the project overview")
        self.assertTrue((ROOT / ".github" / "CI.md").is_file())


class GateTests(unittest.TestCase):
    def run_gate(self, code, variable, needs):
        result = subprocess.run([sys.executable, "-c", code],
                                env={**os.environ, variable: json.dumps(needs)},
                                capture_output=True, text=True, check=False)
        return result.returncode

    def assert_gate_contract(self, code, variable, categories):
        flags = dict.fromkeys(set(categories.values()), "true")
        needs = {"changes": {"result": "success", "outputs": flags}}
        needs.update({name: {"result": "success"} for name in categories})
        self.assertEqual(self.run_gate(code, variable, needs), 0)
        for name in needs:
            for failure in ("failure", "skipped", "cancelled"):
                with self.subTest(job=name, result=failure):
                    broken = copy.deepcopy(needs)
                    broken[name]["result"] = failure
                    self.assertNotEqual(self.run_gate(code, variable, broken), 0)
        for invalid in ({}, dict.fromkeys(flags, ""), dict.fromkeys(flags, "maybe")):
            broken = copy.deepcopy(needs)
            broken["changes"]["outputs"] = invalid
            self.assertNotEqual(self.run_gate(code, variable, broken), 0)
        skipped = copy.deepcopy(needs)
        skipped["changes"]["outputs"] = dict.fromkeys(flags, "false")
        for name in categories:
            skipped[name]["result"] = "skipped"
        self.assertEqual(self.run_gate(code, variable, skipped), 0)
        # An incorrectly executed job must not be confused with a legitimate skip.
        for name in categories:
            broken = copy.deepcopy(skipped)
            broken[name]["result"] = "success"
            self.assertNotEqual(self.run_gate(code, variable, broken), 0)

    def test_server_gate(self):
        categories = {"frontend": "frontend", "go-static": "backend", "go-native": "backend",
                      "race": "backend", "security": "security", "generated": "generated",
                      "docs": "docs", "workflows": "workflows"}
        code = gate_code("ci.yml", "checks", "Require every selected check to succeed")
        self.assert_gate_contract(code, "RESULTS", categories)
        block = job(workflow("ci.yml"), "checks")
        for name in ("changes", *categories):
            self.assertIn(name, block.split("needs:", 1)[1].splitlines()[0])
        self.assertIn("if: always()", block)

    def test_mihon_gate(self):
        code = gate_code("mihon-ci.yml", "required", "Verify selected checks succeeded")
        self.assert_gate_contract(code, "NEEDS", {"tools": "mihon", "android": "mihon"})
        block = job(workflow("mihon-ci.yml"), "required")
        self.assertIn("needs: [changes, tools, android]", block)
        self.assertIn("if: always()", block)


class WorkflowBoundaryTests(unittest.TestCase):
    def test_daily_entries_always_report_and_full_entries_force_validation(self):
        for filename in ("ci.yml", "mihon-ci.yml"):
            with self.subTest(workflow=filename):
                source = workflow(filename)
                triggers = source.split("\njobs:\n", 1)[0]
                self.assertNotRegex(triggers, r"(?m)^\s+paths(?:-ignore)?:")
                self.assertIn("  workflow_dispatch:", triggers)
                self.assertIn("  workflow_call:", triggers)
                self.assertIn("default: true", triggers)
                changes = job(source, "changes")
                self.assertIn("github.event_name == 'workflow_dispatch' || inputs.full == true", changes)
                self.assertIn("--full", changes)
                self.assertIn("fetch-depth: 0", changes)

    def test_only_final_publishers_can_write(self):
        for path in WORKFLOWS.glob("*.yml"):
            source = path.read_text(encoding="utf-8")
            self.assertIn("permissions:\n  contents: read", source)
            for name in re.findall(r"(?m)^  ([\w-]+):$", source.split("\njobs:\n", 1)[1]):
                if "contents: write" in job(source, name):
                    self.assertIn(path.name, ("release.yml", "mihon-publish.yml"))
                    self.assertEqual(name, "publish")

    def test_same_run_frontend_is_reused_without_rebuilding(self):
        packaging = workflow("package.yml")
        self.assertNotIn("npm ", packaging)
        self.assertIn("name: ${{ inputs.frontend-artifact }}", packaging)
        self.assertNotIn("run-id:", packaging)
        self.assertIn('CGO_ENABLED: "1"', packaging)
        for platform in ("ubuntu-latest", "windows-latest", "macos-15"):
            self.assertIn(platform, packaging)
            self.assertIn(platform, job(workflow("ci.yml"), "go-native"))
        for filename in ("build.yml", "release.yml"):
            source = workflow(filename)
            self.assertIn("full: true", job(source, "ci"))
            self.assertIn("needs.ci.outputs.frontend-artifact", job(source, "package"))
        self.assertNotIn("publish:", workflow("build.yml"))

    def test_mihon_signing_follows_same_commit_secrets_free_checks(self):
        source = workflow("mihon-publish.yml")
        ci = job(source, "ci")
        build = job(source, "build")
        self.assertIn("full: true", ci)
        self.assertIn("ref: ${{ github.sha }}", ci)
        self.assertIn("needs: ci", build)
        self.assertIn("ref: ${{ github.sha }}", build)
        for block in (ci, build):
            self.assertIn("github.repository == 'Nieole/yomifolio'", block)
            self.assertIn("github.event.repository.default_branch", block)
        self.assertNotIn("secrets.", workflow("mihon-ci.yml"))
        self.assertIn("if: always()", build)
        self.assertIn('rm -f "$RUNNER_TEMP/mihon-release.keystore"', build)
        self.assertNotIn("secrets.", job(source, "publish"))
        self.assertIn("cancel-in-progress: false", source)


class TagResolverTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.cwd = self.temp.name
        self.git("init", "-q")
        self.git("config", "user.email", "ci-test@example.invalid")
        self.git("config", "user.name", "CI Test")
        self.git("-c", "commit.gpgsign=false", "commit", "-qm", "fixture", "--allow-empty")
        self.sha = self.git("rev-parse", "HEAD").strip()
        self.git("tag", "v1.2.3")
        self.git("-c", "tag.gpgsign=false", "tag", "-am", "annotated", "v1.2.4")
        self.git("branch", "v-branch-only")
        self.code = script(job(workflow("release.yml"), "resolve"), "Resolve existing version tag")

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.cwd, text=True, stderr=subprocess.PIPE)

    def resolve(self, requested, event="workflow_dispatch", ref_type="branch", ref_name="main"):
        output = Path(self.cwd) / "output"
        output.unlink(missing_ok=True)
        env = {**os.environ, "REQUESTED_TAG": requested, "GITHUB_EVENT_NAME": event,
               "GITHUB_REF_TYPE": ref_type, "GITHUB_REF_NAME": ref_name, "GITHUB_OUTPUT": "output"}
        result = subprocess.run(["bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", self.code],
                                cwd=self.cwd, env=env, capture_output=True, text=True, check=False)
        return result.returncode, output.read_text(encoding="utf-8") if output.exists() else ""

    def test_existing_lightweight_and_annotated_tags_pin_commit(self):
        for tag in ("v1.2.3", "v1.2.4"):
            with self.subTest(tag=tag):
                status, output = self.resolve(tag)
                self.assertEqual(status, 0)
                self.assertEqual(output, f"tag={tag}\nsha={self.sha}\n")

    def test_push_uses_event_tag_not_manual_input(self):
        status, output = self.resolve("v-nonexistent", "push", "tag", "v1.2.3")
        self.assertEqual(status, 0)
        self.assertIn("tag=v1.2.3\n", output)
        self.assertNotEqual(self.resolve("", "push", "branch", "v1.2.3")[0], 0)

    def test_non_tag_missing_or_unsafe_inputs_cannot_publish(self):
        for tag in ("", "main", "v-branch-only", "v-missing", "v1/other", "v1\nsha=bad",
                    "v$(touch injected)", "--help", "v1' -X malicious"):
            with self.subTest(tag=tag):
                status, output = self.resolve(tag)
                self.assertNotEqual(status, 0)
                self.assertEqual(output, "")
        self.assertFalse((Path(self.cwd) / "injected").exists())


if __name__ == "__main__":
    unittest.main()
