---
name: code-review
description: Review code changes for correctness, regressions, security, and missing tests.
---

# Code Review

Inspect the relevant diff and surrounding code before judging it. Prioritize concrete defects that can affect users, data, security, or maintainability. For each finding, identify the file and the smallest useful line range, explain the failure mode, and suggest a practical correction. Mention missing tests when they leave meaningful behavior unprotected. If no actionable defects are found, say so plainly and note any residual testing risk.
