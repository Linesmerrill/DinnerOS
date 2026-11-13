#!/usr/bin/env python3
"""Swaps the website's TestFlight beta links for the App Store listing, on launch day.

    python3 scripts/website-launch.py --app-id 1234567890          # rewrite the pages
    python3 scripts/website-launch.py --app-id 1234567890 --check  # list what would change

Every "Get the free beta" button becomes "Download on the App Store" pointing at
https://apps.apple.com/app/id<app-id>. If website/assets/app-store-badge.svg exists (Apple's
official badge, from https://developer.apple.com/app-store/marketing/guidelines/), the home
page's hero button becomes the badge image instead. Pages that talk about the beta in their
text (the FAQ) are listed so their words can be updated by hand.
"""

import argparse
import pathlib
import re
import sys

SITE = pathlib.Path(__file__).resolve().parent.parent / "website"
BETA_URL = "https://testflight.apple.com/join/cyVY8K6r"
BETA_BUTTON = re.compile(r'(<a class="button[^"]*" href=")' + re.escape(BETA_URL) + r'(">)Get the free beta(</a>)')
BADGE = "assets/app-store-badge.svg"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--app-id", required=True, help="the numeric Apple ID from App Store Connect")
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    if not args.app_id.isdigit():
        print("The App ID is the number in App Store Connect, App Information, Apple ID.", file=sys.stderr)
        return 2
    store_url = f"https://apps.apple.com/app/id{args.app_id}"
    has_badge = (SITE / BADGE).exists()
    by_hand = []
    for page in sorted(SITE.rglob("*.html")):
        text = page.read_text()
        new = BETA_BUTTON.sub(lambda m: f"{m.group(1)}{store_url}{m.group(2)}Download on the App Store{m.group(3)}", text)
        if has_badge and page.name == "index.html" and page.parent == SITE:
            new = new.replace(
                f'<a class="button" href="{store_url}">Download on the App Store</a>',
                f'<a class="store-badge" href="{store_url}"><img src="/{BADGE}" width="156" height="52" '
                'alt="Download on the App Store"></a>',
                1,
            )
        if BETA_URL in new:
            by_hand.append(page.relative_to(SITE))
        if new != text:
            print(f"{'would update' if args.check else 'updated'} {page.relative_to(SITE)}")
            if not args.check:
                page.write_text(new)
    for page in by_hand:
        print(f"still mentions the beta, edit by hand: {page}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
