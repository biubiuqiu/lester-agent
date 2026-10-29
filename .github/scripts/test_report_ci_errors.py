import json
import unittest

from report_ci_errors import annotation, messages


class ReportErrorsTest(unittest.TestCase):
    def test_swift_and_kotlin_compiler_errors_retain_actionable_context(self):
        for mode, line in (
            ("ios", "/runner/Lester/Web.swift:12:3: error: incorrect argument label"),
            ("android", "e: file:///runner/Web.kt:14:9 Unresolved reference 'frame'"),
        ):
            with self.subTest(mode=mode):
                result = messages(mode, "building\n" + line + "\nsource context\nmore context")
                self.assertTrue(any(line in item and "source context" in item for item in result))

    def test_gradle_dependency_failures_are_not_replaced_with_only_exit_code(self):
        result = messages("android", "FAILURE: Build failed\n* What went wrong:\n> Could not resolve pinned artifact\nverification failed")
        self.assertTrue(any("Could not resolve pinned artifact" in item for item in result))

    def test_go_reports_id_and_fixed_version_instead_of_only_call_trace(self):
        result = messages("security-go", "Vulnerability #1: GO-2026-6809\n  Certificate validation\n  More info: example.test\n  Module: standard library\n    Found in: go1.26.9\n    Fixed in: go1.26.10\n\n  Trace:\n    SMTPMailer.Send calls Verify")
        self.assertEqual(len(result), 1)
        self.assertIn("GO-2026-6809", result[0])
        self.assertIn("Fixed in: go1.26.10", result[0])

    def test_pnpm_advisories_keep_affected_range_and_patch_without_low_severity(self):
        report = {"advisories": {"1": {"module_name": "next", "title": "RCE", "severity": "critical", "vulnerable_versions": "<=16.4.0", "patched_versions": ">=16.4.1", "url": "https://example.test/advisory"}, "2": {"severity": "moderate", "title": "not a blocking advisory"}}}
        result = messages("security-web", json.dumps(report))
        self.assertEqual(len(result), 1)
        self.assertIn("<=16.4.0", result[0])
        self.assertIn(">=16.4.1", result[0])
        self.assertIn("https://example.test/advisory", result[0])

    def test_missing_or_invalid_audit_data_does_not_claim_success(self):
        self.assertIn("failed", messages("security-web", "{}")[0])
        self.assertIn("did not return valid JSON", messages("security-web", "registry unavailable")[0])
        self.assertIn("did not return an advisory object", messages("security-web", "[]")[0])

    def test_annotation_escapes_commands_and_bounds_output(self):
        value = annotation("iOS build failure", "error: 100%\r\n::notice::not another command" + "x" * 5000)
        self.assertNotIn("\n", value)
        self.assertIn("100%25%0D%0A::notice", value)
        self.assertLess(len(value), 3600)


if __name__ == "__main__":
    unittest.main()
