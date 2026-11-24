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


if __name__ == "__main__":
    unittest.main()


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


class RequiredTest(unittest.TestCase):
    def test_every_config_needs_the_common_screens(self):
        for config in ("large-light", "small-dark", "duo-light", "ipadL-dark", "ipadP-light"):
            self.assertTrue(set(qa.REQUIRED) <= set(qa.required(config)))

    def test_the_ipads_need_the_scrolled_steps(self):
        self.assertIn("Start Cooking › Steps scrolled", qa.required("ipadL-light"))
        self.assertIn("Start Cooking › Steps scrolled", qa.required("ipadP-dark"))
        self.assertNotIn("Start Cooking › Steps scrolled", qa.required("small-light"))

    def test_the_short_screens_need_the_middle_of_the_typed_list(self):
        middle = "Cooking › typed by hand › middle of ingredients"
        self.assertIn(middle, qa.required("duo-dark"))
        self.assertIn(middle, qa.required("small-light"))
        self.assertNotIn(middle, qa.required("large-light"))
        self.assertNotIn(middle, qa.required("ipadL-light"))
