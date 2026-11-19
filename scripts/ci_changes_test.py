"""Regression checks for path selection and conservative comparison handling."""

from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from ci_changes import CATEGORIES, classify, select


class SelectionTests(unittest.TestCase):
    def enabled(self, *paths):
        return {key for key, value in classify(paths).items() if value}

    def test_product_boundaries(self):
        self.assertEqual(self.enabled("extensions/mihon/src/Foo.kt"), {"mihon"})
        self.assertEqual(self.enabled("extensions/mihon/tools/test_repository.py"), {"mihon"})
        for path in ("README.md", "web/README.md", "extensions/mihon/README.md", "extensions/mihon/SPEC.md"):
            with self.subTest(path=path):
                self.assertEqual(self.enabled(path), {"docs"})
        self.assertEqual(self.enabled("web/src/App.tsx"), {"frontend", "backend", "docs"})
        self.assertNotIn("mihon", self.enabled("internal/reading/state.go"))

    def test_shared_inputs_and_unknown_paths(self):
        self.assertIn("mihon", self.enabled("web/public/icons/icon-192.png"))
        self.assertIn("mihon", self.enabled("internal/api/mihon.go"))
        for path in (".github/workflows/ci.yml", "scripts/ci_changes.py", "new-build-input.cfg"):
            with self.subTest(path=path):
                self.assertEqual(self.enabled(path), set(CATEGORIES))

    def test_server_dependencies_select_all_server_checks(self):
        expected = set(CATEGORIES) - {"workflows", "mihon"}
        for path in ("go.mod", "go.sum", "sql/query.sql", "internal/database/schema.sql",
                     "cmd/tsgen/main.go", "web/web.go", ".golangci.yml", "sqlc.yaml"):
            with self.subTest(path=path):
                self.assertEqual(self.enabled(path), expected)

    def test_hand_edited_generated_contract_is_checked(self):
        self.assertIn("generated", self.enabled("web/src/api/generated.ts"))

    def test_paths_are_combined_including_deleted_or_renamed_inputs(self):
        self.assertEqual(self.enabled(), set())
        self.assertEqual(self.enabled("README.md", "extensions/mihon/build.gradle.kts"), {"docs", "mihon"})
        self.assertIn("backend", self.enabled("web/old.ts", "docs/new.md"))

    def test_manual_full_cannot_be_path_filtered(self):
        self.assertTrue(all(select({}, "workflow_dispatch", full=True).values()))
        self.assertTrue(all(select({}, "workflow_dispatch").values()))

    @patch("ci_changes.git")
    def test_pr_uses_merge_base_and_nul_paths(self, git):
        base, head, ancestor = "a" * 40, "b" * 40, "c" * 40
        git.side_effect = [ancestor.encode(), b"web/src/name with spaces.ts\0README.md\0"]
        selected = select({"pull_request": {"base": {"sha": base}, "head": {"sha": head}}}, "pull_request")
        self.assertTrue(selected["frontend"])
        self.assertFalse(selected["mihon"])
        self.assertEqual(git.call_args_list[0].args, ("merge-base", base, head))
        self.assertEqual(git.call_args_list[1].args,
                         ("diff", "--name-only", "--no-renames", "-z", ancestor, head, "--"))

    @patch("ci_changes.git")
    def test_push_compares_before_and_after(self, git):
        before, after = "a" * 40, "b" * 40
        git.return_value = b"extensions/mihon/build.gradle.kts\0"
        selected = select({"before": before, "after": after}, "push")
        self.assertEqual({key for key, enabled in selected.items() if enabled}, {"mihon"})
        self.assertEqual(git.call_args.args, ("diff", "--name-only", "--no-renames", "-z", before, after, "--"))

    @patch("ci_changes.git")
    def test_invalid_pr_revisions_never_reach_git(self, git):
        event = {"pull_request": {"base": {"sha": "--bad"}, "head": {"sha": "b" * 40}}}
        self.assertTrue(all(select(event, "pull_request").values()))
        git.assert_not_called()

    @patch("ci_changes.git", side_effect=subprocess.CalledProcessError(128, "git"))
    def test_missing_history_runs_everything(self, _git):
        self.assertTrue(all(select({"before": "a" * 40, "after": "b" * 40}, "push").values()))

    def test_new_branch_or_malformed_event_runs_everything(self):
        for event in ({}, {"before": "0" * 40, "after": "b" * 40},
                      {"before": "--bad-flag", "after": "b" * 40}):
            with self.subTest(event=event):
                self.assertTrue(all(select(event, "push").values()))


class GitComparisonTests(unittest.TestCase):
    def test_divergent_pr_excludes_unrelated_base_changes_and_tracks_renames(self):
        with tempfile.TemporaryDirectory() as directory:
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=directory, stderr=subprocess.PIPE)

            def commit(path, content):
                target = Path(directory) / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(content, encoding="utf-8")
                git("add", "--all")
                git("-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
                return git("rev-parse", "HEAD").decode().strip()

            git("init", "-q")
            git("config", "user.name", "CI Test")
            git("config", "user.email", "ci-test@example.invalid")
            root = commit("README.md", "root")
            git("branch", "feature")
            base = commit("internal/reading/state.go", "base-only")
            git("checkout", "-q", "feature")
            head = commit("extensions/mihon/src/Source.kt", "feature-only")
            event = {"pull_request": {"base": {"sha": base}, "head": {"sha": head}}}
            with patch("ci_changes.git", side_effect=git):
                selected = select(event, "pull_request")
                self.assertEqual({key for key, enabled in selected.items() if enabled}, {"mihon"})
                self.assertEqual(select({"before": root, "after": head}, "push"), selected)
            before = commit("web/old name.ts", "renamed")
            git("mv", "web/old name.ts", "moved.md")
            git("-c", "commit.gpgsign=false", "commit", "-qm", "rename")
            after = git("rev-parse", "HEAD").decode().strip()
            with patch("ci_changes.git", side_effect=git):
                selected = select({"before": before, "after": after}, "push")
                self.assertTrue(selected["frontend"])
                self.assertTrue(selected["backend"])


if __name__ == "__main__":
    unittest.main()
