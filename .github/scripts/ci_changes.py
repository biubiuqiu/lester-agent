"""Choose CI jobs without relying on a third-party action or API token."""

import json
import os
import re
import subprocess
from pathlib import Path

JOBS = ("go", "web", "gateway", "helm", "security_go", "security_web", "workflows")


def changed_paths(event_name, event, repo="."):
    """Return both sides of renames; None means the comparison is uncertain."""
    if event_name == "pull_request":
        base = event["pull_request"]["base"]["sha"]
        head = event["pull_request"]["head"]["sha"]
    elif event_name == "push":
        base, head = event.get("before", ""), event.get("after", "")
    else:
        return None
    if any(not re.fullmatch(r"[0-9a-f]{40,64}", sha) or not int(sha, 16) for sha in (base, head)):
        return None
    try:
        if event_name == "pull_request":
            base = subprocess.check_output(["git", "merge-base", base, head], cwd=repo, text=True).strip()
        diff = subprocess.check_output(
            ["git", "diff", "--name-only", "--no-renames", "-z", base, head, "--"], cwd=repo
        )
        return [path.decode("utf-8", errors="surrogateescape") for path in diff.split(b"\0") if path]
    except subprocess.CalledProcessError:
        return None


def select_jobs(event_name, paths):
    jobs = dict.fromkeys(JOBS, False)
    if event_name == "schedule":
        jobs.update(security_go=True, security_web=True)
        return jobs
    if event_name == "workflow_dispatch" or paths is None:
        return dict.fromkeys(JOBS, True)
    for path in paths:
        if path.startswith(".github/"):
            return dict.fromkeys(JOBS, True)
        # Built-in Skills also use Markdown, but are embedded runtime inputs.
        if (path.startswith("docs/") or ("/" not in path and path.endswith(".md"))
                or Path(path).name in ("README.md", "README.zh-CN.md", "AGENTS.md")
                or path in ("backend/ARCHITECTURE.md", ".gitignore", ".gitattributes", "LICENSE")):
            continue
        if path.startswith("backend/"):
            jobs.update(go=True, security_go=True)
        elif path.startswith("frontend/"):
            jobs["web"] = True
            if path in ("frontend/package.json", "frontend/pnpm-lock.yaml", "frontend/pnpm-workspace.yaml"):
                jobs["security_web"] = True
        elif path.startswith("deploy/gateway/") or path.startswith("deploy/docker-compose") or path == "deploy/.env.example":
            jobs["gateway"] = True
        elif path.startswith("deploy/helm/"):
            jobs["helm"] = True
        else:
            # New root/build configuration gets full coverage until classified.
            return dict.fromkeys(JOBS, True)
    return jobs


def main():
    event_name = os.environ["GITHUB_EVENT_NAME"]
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
    paths = changed_paths(event_name, event)
    jobs = select_jobs(event_name, paths)
    if paths is None and event_name not in ("schedule", "workflow_dispatch"):
        print("Commit comparison unavailable; running all checks.")
    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
        for job, enabled in jobs.items():
            output.write(f"{job}={str(enabled).lower()}\n")
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as summary:
        summary.write("### Selected checks\n\n")
        summary.write("\n".join(f"- {job}: {'run' if enabled else 'skip'}" for job, enabled in jobs.items()) + "\n")


if __name__ == "__main__":
    main()
