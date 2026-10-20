"""Guard the trusted comparison base, including already committed branch work."""
import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("check_quality", Path(__file__).with_name("check-quality.py"))
quality = importlib.util.module_from_spec(spec)
spec.loader.exec_module(quality)


class QualityCommandTests(unittest.TestCase):
    def test_ci_uses_event_base_and_fixed_candidate(self):
        with patch.dict(quality.os.environ, {"QUALITY_BASE": "a" * 40, "CI": "true"}, clear=True), \
                patch.object(quality.subprocess, "check_output", return_value="b" * 40), \
                patch.object(quality.subprocess, "run") as run:
            run.return_value.returncode = 1
            with self.assertRaises(SystemExit) as result:
                quality.main()
            self.assertEqual(result.exception.code, 1)
            args = run.call_args.args[0]
            self.assertEqual(args[args.index("--base") + 1], "a" * 40)
            self.assertEqual(args[args.index("--candidate") + 1], "b" * 40)

    def test_local_check_includes_committed_branch_debt(self):
        with patch.dict(quality.os.environ, {}, clear=True), \
                patch.object(quality.subprocess, "check_output", return_value="c" * 40) as read, \
                patch.object(quality.subprocess, "run") as run:
            run.return_value.returncode = 0
            with self.assertRaises(SystemExit):
                quality.main()
            self.assertIn("refs/remotes/origin/main^{commit}", read.call_args.args[0])
            self.assertIn("--working-tree", run.call_args.args[0])

    def test_unknown_initial_push_cannot_self_baseline(self):
        with patch.dict(quality.os.environ, {"QUALITY_BASE": "0" * 40, "CI": "true"}, clear=True), \
                patch.object(quality.subprocess, "check_output", side_effect=subprocess.CalledProcessError(1, "git")), \
                patch.object(quality.subprocess, "run") as run:
            with self.assertRaises(SystemExit):
                quality.main()
            run.assert_not_called()

    def test_new_branch_push_uses_target_main_not_its_own_head(self):
        with patch.dict(quality.os.environ, {"QUALITY_BASE": "0" * 40, "CI": "true"}, clear=True), \
                patch.object(quality.subprocess, "check_output", side_effect=["a" * 40, "b" * 40]), \
                patch.object(quality.subprocess, "run") as run:
            run.return_value.returncode = 0
            with self.assertRaises(SystemExit):
                quality.main()
            args = run.call_args.args[0]
            self.assertEqual(args[args.index("--base") + 1], "a" * 40)
            self.assertEqual(args[args.index("--candidate") + 1], "b" * 40)


if __name__ == "__main__":
    unittest.main()
