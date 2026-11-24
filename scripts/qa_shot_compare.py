"""Finds QA screenshots in one config that show the same screen (Hark's build 122 review, item 13a).

Two shots of the same screen used to pass because the clock in the status bar had moved on. Here
the status bar and the home indicator are left out, each shot is sampled at one pixel per point
and cut into 8-point cells, and two shots count as the same screen when fewer than 1% of the cells
changed. A cell changes when any of its pixels moves by more than 24 (of 255) in a color channel.

Cells rather than single pixels: two different screens can differ in thin text on white only (a
menu opened over the Menu, a different household's name), which is under 1% of pixels but 3% or
more of cells, while the same screen changes no cells, or a few under the toolbar's glass.

The same rule runs at capture time in the UI tests (QAShotCompare in
ios/DinnerOSUITests/QAHelpers.swift); keep the two in step.

    python3 scripts/qa_shot_compare.py <config folder> [...]   # prints duplicate pairs, exits 1 on any

A config folder is named <device>-<appearance>, as qa-screenshots.py writes them. Needs Pillow and
NumPy.

A few pairs are different screens that differ in little (ALLOWED_PAIRS, Hark's review, build 122
round 4 item 15c): they are never reported. Keep the list in step with QAShotCompare.allowedPairs.
"""
import itertools
import os
import sys

import numpy as np
from PIL import Image

# Fewer than this share of cells changed: the same screen.
SAME_SCREEN = 0.01
# A pixel changed when a color channel moves by more than this (0-255).
CHANNEL_TOLERANCE = 24
# Side of a cell, in points.
CELL = 8

# Areas left out per device, in points: (top, bottom, rail). Top is the status bar, bottom the
# home indicator. On the iPhone Duo cover screen the status bar sits at the top of the right-hand
# rail (from x = 382 pt, the top 160 pt), not across the top.
MASKS = {
    "large": (62, 34, None),
    "small": (20, 0, None),
    "duo": (0, 0, (382, 160)),
    "ipadL": (24, 20, None),
    "ipadP": (24, 20, None),
}
# Pairs of screens, by file name without ".png", that are different screens though under 1% of
# cells may change: on an iPad the search for one recipe and for another show one result card each
# in a wide, mostly empty page; the Household tab of a household without Plus differs from the
# main test household's only in its name, its initials and its Plus line.
ALLOWED_PAIRS = {
    # Two searches, one result card each: on an iPad the rest of the Menu is the same.
    "Menu search, one card each": frozenset({"menu-search-typed-imported", "menu-search-typed-typed-by-hand"}),
    # A household without Plus differs only in its name, initials and Plus line.
    "Household with and without Plus": frozenset({"household-new-household", "household"}),
    # On an iPad Butter and the flour already show on the tab, so the "scrolled to" shot barely
    # moves the list, or not at all (Hark's build 121 review, item 18). A phone's pair differs.
    "Pantry with Butter in view": frozenset({"pantry", "pantry-scrolled-to-butter"}),
    "Shop with the flour in view": frozenset({"shop", "shop-scrolled-to-flour"}),
    # Account fits on a large iPhone's and an iPad's screen, so its end is the screen as it opens
    # (Hark's build 121 round 2 review, item 2: every config has the end shot).
    "Account that fits on the screen": frozenset(
        {"face-id-back-from-the-background", "face-id-back-from-the-background-end"}),
}
# Pixels per point by device.
SCALES = {"large": 3, "small": 2, "duo": 3, "ipadL": 2, "ipadP": 2}


def device_of(config):
    """'ipadL-dark' -> 'ipadL'."""
    return config.split("-")[0]


def kept_cells(device, height, width):
    """A (rows, columns) boolean array for an image of height x width points: True for each whole
    cell clear of the status bar, home indicator and Duo rail."""
    top, bottom, rail = MASKS.get(device, MASKS["large"])
    rows, columns = height // CELL, width // CELL
    tops = np.arange(rows) * CELL
    lefts = np.arange(columns) * CELL
    keep = np.ones((rows, columns), dtype=bool)
    keep[tops < top, :] = False
    if bottom:
        keep[tops + CELL > height - bottom, :] = False
    if rail:
        x, h = rail
        keep[np.ix_(tops < h, lefts + CELL > x)] = False
    return keep


def load(path, device):
    """The image at one pixel per point (sampled), as an int16 RGB array."""
    step = SCALES.get(device, 3)
    with Image.open(path) as image:
        pixels = np.asarray(image.convert("RGB"), dtype=np.int16)
    return pixels[::step, ::step]


def changed_share(a, b, device):
    """The share of kept cells (0 to 1) in which some pixel changed; 1 for different sizes."""
    if a.shape != b.shape:
        return 1.0
    keep = kept_cells(device, a.shape[0], a.shape[1])
    rows, columns = keep.shape
    moved = np.abs(a - b).max(axis=2) > CHANNEL_TOLERANCE
    moved = moved[: rows * CELL, : columns * CELL].reshape(rows, CELL, columns, CELL).any(axis=(1, 3))
    total = int(keep.sum())
    return float((moved & keep).sum()) / total if total else 0.0


def cell_means(pixels):
    """The average color of each cell, (rows, columns, 3)."""
    rows, columns = pixels.shape[0] // CELL, pixels.shape[1] // CELL
    cells = pixels[: rows * CELL, : columns * CELL].reshape(rows, CELL, columns, CELL, 3)
    return cells.mean(axis=(1, 3))


def surely_different(means_a, means_b, device, threshold):
    """True when the cell averages alone prove at least `threshold` of the cells changed: a cell
    whose average moved by more than the tolerance in a channel has a pixel that did too."""
    if means_a.shape != means_b.shape:
        return True
    keep = kept_cells(device, means_a.shape[0] * CELL, means_a.shape[1] * CELL)
    moved = (np.abs(means_a - means_b).max(axis=2) > CHANNEL_TOLERANCE) & keep
    total = int(keep.sum())
    return total > 0 and moved.sum() / total >= threshold


def screen_name(path):
    """'ipadL-dark/menu-search-typed-imported.png' -> 'menu-search-typed-imported'."""
    return os.path.splitext(os.path.basename(path))[0]


def allowed_name(first, second):
    """The name of the exception (ALLOWED_PAIRS) for a pair of different screens that may look
    alike, or None."""
    pair = frozenset({screen_name(first), screen_name(second)})
    return next((name for name, allowed in ALLOWED_PAIRS.items() if allowed == pair), None)


def is_allowed(first, second):
    """True for a pair of different screens that may look alike (ALLOWED_PAIRS)."""
    return allowed_name(first, second) is not None


def duplicates(paths, device, threshold=SAME_SCREEN):
    """Pairs of paths, with the share of cells changed, that show the same screen. Allowed pairs
    are left out."""
    images = {path: load(path, device) for path in paths}
    means = {path: cell_means(pixels) for path, pixels in images.items()}
    found = []
    for first, second in itertools.combinations(sorted(images), 2):
        if is_allowed(first, second):
            continue
        if surely_different(means[first], means[second], device, threshold):
            continue
        share = changed_share(images[first], images[second], device)
        if share < threshold:
            found.append((first, second, share))
    return found


def folder_duplicates(folder, device=None):
    """Duplicate pairs among the PNGs in one config folder."""
    device = device or device_of(os.path.basename(os.path.normpath(folder)))
    paths = [os.path.join(folder, n) for n in sorted(os.listdir(folder)) if n.endswith(".png")]
    return duplicates(paths, device)


def describe(folder, pair):
    first, second, share = pair
    return (f"{os.path.basename(os.path.normpath(folder))}: {os.path.basename(first)} and "
            f"{os.path.basename(second)} are the same screen ({share:.2%} changed)")


def main(folders):
    lines = [describe(folder, pair) for folder in folders for pair in folder_duplicates(folder)]
    print("\n".join(lines) if lines else "No duplicate screens")
    return 1 if lines else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
