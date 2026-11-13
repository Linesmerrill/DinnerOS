#!/usr/bin/env python3
"""Fingerprints the website's CSS and JS so browsers never pair a new page with an old file.

website/assets/site.css and website/assets/menu.js are the files to edit. This writes
site.<hash>.css and menu.<hash>.js next to them, removes older fingerprinted copies, and points
every page at the new names. _headers caches fingerprinted files for a year and pages not at all.

    python3 scripts/website-assets.py          # update after editing site.css or menu.js
    python3 scripts/website-assets.py --check  # fail if a page or copy is out of date
"""

import hashlib
import pathlib
import re
import sys

SITE = pathlib.Path(__file__).resolve().parent.parent / "website"
SOURCES = ["assets/site.css", "assets/menu.js"]


def fingerprinted(source: str) -> tuple[pathlib.Path, str]:
    path = SITE / source
    digest = hashlib.sha256(path.read_bytes()).hexdigest()[:10]
    return path, f"{path.stem}.{digest}{path.suffix}"


def link_pattern(path: pathlib.Path) -> re.Pattern:
    # /assets/site.css, /assets/site.css?v=123, or /assets/site.0123456789.css
    stem, suffix = re.escape(path.stem), re.escape(path.suffix)
    return re.compile(rf"/assets/{stem}(?:\.[0-9a-f]{{10}})?{suffix}(?:\?v=\d+)?")


def main() -> int:
    check = "--check" in sys.argv[1:]
    pages = sorted(SITE.rglob("*.html"))
    problems = []
    for source in SOURCES:
        path, name = fingerprinted(source)
        copy = path.with_name(name)
        stale = [p for p in path.parent.glob(f"{path.stem}.*{path.suffix}")
                 if re.fullmatch(rf"{re.escape(path.stem)}\.[0-9a-f]{{10}}{re.escape(path.suffix)}", p.name)
                 and p.name != name]
        if check:
            if not copy.exists() or copy.read_bytes() != path.read_bytes():
                problems.append(f"{copy.relative_to(SITE)} is missing or out of date")
            problems += [f"{p.relative_to(SITE)} is an old copy" for p in stale]
        else:
            copy.write_bytes(path.read_bytes())
            for p in stale:
                p.unlink()
        want = f"/assets/{name}"
        for page in pages:
            text = page.read_text()
            found = set(link_pattern(path).findall(text))
            if not found or found == {want}:
                continue
            if check:
                problems.append(f"{page.relative_to(SITE)} links {', '.join(sorted(found))}, not {want}")
            else:
                page.write_text(link_pattern(path).sub(want, text))
    if check:
        for page in pages:
            for ref in re.findall(r'(?:href|src)="(/assets/[^"]+)"', page.read_text()):
                if not (SITE / ref.lstrip("/").split("?")[0]).exists():
                    problems.append(f"{page.relative_to(SITE)} links missing {ref}")
        for p in problems:
            print(p, file=sys.stderr)
        if problems:
            print("Run: python3 scripts/website-assets.py", file=sys.stderr)
            return 1
        print("website assets: up to date")
    return 0


if __name__ == "__main__":
    sys.exit(main())
