"""Exercise collection while two real supervised workers overlap."""
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


class ConcurrentSupervisorTests(unittest.TestCase):
    def test_empty_lock_file_serializes_contenders_without_initialization(self):
        with tempfile.TemporaryDirectory() as directory:
            parent = Path(directory).resolve()
            attempted, acquired = parent / "attempted", parent / "acquired"
            code = f"""
import importlib.util, pathlib
s = importlib.util.spec_from_file_location('g', {str(Path(guard.__file__))!r})
g = importlib.util.module_from_spec(s)
s.loader.exec_module(g)
pathlib.Path({str(attempted)!r}).touch()
with g.lifecycle_lock(pathlib.Path({str(parent)!r})):
    pathlib.Path({str(acquired)!r}).touch()
"""
            program = None
            try:
                with guard.lifecycle_lock(parent):
                    lock, = parent.glob(".coordica-supervisor-*.lock")
                    self.assertEqual(lock.stat().st_size, 0)
                    program = subprocess.Popen([sys.executable, "-c", code],
                                               stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
                    deadline = time.monotonic() + 10
                    while not attempted.exists():
                        if program.poll() is not None:
                            output, _ = program.communicate(timeout=5)
                            self.fail(output.decode(errors="replace"))
                        self.assertLess(time.monotonic(), deadline, "contender did not start")
                        time.sleep(.02)
                    # Keep the empty byte range locked while the contender
                    # tries to enter; initialization must not write through it.
                    time.sleep(.2)
                    self.assertFalse(acquired.exists())
                    self.assertIsNone(program.poll())
                output, _ = program.communicate(timeout=10)
                self.assertEqual(program.returncode, 0, output.decode(errors="replace"))
                self.assertTrue(acquired.exists())
                self.assertEqual(lock.stat().st_size, 0)
            finally:
                if program is not None:
                    if program.poll() is None:
                        program.kill()
                    program.communicate(timeout=10)

    def test_concurrent_supervisors_do_not_recover_each_others_work(self):
        with tempfile.TemporaryDirectory() as directory:
            parent = Path(directory).resolve()
            release = parent / "release"
            programs = []
            try:
                for number in range(2):
                    ready = parent / ("ready-" + str(number))
                    payload = (
                        "import pathlib,time\n"
                        f"pathlib.Path({str(ready)!r}).write_text('ready')\n"
                        f"while not pathlib.Path({str(release)!r}).exists(): time.sleep(.05)\n"
                    )
                    code = (
                        "import importlib.util,pathlib,sys; "
                        f"s=importlib.util.spec_from_file_location('g',{str(Path(guard.__file__))!r}); "
                        "g=importlib.util.module_from_spec(s);s.loader.exec_module(g); "
                        f"sys.exit(g.run([sys.executable,'-c',{payload!r}],pathlib.Path({str(parent)!r}),40,1,33554432))"
                    )
                    programs.append(subprocess.Popen(
                        [sys.executable, "-c", code], stdout=subprocess.PIPE, stderr=subprocess.STDOUT
                    ))
                deadline = time.monotonic() + 30
                while not all((parent / ("ready-" + str(n))).exists() for n in range(2)):
                    for number, program in enumerate(programs):
                        if program.poll() is not None:
                            output, _ = program.communicate(timeout=5)
                            self.fail(f"supervisor {number} exited before the shared barrier "
                                      f"(status {program.returncode}):\n" + output.decode(errors="replace"))
                    self.assertLess(time.monotonic(), deadline, "workers did not reach the shared barrier")
                    time.sleep(.05)
                guard.recover_stale(parent)
                self.assertEqual(len(list(parent.glob(guard.PREFIX + "*"))), 2)
                release.write_text("continue")
                for program in programs:
                    output, _ = program.communicate(timeout=30)
                    self.assertEqual(program.returncode, 0, output.decode(errors="replace"))
                self.assertEqual(list(parent.glob(guard.PREFIX + "*")), [])
            finally:
                release.touch()
                for program in programs:
                    if program.poll() is None:
                        program.kill()
                    program.communicate(timeout=10)
                guard.recover_stale(parent)
