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
