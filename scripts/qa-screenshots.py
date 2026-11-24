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
import collections
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
TESTS = ("AppSweepQATests", "ScreensQATests", "testSignInScreenWithFaceID", "StartCookingQATests",
         "testComingBackFromTheBackgroundNeverAsksForFaceID", "ShopComponentQATests", "ShopPrepQATests",
         "MenuCardsQATests")
# Screens every config must have.
REQUIRED = ("Sign-in", "Sign-in › Face ID", "Menu", "Menu › By Day", "Shop", "Pantry", "Pantry › Add an Item",
            "Household", "Plus paywall",
            # The cooking screen's steps, on a phone's Steps tab or an iPad's steps column (build 122
            # round 4, Hark's item 15b: the iPads had none).
            "Start Cooking › Steps",
            # Nov 22's three, in every config since Hark's build 122 round 4 review (item 1).
            "Face ID › Back from the background", "Shop › Sauce with every item had", "Shop › Freeze card",
            # Both recipe pages scroll everywhere, the upright iPad included (the same review).
            "Recipe page › typed by hand › scrolled", "Recipe page › imported › scrolled",
            # Your Meals with no empty band under the cards (build 123, Merrill's Nov 22 Menu).
            "Menu › Your Meals at rest",
            # The cooking screen with a recipe's photo and its whole ingredient list (Merrill's
            # Nov 23 iPad report: a blank band for the photo and rows missing on TestFlight 120).
            "Cooking › a recipe with a photo",
            # The end of the cooking list for both recipes, and Your Meals with a meal made, in
            # every config (Hark's build 123 review, items 3 and 10).
            "Cooking › imported › end of ingredients", "Cooking › typed by hand › end of ingredients",
            "Menu › Your Meals with a meal made",
            # What sits under a bottom fade as a screen opens, seen whole in a shot of its own
            # (Hark's build 121 round 2 review, item 2): the end of Account, and the freeze card
            # screen's two cards brought up under the week bar. The typed recipe's Add Nutrition
            # shows whole in its Nutrition shot, so its More menu has no end shot.
            "Face ID › Back from the background › end", "Shop › Freeze card › order",
            "Shop › Freeze card › this week’s cost")
# Required screens a config may lack, each with its reason. Since Hark's build 123 review (item 3)
# every config has the same set but one: Account fits on the screen of a large iPhone and an iPad,
# so nothing of it is under the tab bar's fade and its end is the shot as it opens (the test leaves
# it out with a QA-SKIP line).
ACCOUNT_FITS = "Account fits on the screen: its end is the shot as it opens"
COVERAGE_EXCEPTIONS: dict = {
    (f"{device}-{mode}", "Face ID › Back from the background › end"): ACCOUNT_FITS
    for device in ("large", "ipadL", "ipadP") for mode in ("light", "dark")
}
DEVICES = {
    "large": ("iPhone 17 Pro Max", "portrait"),
    "small": ("iPhone SE", "portrait"),
    "duo": ("iPhone Duo cover screen", "portrait"),
    "ipadL": ("iPad", "landscape"),
    "ipadP": ("iPad", "portrait"),
}


def run(*args, **kw):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kw).stdout


def clean_png(path, landscape):
    """Drops EXIF and XMP chunks (an iPad landscape shot once carried an EXIF rotation that showed
    it sideways, build 120) and checks the image is the right way round. Returns (width, height)."""
    data = open(path, "rb").read()
    out, i = data[:8], 8
    width = height = 0
    while i < len(data):
        n = int.from_bytes(data[i:i + 4], "big")
        kind = data[i + 4:i + 8]
        if kind == b"IHDR":
            width = int.from_bytes(data[i + 8:i + 12], "big")
            height = int.from_bytes(data[i + 12:i + 16], "big")
        if kind not in (b"eXIf", b"iTXt"):
            out += data[i:i + 12 + n]
        i += 12 + n
    open(path, "wb").write(out)
    if landscape and width < height:
        raise SystemExit(f"{path} is {width}x{height}: a landscape screenshot must be wider than tall")
    return width, height


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


def failed_tests(folders):
    """The config and test of every test whose last result in the matrix summaries failed. A
    rerun folder passed later replaces the earlier result, as for the screenshots."""
    verdicts = {}
    for folder in folders:
        try:
            lines = open(os.path.join(folder, "summary.txt")).read().splitlines()
        except FileNotFoundError:
            continue
        for line in lines:
            m = re.match(r"^(\S+) (\S+): (ok|FAILED)\b", line)
            if m:
                verdicts[(m.group(1), m.group(2))] = m.group(3)
    return sorted(f"{c} {t}" for (c, t), v in verdicts.items() if v == "FAILED")


def wrong_build(build, folders):
    """The folders whose app was built with another number, or with none recorded (before
    build 121). TestFlight numbers builds itself (Hark, Nov 23), and the screenshots are matched
    to a phone by the number Household shows."""
    wrong = []
    for folder in folders:
        try:
            recorded = open(os.path.join(folder, "build-number")).read().strip()
        except OSError:
            recorded = "none"
        if recorded != build:
            wrong.append(f"{folder}: built as {recorded}")
    return wrong


def main():
    if len(sys.argv) < 3 or not sys.argv[1].isdigit():
        sys.exit(__doc__)
    build, folders = sys.argv[1], sys.argv[2:]
    # A failed test (the sweep's Menu, loading and blank-band checks among them) blocks the set,
    # and with it the TestFlight push that waits on it (Merrill, Nov 22: "this cannot happen
    # again").
    wrong = wrong_build(build, folders)
    if wrong:
        print("\n".join(wrong), file=sys.stderr)
        sys.exit(f"these runs weren't built as {build}: rerun with BUILD_NUMBER={build}")
    failed = failed_tests(folders)
    if failed:
        print("\n".join(failed), file=sys.stderr)
        sys.exit(f"{len(failed)} test(s) failed: rerun them (RERUN=) and pass the rerun folder after the run")
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
                size = clean_png(os.path.join(out, name), orientation == "landscape")
                manifest.append({
                    "file": name,
                    "screen": screen,
                    "config": config,
                    "device": device,
                    "state": f"{appearance}, {orientation}",
                    "size": f"{size[0]}x{size[1]}",
                    "test": test,
                })
    if not manifest:
        sys.exit("No screenshots found")
    json.dump({"build": int(build), "screenshots": manifest}, open(os.path.join(out, "manifest.json"), "w"), indent=1)
    configs = sorted({m["config"] for m in manifest})
    print(f"{len(manifest)} screenshots across {len(configs)} configs: {', '.join(configs)}")
    # Coverage: every config (each device in light and dark) and, per config, the screens every
    # config must have (build 121: duo-dark and three configs' Start Cooking were missing).
    expected = {f"{d}-{m}" for d in DEVICES for m in ("light", "dark")}
    problems = [f"no screenshots for {c}" for c in sorted(expected - set(configs))]
    screens = collections.defaultdict(set)
    for m in manifest:
        screens[m["config"]].add(re.sub(r" \((light|dark)\)$", "", m["screen"]))
    for c in configs:
        missing = [r for r in REQUIRED if r not in screens[c] and (c, r) not in COVERAGE_EXCEPTIONS]
        if missing:
            problems.append(f"{c} is missing: {', '.join(missing)}")
    if problems:
        print("\n".join(problems), file=sys.stderr)
        if os.environ.get("MISSING_OK") != "1":
            sys.exit("Screenshot coverage is incomplete (MISSING_OK=1 to publish anyway)")
    # The same screen twice in one config (status bar and home indicator aside, under 1% changed)
    # is never published: build 122 passed four such pairs because the clock had moved on.
    try:
        import qa_shot_compare
    except ImportError as error:
        sys.exit(f"Can't check for duplicate screens ({error}): pip3 install pillow numpy")
    duplicates = [qa_shot_compare.describe(os.path.join(out, c), pair)
                  for c in configs for pair in qa_shot_compare.folder_duplicates(os.path.join(out, c))]
    if duplicates:
        print("\n".join(duplicates), file=sys.stderr)
        sys.exit(f"{len(duplicates)} pair(s) of screenshots show the same screen: not publishing")
    print("No duplicate screens")
    # The named exceptions, so the review sees each one (Hark's build 122 round 4 review, item 10).
    for name, pair in qa_shot_compare.ALLOWED_PAIRS.items():
        print(f"Allowed look-alikes, {name}: {' and '.join(sorted(pair))}")
    for (config, screen), reason in sorted(COVERAGE_EXCEPTIONS.items()):
        if screen not in screens[config]:
            print(f"Not shot on {config}, {screen}: {reason}")
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
