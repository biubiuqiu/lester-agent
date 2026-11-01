"""Stage only checked, public reading pages for GitHub Pages."""

import argparse
from pathlib import Path
import shutil
import subprocess
import sys


def stage(output):
    repo = Path(__file__).resolve().parents[2]
    research = repo / "research"
    package = research / "agent-context-management"
    subprocess.run([sys.executable, str(package / "scripts/build.py"), "--check"], check=True)
    sources = {"index.html": research / "index.html",
               "agent-context-management/index.html": package / "index.html",
               ".nojekyll": research / ".nojekyll"}
    if any(not source.is_file() or source.is_symlink() for source in sources.values()):
        raise ValueError("Every published page must be a regular, existing source file")
    output = output.resolve()
    if output == repo or output in repo.parents or research == output or research in output.parents:
        raise ValueError("The Pages staging directory must be outside the research source tree")
    if output.exists():
        if not output.is_dir() or any(output.iterdir()):
            raise ValueError("Use a new or empty Pages staging directory")
    output.mkdir(parents=True, exist_ok=True)
    for relative, source in sources.items():
        target = output / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)
    print(f"Staged {len(sources)} reading files in {output}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    stage(parser.parse_args().output)

