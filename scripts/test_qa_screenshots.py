"""Tests for qa-screenshots.py's failed-test gate."""
import importlib.util
import os
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("qa_screenshots", os.path.join(os.path.dirname(__file__), "qa-screenshots.py"))
qa = importlib.util.module_from_spec(spec)
spec.loader.exec_module(qa)


class FailedTestsTest(unittest.TestCase):
    def folder(self, text):
        path = tempfile.mkdtemp()
        with open(os.path.join(path, "summary.txt"), "w") as f:
            f.write(text)
        return path

    def test_a_failure_blocks_the_set(self):
        run = self.folder("small-dark AppSweepQATests/SWEEP: FAILED (0 passed, 1 failed)\nlarge-dark ShopPrepQATests: ok (1 passed, 0 failed)\n")
        self.assertEqual(qa.failed_tests([run]), ["small-dark AppSweepQATests/SWEEP"])

    def test_a_passing_rerun_replaces_the_failure(self):
        run = self.folder("small-dark AppSweepQATests/SWEEP: FAILED (0 passed, 1 failed)\n")
        rerun = self.folder("small-dark AppSweepQATests/SWEEP: ok (1 passed, 0 failed)\n")
        self.assertEqual(qa.failed_tests([run, rerun]), [])

    def test_skips_and_notes_are_not_failures(self):
        run = self.folder("ipadP-dark ReviewAccountQATests: skipped (export REVIEW_ACCOUNT_EMAIL)\ndone in 1776s\n")
        self.assertEqual(qa.failed_tests([run]), [])



class WrongBuildTest(unittest.TestCase):
    def folder(self, number):
        path = tempfile.mkdtemp()
        if number is not None:
            with open(os.path.join(path, "build-number"), "w") as f:
                f.write(number + "\n")
        return path

    def test_runs_built_as_the_number_pass(self):
        self.assertEqual(qa.wrong_build("121", [self.folder("121"), self.folder("121")]), [])

    def test_another_number_or_none_blocks_the_set(self):
        other, none = self.folder("120"), self.folder(None)
        wrong = qa.wrong_build("121", [self.folder("121"), other, none])
        self.assertEqual(wrong, [f"{other}: built as 120", f"{none}: built as none"])


class CoverageTest(unittest.TestCase):
    ROWS = ["Pantry › scrolled to Butter", "Shop › scrolled to Flour"]

    def test_every_config_needs_the_butter_and_flour_rows(self):
        # One identical set for every config (Hark's build 123 review, item 3): iPads included.
        for config in ("small-light", "duo-dark", "large-light", "ipadL-light", "ipadP-dark"):
            have = set(qa.REQUIRED) - set(self.ROWS)
            self.assertEqual(qa.missing_screens(config, have), self.ROWS)
            self.assertEqual(qa.missing_screens(config, set(qa.REQUIRED)), [])

    def test_no_device_has_a_set_of_its_own(self):
        self.assertFalse(hasattr(qa, "REQUIRED_ON"))
        self.assertEqual(qa.COVERAGE_EXCEPTIONS, {})

    def test_the_ipad_look_alikes_are_allowed(self):
        import qa_shot_compare
        self.assertEqual(qa_shot_compare.allowed_name("ipadL-light/pantry.png", "ipadL-light/pantry-scrolled-to-butter.png"),
                         "Pantry with Butter in view")
        self.assertEqual(qa_shot_compare.allowed_name("ipadP-dark/shop-scrolled-to-flour.png", "ipadP-dark/shop.png"),
                         "Shop with the flour in view")
        self.assertIsNone(qa_shot_compare.allowed_name("a/pantry.png", "a/shop-scrolled-to-flour.png"))
        # The published file names are what the sweep's screenshot names slug to.
        self.assertEqual(qa.slug("Pantry › scrolled to Butter"), "pantry-scrolled-to-butter")
        self.assertEqual(qa.slug("Shop › scrolled to Flour"), "shop-scrolled-to-flour")

    def test_the_sweep_and_the_swift_list_match(self):
        root = os.path.join(os.path.dirname(__file__), "..", "ios", "DinnerOSUITests")
        sweep = open(os.path.join(root, "AppSweepQATests.swift")).read()
        self.assertIn('"Pantry": "Butter", "Shop": "All-Purpose Flour"', sweep)
        self.assertIn("› scrolled to", sweep)
        helpers = open(os.path.join(root, "QAHelpers.swift")).read()
        self.assertIn('"Pantry with Butter in view": ["pantry", "pantry-scrolled-to-butter"]', helpers)
        self.assertIn('"Shop with the flour in view": ["shop", "shop-scrolled-to-flour"]', helpers)


if __name__ == "__main__":
    unittest.main()
