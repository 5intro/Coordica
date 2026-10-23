"""Distinguish delayed Windows Job accounting from persistent descendants."""
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location("test_go", Path(__file__).with_name("test-go.py"))
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


class JobDrainTests(unittest.TestCase):
    def test_exited_command_can_settle_after_its_wait_receipt(self):
        job = Mock()
        job.active.side_effect = [2, 1]
        with patch.object(guard.time, "sleep"):
            self.assertEqual(guard.settled_job_members(job, anchor=1), 0)
        self.assertEqual(job.active.call_count, 2)

    def test_persistent_descendant_is_still_reported(self):
        job = Mock()
        job.active.return_value = 2
        with patch.object(guard.time, "monotonic", side_effect=[0, 0, 2]), \
                patch.object(guard.time, "sleep"):
            self.assertEqual(guard.settled_job_members(job, anchor=1), 1)

    def test_unknown_job_state_cannot_count_as_quiet(self):
        job = Mock()
        job.active.side_effect = OSError("job accounting unavailable")
        with self.assertRaisesRegex(OSError, "unavailable"):
            guard.settled_job_members(job, anchor=1)
