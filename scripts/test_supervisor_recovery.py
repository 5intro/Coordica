"""Crash recovery of a registered supervisor worker across process boundaries."""
import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

spec = importlib.util.spec_from_file_location("test_go", Path(__file__).with_name("test-go.py"))
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


class CrashRecoveryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.parent = Path(self.temp.name).resolve()

    def tearDown(self):
        self.temp.cleanup()

    def assert_clean(self):
        self.assertEqual(list(self.parent.glob(guard.PREFIX + "*")), [])

    def test_killed_supervisor_is_recovered_on_next_invocation(self):
        # The registered worker is deliberately outside its temp root. Its
        # persisted creation identity must recover it after supervisor death.
        ready = self.parent / "worker-ready"
        payload = f"import pathlib,time; pathlib.Path({str(ready)!r}).write_text('ready'); time.sleep(60)"
        code = ("import importlib.util,pathlib,sys; "
                f"s=importlib.util.spec_from_file_location('g',{str(Path(guard.__file__))!r}); "
                "g=importlib.util.module_from_spec(s);s.loader.exec_module(g); "
                f"g.run([sys.executable,'-c',{payload!r}],pathlib.Path({str(self.parent)!r}),60,1,33554432)")
        supervisor = subprocess.Popen([sys.executable, "-c", code], stdout=subprocess.PIPE,
                                      stderr=subprocess.STDOUT)
        worker = None
        try:
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                with guard.lifecycle_lock(self.parent):
                    for root in self.parent.glob(guard.PREFIX + "*"):
                        record = guard.read_owner(root)
                        if record and record.get("child"):
                            worker = record["child"]
                            break
                if worker and ready.exists():
                    break
                if supervisor.poll() is not None:
                    output, _ = supervisor.communicate(timeout=5)
                    self.fail("supervisor exited before registering its worker:\n" + output.decode(errors="replace"))
                time.sleep(.05)
            self.assertIsNotNone(worker)
            self.assertTrue(ready.exists(), "registered worker never started the fixture")
            supervisor.kill()
            supervisor.communicate(timeout=10)
            guard.recover_stale(self.parent)
            self.assertNotEqual(guard.process_snapshot().get(worker["pid"], {}).get("start"), worker["start"])
            self.assert_clean()
        finally:
            if supervisor.poll() is None:
                supervisor.kill()
            supervisor.communicate(timeout=10)
            guard.recover_stale(self.parent)
