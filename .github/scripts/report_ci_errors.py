"""Expose bounded, actionable build/security errors in public job annotations.

The original command and pipefail retain ownership of the job's exit status.
This reporter never turns a failed check into a successful one.
"""

import argparse
import json
import re
from pathlib import Path


TITLES = {
    "ios": "iOS build failure",
    "android": "Android build failure",
    "security-go": "Go dependency vulnerability",
    "security-web": "Frontend dependency vulnerability",
}
ANSI = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
ERROR = re.compile(r"(?:\berror:|^e:|^FAILURE:|^\* What went wrong:|^> |AssertionError|^Traceback)")


def messages(mode, raw):
    if mode == "security-web":
        try:
            report = json.loads(raw)
        except (ValueError, TypeError):
            return ["Dependency audit did not return valid JSON. Inspect the retained audit log."]
        if not isinstance(report, dict) or not isinstance(report.get("advisories", {}), dict):
            return ["Dependency audit did not return an advisory object. Inspect the retained audit log."]
        result = []
        for advisory in report.get("advisories", {}).values():
            if not isinstance(advisory, dict):
                continue
            if advisory.get("severity") in ("high", "critical"):
                result.append(
                    f"{advisory.get('module_name')}: {advisory.get('title')} "
                    f"[{advisory.get('severity')}]. "
                    f"Affected: {advisory.get('vulnerable_versions')}; "
                    f"patched: {advisory.get('patched_versions')}. {advisory.get('url')}"
                )
        return result or ["Dependency audit failed without an advisory. Inspect the retained audit log."]

    lines = ANSI.sub("", raw).splitlines()
    if mode == "security-go":
        starts = [index for index, line in enumerate(lines) if line.startswith("Vulnerability #")]
        if starts:
            # Keep the ID, description and fixed version, rather than a long
            # call trace whose last line alone omits the dependency to update.
            return ["\n".join(lines[start : start + 10]) for start in starts]
    indices = [index for index, line in enumerate(lines) if ERROR.search(line.strip())]
    if not indices:
        return ["\n".join(lines[-20:]) or "The check failed before producing a log."]
    return ["\n".join(lines[max(0, index - 1) : index + 3]) for index in indices]


def annotation(title, message):
    message = message[:3500].replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A")
    return f"::error title={title}::{message}"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=TITLES)
    parser.add_argument("logs", nargs="+")
    args = parser.parse_args()
    seen = set()
    for name in args.logs:
        path = Path(name)
        if not path.is_file():
            continue
        # Audits are JSON; native logs can be large after a compiler failure.
        with path.open("rb") as stream:
            if args.mode != "security-web":
                stream.seek(max(0, path.stat().st_size - 128_000))
            raw = stream.read(2_000_000).decode("utf-8", errors="replace")
        for message in messages(args.mode, raw):
            if message not in seen and len(seen) < 10:
                seen.add(message)
                print(annotation(TITLES[args.mode], message))


if __name__ == "__main__":
    main()
