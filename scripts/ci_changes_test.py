"""Regression checks for path selection and conservative comparison handling."""

import subprocess
import unittest
from unittest.mock import patch

from ci_changes import CATEGORIES, classify, select


class SelectionTests(unittest.TestCase):
    def enabled(self, *paths):
        return {key for key, value in classify(paths).items() if value}

    def test_product_boundaries(self):
        self.assertEqual(self.enabled("extensions/mihon/src/Foo.kt"), {"mihon"})
        self.assertEqual(self.enabled("extensions/mihon/tools/test_repository.py"), {"mihon"})
        self.assertEqual(self.enabled("README.md"), {"docs"})
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
        git.return_value = b"extensions/mihon/README.md\0"
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


if __name__ == "__main__":
    unittest.main()
