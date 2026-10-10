import subprocess
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

from ci_changes import JOBS, changed_paths, select_jobs


class ChangeSelectionTests(unittest.TestCase):
    def enabled(self, event, paths):
        return {job for job, enabled in select_jobs(event, paths).items() if enabled}

    def test_documentation_only(self):
        self.assertEqual(self.enabled("push", ["README.md", "AGENTS.md", "docs/media/demo.mp4", "backend/ARCHITECTURE.md"]), set())

    def test_source_and_lockfile_scans(self):
        cases = [
            (["frontend/src/app/page.tsx"], {"web"}),
            (["frontend/pnpm-lock.yaml"], {"web", "security_web"}),
            (["frontend/pnpm-workspace.yaml"], {"web", "security_web"}),
            (["backend/internal/auth/token.go"], {"go", "security_go"}),
            (["backend/internal/skill/defaults/code-review/SKILL.md"], {"go", "security_go"}),
            (["backend/prompts/lester.md"], {"go", "security_go"}),
            (["deploy/gateway/nginx.conf"], {"gateway"}),
            (["deploy/docker-compose.debug.yaml"], {"gateway"}),
            (["deploy/.env.example"], {"gateway"}),
            (["deploy/helm/lester/templates/api.yaml"], {"helm"}),
        ]
        for paths, expected in cases:
            with self.subTest(paths=paths):
                self.assertEqual(self.enabled("pull_request", paths), expected)

    def test_ci_changes_unknown_paths_and_missing_comparison_run_everything(self):
        for paths in ([".github/scripts/ci_changes.py"], ["Makefile"], ["new-build-config"], None):
            self.assertEqual(self.enabled("push", paths), set(JOBS))

    def test_manual_full_run_and_weekly_security(self):
        self.assertEqual(self.enabled("workflow_dispatch", []), set(JOBS))
        self.assertEqual(self.enabled("schedule", None), {"security_go", "security_web"})

    def test_combined_changes(self):
        self.assertEqual(self.enabled("push", ["frontend/src/app/page.tsx", "deploy/helm/lester/values.yaml"]), {"web", "helm"})

    def test_new_branch_and_unavailable_commit(self):
        self.assertIsNone(changed_paths("push", {"before": "0" * 40, "after": "a" * 40}))
        self.assertIsNone(changed_paths("push", {"before": "missing", "after": "a" * 40}))

    def test_cli_writes_all_github_outputs_and_summary(self):
        script = Path(__file__).with_name("ci_changes.py").resolve()
        for event_name in ("schedule", "workflow_dispatch", "push"):
            with self.subTest(event=event_name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / "event.json").write_text(json.dumps({}))
                env = {**os.environ, "GITHUB_EVENT_NAME": event_name,
                       "GITHUB_EVENT_PATH": str(root / "event.json"),
                       "GITHUB_OUTPUT": str(root / "output"),
                       "GITHUB_STEP_SUMMARY": str(root / "summary")}
                subprocess.run([sys.executable, str(script)], cwd=root, env=env, check=True, capture_output=True)
                outputs = dict(line.split("=") for line in (root / "output").read_text().splitlines())
                expected = {name: str(enabled).lower() for name, enabled in select_jobs(event_name, None).items()}
                self.assertEqual(outputs, expected)
                self.assertIn("Selected checks", (root / "summary").read_text())

    def test_git_renames_deletions_and_pull_request_merge_base(self):
        with tempfile.TemporaryDirectory() as directory:
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=directory, text=True).strip()

            git("init", "-q")
            git("config", "user.name", "CI fixture")
            git("config", "user.email", "ci@example.test")
            root = Path(directory)
            (root / "backend").mkdir()
            (root / "frontend").mkdir()
            (root / "backend/a.go").write_text("package a\n")
            (root / "frontend/package.json").write_text("{}\n")
            git("add", ".")
            git("commit", "-qm", "base")
            base = git("rev-parse", "HEAD")
            git("checkout", "-qb", "feature")
            (root / "backend/a.go").rename(root / "a.md")
            (root / "frontend/package.json").unlink()
            git("add", "-A")
            git("commit", "-qm", "rename and delete")
            head = git("rev-parse", "HEAD")
            paths = changed_paths("push", {"before": base, "after": head}, directory)
            self.assertEqual(set(paths), {"backend/a.go", "a.md", "frontend/package.json"})
            self.assertEqual(self.enabled("push", paths), {"go", "web", "security_go", "security_web"})
            git("checkout", "-qb", "base-advanced", base)
            (root / "frontend/new.ts").write_text("export {};\n")
            git("add", ".")
            git("commit", "-qm", "advance base")
            advanced = git("rev-parse", "HEAD")
            event = {"pull_request": {"base": {"sha": advanced}, "head": {"sha": head}}}
            self.assertEqual(set(changed_paths("pull_request", event, directory)), set(paths))


if __name__ == "__main__":
    unittest.main()
