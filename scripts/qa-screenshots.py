#!/usr/bin/env python3
"""Publishes a build's QA screenshots for Hark's review (docs/deployment.md, "Screenshot review").

    scripts/qa-screenshots.py 120 /path/to/qa-run [/path/to/rerun ...]

Takes the screenshots from the QA matrix results (scripts/qa-matrix.sh OUT folders; for a config
and test in more than one, the last folder wins, so pass reruns after the full run), writes one
PNG per screen per config plus manifest.json, zips them, attaches the zip to a temporary draft
release, and runs the "QA screenshots" workflow, which publishes the artifact
qa-screenshots-<build> and deletes the draft. Prints "SCREENSHOTS READY build <n>, run <id>".
Nothing is committed. Set DRY_RUN=1 to stop after writing the folder and zip.
"""
import calendar
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time

REPO = "Linesmerrill/DinnerOS"
# The tests whose screenshots are screens: the tab sweeps, the deeper screens and sheets, the
# sign-in screen with and without Face ID, and Start Cooking on a scrolled recipe.
TESTS = ("AppSweepQATests", "ScreensQATests", "testSignInScreenWithFaceID", "StartCookingQATests")
DEVICES = {
    "large": ("iPhone 17 Pro Max", "portrait"),
    "small": ("iPhone SE", "portrait"),
    "duo": ("iPhone Duo cover screen", "portrait"),
    "ipadL": ("iPad", "landscape"),
    "ipadP": ("iPad", "portrait"),
}


def run(*args, **kw):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kw).stdout


def slug(text):
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")[:80] or "screen"


def results(folders):
    """(config, test) -> xcresult path, later folders winning."""
    found = {}
    for folder in folders:
        for name in sorted(os.listdir(folder)):
            if not name.endswith(".xcresult") or not any(t in name for t in TESTS):
                continue
            config = "-".join(name.split("-")[:2])
            if config.split("-")[0] not in DEVICES:
                continue
            test = name[len(config) + 1 : -len(".xcresult")]
            found[(config, test)] = os.path.join(folder, name)
    return found


def main():
    if len(sys.argv) < 3 or not sys.argv[1].isdigit():
        sys.exit(__doc__)
    build, folders = sys.argv[1], sys.argv[2:]
    work = tempfile.mkdtemp(prefix=f"qa-screenshots-{build}-")
    out = os.path.join(work, f"qa-screenshots-{build}")
    os.makedirs(out)
    manifest, seen = [], set()
    for (config, test), path in sorted(results(folders).items()):
        export = os.path.join(work, "export", f"{config}-{test}")
        os.makedirs(export)
        subprocess.run(
            ["xcrun", "xcresulttool", "export", "attachments", "--path", path, "--output-path", export],
            capture_output=True)
        try:
            attachments = json.load(open(os.path.join(export, "manifest.json")))
        except (OSError, ValueError):
            print(f"no screenshots in {path}", file=sys.stderr)
            continue
        device, orientation = DEVICES[config.split("-")[0]]
        appearance = config.split("-")[1]
        for entry in attachments:
            for a in sorted(entry.get("attachments", []), key=lambda a: a.get("timestamp", 0)):
                if not a.get("exportedFileName", "").endswith(".png"):
                    continue
                label = a.get("suggestedHumanReadableName", "")
                label = re.sub(r"_\d+_[0-9A-F-]+\.png$", "", label)
                screen = re.sub(r"\s*\((light|dark)\)$", "", label).strip()
                if not screen or screen.lower().startswith("debug"):
                    continue
                name = f"{config}/{slug(screen)}.png"
                n = 2
                while name in seen:
                    name, n = f"{config}/{slug(screen)}-{n}.png", n + 1
                seen.add(name)
                os.makedirs(os.path.join(out, config), exist_ok=True)
                shutil.copy(os.path.join(export, a["exportedFileName"]), os.path.join(out, name))
                manifest.append({
                    "file": name,
                    "screen": screen,
                    "config": config,
                    "device": device,
                    "state": f"{appearance}, {orientation}",
                    "test": test,
                })
    if not manifest:
        sys.exit("No screenshots found")
    json.dump({"build": int(build), "screenshots": manifest}, open(os.path.join(out, "manifest.json"), "w"), indent=1)
    configs = sorted({m["config"] for m in manifest})
    print(f"{len(manifest)} screenshots across {len(configs)} configs: {', '.join(configs)}")
    archive = shutil.make_archive(out, "zip", out)
    print(f"Folder: {out}\nZip: {archive} ({os.path.getsize(archive) // 1_000_000} MB)")
    if os.environ.get("DRY_RUN") == "1":
        return

    tag = f"qa-screenshots-{build}-{int(time.time())}"
    run("gh", "release", "create", tag, archive, "--repo", REPO, "--draft",
        "--title", f"QA screenshots for build {build} (temporary)",
        "--notes", "Temporary: the QA screenshots workflow publishes this as an artifact and deletes it.")
    started = time.time()
    run("gh", "workflow", "run", "qa-screenshots.yml", "--repo", REPO, "--ref", "main",
        "-f", f"build={build}", "-f", f"release={tag}")
    run_id = None
    for _ in range(30):
        time.sleep(5)
        runs = json.loads(run("gh", "run", "list", "--repo", REPO, "--workflow", "qa-screenshots.yml",
                              "--limit", "5", "--json", "databaseId,createdAt,displayTitle"))
        recent = [r for r in runs
                  if calendar.timegm(time.strptime(r["createdAt"], "%Y-%m-%dT%H:%M:%SZ")) >= started - 60]
        if recent:
            run_id = recent[0]["databaseId"]
            break
    if not run_id:
        sys.exit("The workflow run didn't start")
    subprocess.run(["gh", "run", "watch", str(run_id), "--repo", REPO, "--exit-status", "--interval", "15"],
                   stdout=subprocess.DEVNULL, check=True)
    artifacts = json.loads(run("gh", "api", f"repos/{REPO}/actions/runs/{run_id}/artifacts"))
    names = [a["name"] for a in artifacts.get("artifacts", [])]
    if f"qa-screenshots-{build}" not in names:
        sys.exit(f"Run {run_id} finished without the artifact")
    print(f"SCREENSHOTS READY build {build}, run {run_id}")


if __name__ == "__main__":
    main()
