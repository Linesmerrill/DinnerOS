"""Tests for qa_shot_compare (the duplicate-screen check before QA screenshots are published).

    python3 scripts/test_qa_shot_compare.py
"""
import os
import sys
import tempfile
import unittest

import numpy as np
from PIL import Image

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import qa_shot_compare as compare  # noqa: E402

# iPhone 17 Pro Max in points; at 3x, 1320x2868 pixels.
WIDTH, HEIGHT = 440, 956


def screen(device="large", width=WIDTH, height=HEIGHT, color=(255, 255, 255)):
    """A blank screen in pixels for the device."""
    scale = compare.SCALES[device]
    return np.full((height * scale, width * scale, 3), color, dtype=np.uint8)


def paint(pixels, device, x, y, w, h, color=(0, 0, 0)):
    """Fills a rectangle given in points."""
    scale = compare.SCALES[device]
    pixels[y * scale:(y + h) * scale, x * scale:(x + w) * scale] = color
    return pixels


class CompareTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.mkdtemp(prefix="large-light-")

    def save(self, name, pixels):
        path = os.path.join(self.folder, name)
        Image.fromarray(pixels).save(path)
        return path

    def share(self, a, b, device="large"):
        pa, pb = self.save("a.png", a), self.save("b.png", b)
        return compare.changed_share(compare.load(pa, device), compare.load(pb, device), device)

    def test_a_different_clock_is_still_the_same_screen(self):
        page = paint(screen(), "large", 20, 200, 300, 40)
        other_clock = paint(page.copy(), "large", 30, 15, 50, 20, (40, 40, 40))
        self.assertEqual(self.share(page, other_clock), 0)

    def test_the_home_indicator_is_left_out(self):
        page = screen()
        bar = paint(page.copy(), "large", 150, HEIGHT - 10, 140, 5)
        self.assertEqual(self.share(page, bar), 0)

    def test_the_duo_status_bar_in_the_rail_is_left_out(self):
        duo = screen("duo", 466, 678)
        clock = paint(duo.copy(), "duo", 395, 80, 60, 20)
        self.assertEqual(self.share(duo, clock, "duo"), 0)
        # Below the status bar the rail's buttons count.
        button = paint(duo.copy(), "duo", 395, 200, 60, 60)
        self.assertGreater(self.share(duo, button, "duo"), 0)

    def test_a_menu_of_thin_text_is_a_different_screen(self):
        # Ten lines of 1-point-high text across 120 points: few pixels, many cells.
        page = screen()
        menu = page.copy()
        for line in range(10):
            paint(menu, "large", 300, 100 + line * 20, 120, 1)
        pixels = np.abs(page.astype(int) - menu.astype(int)).max(axis=2) > compare.CHANNEL_TOLERANCE
        self.assertLess(pixels.mean(), compare.SAME_SCREEN)
        self.assertGreaterEqual(self.share(page, menu), compare.SAME_SCREEN)

    def test_faint_changes_under_the_tolerance_dont_count(self):
        page = screen(color=(200, 200, 200))
        tinted = paint(page.copy(), "large", 0, 70, WIDTH, 300, (210, 210, 190))
        self.assertEqual(self.share(page, tinted), 0)

    def test_screens_of_different_sizes_differ(self):
        a = compare.load(self.save("a.png", screen()), "large")
        b = compare.load(self.save("b.png", screen(width=430, height=932)), "large")
        self.assertEqual(compare.changed_share(a, b, "large"), 1.0)

    def test_folder_reports_only_the_duplicate_pair(self):
        recipe = paint(screen(), "large", 20, 120, 400, 300, (200, 120, 40))
        self.save("recipe-page.png", recipe)
        self.save("start-cooking.png", paint(recipe.copy(), "large", 30, 15, 50, 20))  # clock only
        self.save("cooking.png", paint(screen(), "large", 20, 400, 400, 300, (30, 120, 60)))
        found = compare.folder_duplicates(self.folder, "large")
        self.assertEqual([(os.path.basename(a), os.path.basename(b)) for a, b, _ in found],
                         [("recipe-page.png", "start-cooking.png")])
        self.assertIn("are the same screen", compare.describe(self.folder, found[0]))

    def test_the_quick_look_never_drops_a_duplicate(self):
        # Cell averages only rule a pair out when the averages prove it: a near copy passes on to
        # the full check.
        page = paint(screen(), "large", 20, 200, 300, 200, (90, 90, 90))
        near = paint(page.copy(), "large", 20, 200, 8, 8, (0, 0, 0))
        a, b = compare.load(self.save("a.png", page), "large"), compare.load(self.save("b.png", near), "large")
        self.assertFalse(compare.surely_different(compare.cell_means(a), compare.cell_means(b), "large", 0.01))
        self.assertEqual(len(compare.duplicates([self.save("p.png", page), self.save("n.png", near)], "large")), 1)

    def test_the_device_comes_from_the_folder_name(self):
        self.assertEqual(compare.device_of("ipadL-dark"), "ipadL")
        kept = compare.kept_cells("ipadL", 834, 1210)
        self.assertFalse(kept[0].any())  # the status bar row of cells
        self.assertFalse(kept[-1].any())  # the home indicator
        self.assertTrue(kept[5].all())


if __name__ == "__main__":
    unittest.main()
