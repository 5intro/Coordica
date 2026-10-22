"""Failure-mode regressions for the local/CI Go test supervisor."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("test_go", Path(__file__).with_name("test-go.py"))
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


class TestSupervisor(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.parent = Path(self.temp.name).resolve()

    def tearDown(self):
        self.temp.cleanup()

    def run_code(self, code, **options):
        limits = dict(timeout=20, min_free=1, max_temp=32 * 1024 ** 2)
        limits.update(options)
        return guard.run([sys.executable, "-c", code], self.parent, **limits)

    def assert_clean(self):
        self.assertEqual(list(self.parent.glob(guard.PREFIX + "*")), [])

    def test_success_and_failure_reclaim_only_owned_root(self):
        sentinel = self.parent / "unrelated"
        sentinel.write_text("keep")
        for code in (0, 7):
            self.assertEqual(self.run_code(
                "import os,pathlib,sys; "
                "p=pathlib.Path(os.environ['TMPDIR']); "
                "assert str(p)==os.environ['GOTMPDIR']==os.environ['TEMP']==os.environ['TMP']; "
                "(p/'output').write_bytes(b'fixture'); "
                f"sys.exit({code})"), code)
            self.assert_clean()
        self.assertEqual(sentinel.read_text(), "keep")

    def test_timeout_stops_worker_and_reclaims_files(self):
        self.assertEqual(self.run_code("import time; time.sleep(60)", timeout=.1), 124)
        self.assert_clean()

    def test_owned_read_only_git_like_file_is_reclaimed(self):
        code = ("import os,pathlib,stat; "
                "p=pathlib.Path(os.environ['TMPDIR'])/'pack.idx'; "
                "p.write_bytes(b'fixture'); p.chmod(stat.S_IREAD)")
        self.assertEqual(self.run_code(code), 0)
        self.assert_clean()

    def test_low_disk_refuses_to_start(self):
        with patch.object(guard.subprocess, "Popen") as start:
            # Stale recovery has no candidates and needs no process query here.
            with patch.object(guard, "recover_stale"):
                with self.assertRaisesRegex(RuntimeError, "tests not started"):
                    self.run_code("raise AssertionError('must not run')", min_free=10 ** 30)
            start.assert_not_called()
        self.assert_clean()

    def test_free_space_reserve_stops_an_already_running_worker(self):
        disk_usage = shutil.disk_usage
        calls = 0

        def space_drops(path):
            nonlocal calls
            calls += 1
            usage = disk_usage(path)
            return usage._replace(free=0) if calls > 2 else usage

        with patch.object(guard.shutil, "disk_usage", space_drops):
            self.assertEqual(self.run_code("import time; time.sleep(60)"), 125)
        self.assert_clean()

    def test_reused_pid_does_not_authorize_termination(self):
        snapshot = {42: dict(pid=42, parent=1, start="new", exe=str(self.parent / "unrelated"))}
        root = self.parent / "owned"
        self.assertEqual(guard.owned_processes(snapshot, root, dict(pid=42, start="old")), [])

    def test_unregistered_worker_never_executes_command(self):
        sentinel = self.parent / "must-not-exist"
        code = f"from pathlib import Path; Path({str(sentinel)!r}).write_text('wrong')"
        result = subprocess.run([sys.executable, guard.__file__, "--worker", sys.executable, "-c", code],
                                stdin=subprocess.DEVNULL, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 1)
        self.assertFalse(sentinel.exists())

    def test_storage_limit_stops_producer_without_filling_disk(self):
        code = ("import os,pathlib,time; "
                "(pathlib.Path(os.environ['TMPDIR'])/'large').write_bytes(b'x'*2097152); "
                "time.sleep(60)")
        self.assertEqual(self.run_code(code, max_temp=1024 ** 2), 125)
        self.assert_clean()

    def test_interrupt_runs_cleanup_and_preserves_handler(self):
        previous = signal.getsignal(signal.SIGTERM)

        def interrupt_after_start(_):
            # Patching the global time.sleep can signal before the guard has
            # installed handlers: subprocess.wait also uses it on some OSes.
            signal.raise_signal(signal.SIGTERM)
            return 0

        with patch.object(guard, "allocated_bytes", interrupt_after_start):
            self.assertEqual(self.run_code("import time; time.sleep(60)"), 128 + signal.SIGTERM)
        self.assertEqual(signal.getsignal(signal.SIGTERM), previous)
        self.assert_clean()

    def test_detached_executable_is_stopped_and_makes_success_fail(self):
        pid_file = self.parent / "orphan.pid"
        # System binaries can be killed by macOS signing rules when copied,
        # and framework Python launchers re-exec outside the temp root. Build
        # one tiny native fixture to exercise the same case as a Go test alias.
        source = self.parent / "sleeper.go"
        source.write_text('package main\nimport "time"\nfunc main() { time.Sleep(time.Minute) }\n')
        executable = self.parent / ("sleeper.exe" if os.name == "nt" else "sleeper")
        go = str(guard.REPO / ".tools/go/bin/go") if (guard.REPO / ".tools/go/bin/go").exists() else shutil.which("go")
        self.assertIsNotNone(go)
        self.assertEqual(guard.run([go, "build", "-o", str(executable), str(source)],
                                  self.parent, 120, 8 * guard.GIB, 4 * guard.GIB), 0)
        args = []
        code = ("import os,pathlib,subprocess,shutil,time; "
                "root=pathlib.Path(os.environ['TMPDIR']); "
                f"exe=root/{('orphan.exe' if os.name == 'nt' else 'orphan')!r}; "
                f"shutil.copyfile({str(executable)!r},exe); os.chmod(exe,0o700); "
                f"p=subprocess.Popen([str(exe),*{args!r}], "
                "stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL, "
                f"start_new_session={os.name != 'nt'!r}); "
                "time.sleep(.2); assert p.poll() is None, 'orphan fixture failed to start'; "
                f"pathlib.Path({str(pid_file)!r}).write_text(str(p.pid))")
        try:
            self.assertEqual(self.run_code(code), 1)
            pid = int(pid_file.read_text())
            self.assertNotIn(pid, guard.process_snapshot())
            self.assert_clean()
        finally:
            if pid_file.exists():
                info = guard.process_snapshot().get(int(pid_file.read_text()))
                if info and guard.within(info["exe"], self.parent):
                    guard.terminate([info], self.parent)

    def test_stale_recovery_preserves_live_unknown_and_symlink_roots(self):
        stale = Path(tempfile.mkdtemp(prefix=guard.PREFIX, dir=self.parent))
        record = guard.owner_record(stale)
        record.update(pid=0, start="dead-owner")
        (stale / guard.MARKER).write_text(json.dumps(record))
        live = Path(tempfile.mkdtemp(prefix=guard.PREFIX, dir=self.parent))
        (live / guard.MARKER).write_text(json.dumps(guard.owner_record(live)))
        unknown = Path(tempfile.mkdtemp(prefix=guard.PREFIX, dir=self.parent))
        (unknown / "data").write_text("keep")
        outside = self.parent / "outside"
        outside.mkdir()
        link = self.parent / (guard.PREFIX + "symlink")
        if os.name != "nt":
            link.symlink_to(outside, target_is_directory=True)
        guard.recover_stale(self.parent)
        self.assertFalse(stale.exists())
        self.assertTrue(live.exists())
        self.assertEqual((unknown / "data").read_text(), "keep")
        self.assertTrue(outside.exists())

    def test_failed_shutdown_keeps_root_for_later_recovery(self):
        with patch.object(guard, "stop_processes", side_effect=RuntimeError("shutdown not confirmed")):
            with self.assertRaisesRegex(RuntimeError, "shutdown not confirmed"):
                self.run_code("import sys; sys.exit(0)")
        roots = list(self.parent.glob(guard.PREFIX + "*"))
        self.assertEqual(len(roots), 1)
        self.assertIsNotNone(guard.read_owner(roots[0]))

    def test_killed_supervisor_is_recovered_on_next_invocation(self):
        # The registered worker is deliberately outside its temp root. Its
        # persisted creation identity must recover it after supervisor death.
        ready = self.parent / "worker-ready"
        payload = f"import pathlib,time; pathlib.Path({str(ready)!r}).write_text('ready'); time.sleep(60)"
        code = ("import importlib.util,pathlib,sys; "
                f"s=importlib.util.spec_from_file_location('g',{str(Path(guard.__file__))!r}); "
                "g=importlib.util.module_from_spec(s);s.loader.exec_module(g); "
                f"g.run([sys.executable,'-c',{payload!r}],pathlib.Path({str(self.parent)!r}),60,1,33554432)")
        supervisor = subprocess.Popen([sys.executable, "-c", code], stdout=subprocess.DEVNULL,
                                      stderr=subprocess.DEVNULL)
        worker = None
        try:
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                for root in self.parent.glob(guard.PREFIX + "*"):
                    record = guard.read_owner(root)
                    if record and record.get("child"):
                        worker = record["child"]
                        break
                if worker and ready.exists():
                    break
                if supervisor.poll() is not None:
                    self.fail("supervisor exited before registering its worker")
                time.sleep(.05)
            self.assertIsNotNone(worker)
            supervisor.kill()
            supervisor.wait(timeout=10)
            guard.recover_stale(self.parent)
            self.assertNotEqual(guard.process_snapshot().get(worker["pid"], {}).get("start"), worker["start"])
            self.assert_clean()
        finally:
            if supervisor.poll() is None:
                supervisor.kill()
                supervisor.wait(timeout=10)
            guard.recover_stale(self.parent)

    def test_recovery_observes_new_owner_after_discovering_its_marker(self):
        root = Path(tempfile.mkdtemp(prefix=guard.PREFIX, dir=self.parent))
        owner = guard.owner_record(root)
        (root / guard.MARKER).write_text(json.dumps(owner))
        observed = []
        read = guard.read_owner
        snapshot = guard.process_snapshot

        def read_first(path):
            observed.append("marker")
            return read(path)

        def fresh_snapshot():
            self.assertIn("marker", observed, "snapshot predates a newly discovered supervisor")
            return snapshot()

        with patch.object(guard, "read_owner", read_first), patch.object(guard, "process_snapshot", fresh_snapshot):
            with patch.object(guard, "stop_processes") as stop:
                guard.recover_stale(self.parent)
                stop.assert_not_called()
        self.assertTrue(root.exists())

    def test_external_orphan_survives_parent_exit_but_not_test_cleanup(self):
        # This executable is outside the owned root. Its parent deliberately
        # exits before the supervisor can observe parent-child ancestry.
        evidence = self.parent / "outside-orphan.json"
        code = ("import json,pathlib,subprocess,sys; "
                "p=subprocess.Popen([sys.executable,'-c','import time;time.sleep(30)'], "
                "stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL); "
                f"pathlib.Path({str(evidence)!r}).write_text(json.dumps({{'pid':p.pid}}))")
        self.assertEqual(self.run_code(code), 1)
        self.assertNotIn(json.loads(evidence.read_text())["pid"], guard.process_snapshot())
        self.assert_clean()

    def test_legacy_marker_without_containment_proof_is_preserved(self):
        root = Path(tempfile.mkdtemp(prefix=guard.PREFIX, dir=self.parent))
        record = guard.owner_record(root)
        record.update(version=1, pid=0, start="dead")
        (root / guard.MARKER).write_text(json.dumps(record))
        guard.recover_stale(self.parent)
        self.assertTrue(root.exists())


if __name__ == "__main__":
    unittest.main()
