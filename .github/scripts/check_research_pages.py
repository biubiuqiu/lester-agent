"""Check Pages readiness before publishing, then verify the deployed reading URLs."""

import argparse
import json
import os
import subprocess
import sys
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urljoin, urlsplit
from urllib.request import Request, urlopen

MARKERS = ("Lester Research", "Lester / Research", "上下文管理：从抽象到实现")


def read_page(url):
    if urlsplit(url).scheme != "https":
        raise ValueError("Pages URL must use HTTPS")
    with urlopen(Request(url, headers={"User-Agent": "Lester-Research-Pages"}), timeout=15) as response:
        return response.read(2_000_000).decode("utf-8")


def preflight():
    repository = os.environ["GITHUB_REPOSITORY"]
    result = subprocess.run(["gh", "api", f"repos/{repository}/pages"], capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(f"Pages settings could not be read. If Pages is not enabled, set Settings → Pages → Source to GitHub Actions, then rerun this workflow: https://github.com/{repository}/settings/pages")
    settings = json.loads(result.stdout)
    if settings.get("build_type") != "workflow":
        raise RuntimeError("Pages is using a different publishing source; leave the existing site unchanged. Select GitHub Actions explicitly before rerunning.")
    url = settings["html_url"]
    try:
        current = read_page(url)
    except HTTPError as error:
        if error.code == 404:
            print("Pages is configured; no existing homepage is published.")
            return
        raise
    if not any(marker in current for marker in MARKERS):
        raise RuntimeError(f"An existing, unrelated Pages homepage is present at {url}; stopped without replacing it.")
    print(f"Existing research Pages homepage confirmed: {url}")


def verify(base):
    base = base.rstrip("/") + "/"
    checks = [(base, "Lester Research"),
              (urljoin(base, "agent-context-management/"), "上下文管理：从抽象到实现")]
    for attempt in range(12):
        try:
            for url, marker in checks:
                if marker not in read_page(url):
                    raise RuntimeError(f"Expected research content is missing at {url}")
            print(f"Verified published research homepage and report: {base}")
            return
        except (HTTPError, URLError, TimeoutError, RuntimeError) as error:
            if attempt == 11:
                raise RuntimeError("Pages publication returned, but HTTP content verification did not pass") from error
            time.sleep(5)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("preflight", "verify"))
    parser.add_argument("url", nargs="?")
    args = parser.parse_args()
    try:
        if args.mode == "preflight":
            preflight()
        elif args.url:
            verify(args.url)
        else:
            parser.error("verify requires the deployment URL")
    except (ValueError, KeyError, RuntimeError, HTTPError, URLError, TimeoutError) as error:
        print(f"::error::{error}", file=sys.stderr)
        sys.exit(1)

