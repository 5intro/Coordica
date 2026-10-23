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
                    self.assertTrue(all(p.poll() is None for p in programs), "supervisor exited before the shared barrier")
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
