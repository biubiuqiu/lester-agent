import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

import build_research_site
import check_research_pages


class ResearchPagesTests(unittest.TestCase):
    def test_staging_only_publishes_reading_files(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "site"
            build_research_site.stage(output)
            files = {p.relative_to(output).as_posix() for p in output.rglob("*") if p.is_file()}
            self.assertEqual(files, {"index.html", "agent-context-management/index.html", ".nojekyll"})
            self.assertIn("Lester Research", (output / "index.html").read_text())
            self.assertIn("上下文管理：从抽象到实现", (output / "agent-context-management/index.html").read_text())
            with self.assertRaises(ValueError):
                build_research_site.stage(output)

    def preflight(self, settings=None, page="", returncode=0, error=None):
        completed = subprocess.CompletedProcess([], returncode, json.dumps(settings or {}), "")
        with patch.dict("os.environ", {"GITHUB_REPOSITORY": "biubiuqiu/lester-agent"}), \
                patch("check_research_pages.subprocess.run", return_value=completed), \
                patch("check_research_pages.read_page", return_value=page, side_effect=error) as read:
            check_research_pages.preflight()
            return read

    def test_missing_pages_is_actionable_and_stops_deployment(self):
        with self.assertRaisesRegex(RuntimeError, "settings/pages"):
            self.preflight(returncode=1)

    def test_other_publishing_source_is_not_silently_changed(self):
        with self.assertRaisesRegex(RuntimeError, "different publishing source"):
            self.preflight({"build_type": "legacy", "html_url": "https://example.test/"})

    def test_unrelated_homepage_is_not_replaced(self):
        with self.assertRaisesRegex(RuntimeError, "unrelated"):
            self.preflight({"build_type": "workflow", "html_url": "https://example.test/"}, "An existing product website")

    def test_first_deployment_accepts_a_configured_empty_site(self):
        self.preflight({"build_type": "workflow", "html_url": "https://example.test/"},
                       error=HTTPError("https://example.test/", 404, "Not Found", {}, None))

    def test_existing_research_can_be_updated(self):
        self.preflight({"build_type": "workflow", "html_url": "https://example.test/"}, "Lester / Research")

    def test_verification_keeps_the_repository_base_path(self):
        with patch("check_research_pages.read_page", side_effect=["Lester Research", "上下文管理：从抽象到实现"]) as read:
            check_research_pages.verify("https://biubiuqiu.github.io/lester-agent/")
        self.assertEqual([call.args[0] for call in read.call_args_list], [
            "https://biubiuqiu.github.io/lester-agent/",
            "https://biubiuqiu.github.io/lester-agent/agent-context-management/",
        ])


if __name__ == "__main__":
    unittest.main()
