"""Encode the two ordered Playwright clips; usage: python3 ... <test-output-dir>."""
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

source = Path(sys.argv[1]).resolve()
chapters = json.loads((source / "chapters.json").read_text())
target = Path(__file__).resolve().parent

def run(*args):
    subprocess.run(args, check=True)

def chapter_start(prefix, offset):
    return str(next(item["seconds"] for item in chapters if item["title"].startswith(prefix)) + offset)

with tempfile.TemporaryDirectory(prefix="lester-walkthrough-") as directory:
    work = Path(directory)
    # Normalize timestamps between the closed tab and its replacement.
    for number in (1, 2):
        run("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", str(source / f"part-0{number}.webm"),
            "-an", "-vf", "scale=in_range=auto:out_range=tv", "-color_range", "tv",
            "-c:v", "libx264", "-preset", "slow", "-crf", "22", "-pix_fmt", "yuv420p", str(work / f"part-{number}.mp4"))
    (work / "concat.txt").write_text("file 'part-1.mp4'\nfile 'part-2.mp4'\n")
    movie = target / "lester-walkthrough.mp4"
    run("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "concat", "-safe", "0", "-i", str(work / "concat.txt"),
        "-c", "copy", "-movflags", "+faststart", str(movie))
    # Fourteen-second sampler of actual footage; the MP4 keeps the complete story.
    run("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
        "-ss", chapter_start("05 /", 2), "-t", "6", "-i", str(movie),
        "-ss", chapter_start("09 /", .75), "-t", "4", "-i", str(movie),
        "-ss", chapter_start("11 /", 1.5), "-t", "4", "-i", str(movie),
        "-filter_complex", "[0:v]setpts=PTS-STARTPTS[v0];[1:v]setpts=PTS-STARTPTS[v1];[2:v]setpts=PTS-STARTPTS[v2];[v0][v1][v2]concat=n=3:v=1:a=0,fps=6,scale=720:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=96[p];[b][p]paletteuse=dither=bayer",
        "-loop", "0", str(target / "lester-walkthrough.gif"))
    shutil.copyfile(source / "poster.png", target / "lester-walkthrough.png")
    duration = subprocess.check_output(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", str(movie)], text=True).strip()
    print(f"Duration: {duration}s. Update README chapters from:")
    print(json.dumps(json.loads((source / "chapters.json").read_text()), indent=2))
