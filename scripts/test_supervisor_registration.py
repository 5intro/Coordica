"""Worker registration cannot replace a marker while a collector reads it."""
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


class RegistrationTests(unittest.TestCase):
    def test_registration_waits_for_an_active_marker_reader(self):
        with tempfile.TemporaryDirectory() as directory:
            parent = Path(directory).resolve()
            ready, release, executed = (parent / name for name in ("ready", "release", "executed"))
            code = f"""
import importlib.util, pathlib, sys, time
spec = importlib.util.spec_from_file_location('g', {str(Path(guard.__file__))!r})
g = importlib.util.module_from_spec(spec)
spec.loader.exec_module(g)
original = g.process_snapshot
calls = 0
def snapshot():
    global calls
    values = original()
    calls += 1
    if calls == 2:
        pathlib.Path({str(ready)!r}).touch()
        deadline = time.monotonic() + 20
        while not pathlib.Path({str(release)!r}).exists():
            if time.monotonic() > deadline:
                raise RuntimeError('registration barrier timed out')
            time.sleep(.02)
    return values
g.process_snapshot = snapshot
payload = "from pathlib import Path; Path(" + repr({str(executed)!r}) + ").touch()"
sys.exit(g.run([sys.executable, '-c', payload], pathlib.Path({str(parent)!r}), 30, 1, 33554432))
"""
            supervisor = subprocess.Popen([sys.executable, "-c", code],
                                          stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            try:
                deadline = time.monotonic() + 20
                while not ready.exists():
                    if supervisor.poll() is not None:
                        output, _ = supervisor.communicate(timeout=5)
                        self.fail(output.decode(errors="replace"))
                    self.assertLess(time.monotonic(), deadline, "worker did not reach registration barrier")
                    time.sleep(.02)
                roots = list(parent.glob(guard.PREFIX + "*"))
                self.assertEqual(len(roots), 1)
                with guard.lifecycle_lock(parent), (roots[0] / guard.MARKER).open("rb"):
                    release.touch()
                    # An ordinary open reader on Windows denies replacement.
                    # On Unix the lock must still keep an unregistered worker
                    # from starting while a collector reads the old marker.
                    deadline = time.monotonic() + 1
                    while time.monotonic() < deadline and not executed.exists():
                        time.sleep(.02)
                    self.assertFalse(executed.exists(), "worker ran before its collector released the lifecycle lock")
                output, _ = supervisor.communicate(timeout=30)
                self.assertEqual(supervisor.returncode, 0, output.decode(errors="replace"))
                self.assertTrue(executed.exists())
                self.assertEqual(list(parent.glob(guard.PREFIX + "*")), [])
            finally:
                release.touch()
                if supervisor.poll() is None:
                    supervisor.kill()
                supervisor.communicate(timeout=10)
                guard.recover_stale(parent)
