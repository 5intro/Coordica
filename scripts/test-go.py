#!/usr/bin/env python3
"""Run Go tests in one owned temporary root, with bounded disk/time and cleanup.

No global cache or other temporary directory is ever removed. A failed cleanup
keeps its ownership marker so the next invocation can recover that exact root.
"""
import argparse
from contextlib import contextmanager
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time
import threading

REPO = Path(__file__).resolve().parent.parent
MARKER = ".coordica-test-owner.json"
PREFIX = "ct-" if os.name == "nt" else "coordica-test-"
RESULT = ".coordica-test-result.json"
GIB = 1024 ** 3
DEFAULT_TEMP_PARENT = Path(os.environ.get("RUNNER_TEMP", str(Path.home()))) if os.name == "nt" else Path(tempfile.gettempdir())


@contextmanager
def lifecycle_lock(parent):
    # Outside removable roots; OS locks release automatically after a crash.
    name = ".coordica-supervisor-" + hashlib.sha256(str(REPO).encode()).hexdigest()[:16] + ".lock"
    fd = os.open(parent / name, os.O_RDWR | os.O_CREAT | getattr(os, "O_NOFOLLOW", 0), 0o600)
    locked = False
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise RuntimeError("test lifecycle lock is not a regular file")
        if os.fstat(fd).st_size == 0:
            os.write(fd, b"0")
        deadline = time.monotonic() + 30
        while not locked:
            try:
                os.lseek(fd, 0, os.SEEK_SET)
                if os.name == "nt":
                    import msvcrt
                    msvcrt.locking(fd, msvcrt.LK_NBLCK, 1)
                else:
                    import fcntl
                    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                locked = True
            except OSError:
                if time.monotonic() >= deadline:
                    raise RuntimeError("another test lifecycle operation is still active")
                time.sleep(.05)
        yield
    finally:
        if locked:
            if os.name == "nt":
                import msvcrt
                os.lseek(fd, 0, os.SEEK_SET)
                msvcrt.locking(fd, msvcrt.LK_UNLCK, 1)
            else:
                import fcntl
                fcntl.flock(fd, fcntl.LOCK_UN)
        os.close(fd)


def remove_root(root):
    def readonly_file(function, path, exception):
        error = exception[1]
        candidate = Path(path)
        if os.name != "nt" or not isinstance(error, PermissionError) or candidate.is_symlink() or not candidate.resolve().is_relative_to(root.resolve()):
            raise error
        # Git packfiles are read-only on Windows. Only an already-stopped,
        # explicitly owned root may have this attribute cleared for deletion.
        candidate.chmod(stat.S_IREAD | stat.S_IWRITE)
        function(path)

    shutil.rmtree(root, onerror=readonly_file)


def process_snapshot():
    """Read process identity, ancestry and executable only; never read env/argv."""
    if os.name == "nt":
        script = ("[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false); "
                  "Get-CimInstance Win32_Process | ForEach-Object { "
                  "[pscustomobject]@{pid=$_.ProcessId;parent=$_.ParentProcessId;"
                  "start=$(if ($_.CreationDate) {$_.CreationDate.ToUniversalTime().ToString('o')} else {''});"
                  "exe=$_.ExecutablePath} } | ConvertTo-Json -Compress")
        result = subprocess.run(["powershell.exe", "-NoProfile", "-NonInteractive",
                                 "-Command", script], capture_output=True, text=True, encoding="utf-8",
                                timeout=20, check=True)
        values = json.loads(result.stdout or "[]")
        if isinstance(values, dict):
            values = [values]
        return {int(x["pid"]): x for x in values}
    result = subprocess.run(["ps", "-axo", "pid=,ppid=,lstart=,stat=,comm="],
                            capture_output=True, text=True, timeout=10, check=True)
    values = {}
    for line in result.stdout.splitlines():
        fields = line.strip().split(None, 8)
        if len(fields) == 9 and not fields[7].startswith("Z"):
            pid, parent = map(int, fields[:2])
            executable, started = fields[8], " ".join(fields[2:7])
            if sys.platform.startswith("linux"):
                try:
                    executable = os.readlink(f"/proc/{pid}/exe")
                    # Linux comm is only a basename; /proc supplies both the
                    # full executable and a finer identity than ps's seconds.
                    stat = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()
                    started = stat[19]
                except (OSError, IndexError):
                    continue
            try:
                group, session = os.getpgid(pid), os.getsid(pid)
            except ProcessLookupError:
                continue
            values[pid] = dict(pid=pid, parent=parent, start=started, exe=executable, group=group, session=session)
    return values


def within(executable, root):
    if not executable:
        return False
    try:
        return Path(executable).resolve().is_relative_to(root.resolve())
    except (OSError, ValueError):
        return False


def owned_processes(snapshot, root, child_identity=None):
    owned = {pid for pid, info in snapshot.items() if within(info["exe"], root)}
    if child_identity and snapshot.get(child_identity["pid"], {}).get("start") == child_identity["start"]:
        owned.add(child_identity["pid"])
        # A live, birth-checked session leader anchors orphaned system commands
        # after their direct parent exits. Never seed a reused numeric group.
        if child_identity.get("session") == child_identity["pid"]:
            owned.update(pid for pid, info in snapshot.items() if info.get("session") == child_identity["pid"])
    while True:
        descendants = {pid for pid, info in snapshot.items() if info["parent"] in owned} - owned
        if not descendants:
            return [snapshot[pid] for pid in owned if pid in snapshot]
        owned.update(descendants)


def terminate(infos, root):
    for info in infos:
        # A PID may have been reused between snapshots.
        current = process_snapshot().get(info["pid"])
        if current is None or current["start"] != info["start"]:
            continue
        if os.name == "nt":
            # Use native PID termination after identity checks. Enumerated
            # descendants are handled individually, without a broad /T kill.
            result = subprocess.run(["taskkill.exe", "/F", "/PID", str(info["pid"])],
                                    capture_output=True, timeout=10)
            if result.returncode and process_snapshot().get(info["pid"], {}).get("start") == info["start"]:
                detail = (result.stdout + result.stderr).decode(errors="replace")[-2000:]
                raise RuntimeError(f"Windows test shutdown failed in {root}: {detail}")
        else:
            try:
                if info["pid"] != os.getpid():
                    os.kill(info["pid"], signal.SIGSTOP)
                os.kill(info["pid"], signal.SIGKILL)
            except ProcessLookupError:
                pass


def stop_processes(root, child=None, child_identity=None, job=None):
    stopped = 0
    if job is not None:
        stopped = max(0, job.active() - int(child is not None and child.poll() is None))
        job.stop()
        if child is not None:
            child.wait(timeout=5)
    if os.name != "nt" and child_identity and child_identity["pid"] != os.getpid():
        anchor = process_snapshot().get(child_identity["pid"], {})
        if anchor.get("start") == child_identity["start"]:
            try:
                os.kill(child_identity["pid"], signal.SIGSTOP)
            except ProcessLookupError:
                pass
    for _ in range(20):
        snapshot = process_snapshot()
        if child_identity and child_identity.get("session") == child_identity["pid"]:
            anchor = snapshot.get(child_identity["pid"], {})
            if anchor.get("start") != child_identity["start"] and any(info.get("session") == child_identity["pid"] for info in snapshot.values()):
                # The anchor disappeared (possibly while its crash cleanup was
                # killing children). Wait for quiescence; a numeric session
                # without a live birth identity is never permission to kill.
                time.sleep(.05)
                continue
        owned = owned_processes(snapshot, root, child_identity)
        if not owned:
            if child is not None:
                child.wait(timeout=5)
            return stopped
        others = [info for info in owned if not child_identity or info["pid"] != child_identity["pid"]]
        # Keep the session anchor alive until no descendants remain. Killing it
        # first could hide newly reparented commands on the next snapshot.
        terminate(others or owned, root)
        stopped += len(others)
        if child is not None:
            try:
                child.wait(timeout=1)
            except subprocess.TimeoutExpired:
                pass
        time.sleep(.05)
    raise RuntimeError(f"test process shutdown not confirmed; retained {root}")


def owner_record(root):
    own = process_snapshot().get(os.getpid())
    if own is None or not own["start"]:
        raise RuntimeError("cannot establish test supervisor identity")
    return dict(version=2, repo=str(REPO), root=str(root.resolve()),
                pid=os.getpid(), start=own["start"])


def read_owner(root):
    marker = root / MARKER
    if root.is_symlink() or not root.is_dir() or marker.is_symlink() or not marker.is_file():
        return None
    if marker.stat().st_size > 4096:
        return None
    try:
        record = json.loads(marker.read_text())
        if (record.get("version") == 2 and record.get("repo") == str(REPO)
                and record.get("root") == str(root.resolve())
                and type(record.get("pid")) is int and isinstance(record.get("start"), str) and record["start"]):
            child = record.get("child")
            if child is not None and (not isinstance(child, dict) or type(child.get("pid")) is not int or child["pid"] <= 0 or not isinstance(child.get("start"), str) or not child["start"]):
                return None
            return record
    except (OSError, ValueError):
        pass
    return None


def recover_stale(parent):
    with lifecycle_lock(parent):
        for root in parent.glob(PREFIX + "*"):
            record = read_owner(root)
            if record is None:
                continue
            # Read only after this root's marker, under the same lifecycle lock
            # used by creators and other collectors. A pre-glob snapshot races
            # with a new supervisor's process and marker.
            if process_snapshot().get(record["pid"], {}).get("start") == record["start"]:
                continue
            if read_owner(root) != record:
                continue
            stop_processes(root, child_identity=record.get("child"))
            if read_owner(root) != record:
                raise RuntimeError(f"test ownership marker changed; retained {root}")
            remove_root(root)
            print(f"Recovered stopped test root: {root}", flush=True)


def allocated_bytes(root):
    total = 0
    seen = set()
    for directory, _, names in os.walk(root, followlinks=False):
        for name in names:
            try:
                path = Path(directory) / name
                stat = path.lstat()
                identity = (stat.st_dev, stat.st_ino)
                if path.is_symlink() or identity in seen:
                    continue
                seen.add(identity)
                total += getattr(stat, "st_blocks", 0) * 512 or stat.st_size
            except FileNotFoundError:
                continue
    return total


def run(command, parent, timeout, min_free, max_temp):
    parent = parent.resolve()
    recover_stale(parent)
    for volume in (parent, REPO):
        if shutil.disk_usage(volume).free < min_free:
            raise RuntimeError(f"insufficient free disk space at {volume}; tests not started")
    with lifecycle_lock(parent):
        root = Path(tempfile.mkdtemp(prefix=PREFIX, dir=parent))
        record = owner_record(root)
        (root / MARKER).write_text(json.dumps(record))
    child = None
    child_identity = None
    job = None
    received_signal = []
    old_handlers = {}
    for sig in (signal.SIGINT, signal.SIGTERM):
        old_handlers[sig] = signal.signal(sig, lambda number, _: received_signal.append(number))
    exit_code = 1
    try:
        env = dict(os.environ, TMPDIR=str(root), TMP=str(root), TEMP=str(root), GOTMPDIR=str(root))
        print(f"Test temporary root: {root}", flush=True)
        # The worker blocks on stdin until its creation identity is durable.
        # If the supervisor dies before that point, EOF exits without testing.
        child = subprocess.Popen([sys.executable, str(Path(__file__).resolve()),
                                  "--worker", *command], cwd=REPO, env=env,
                                 stdin=subprocess.PIPE, start_new_session=os.name != "nt")
        identity = process_snapshot().get(child.pid)
        if identity is None:
            raise RuntimeError("test worker exited before registration")
        child_identity = dict(pid=child.pid, start=identity["start"])
        if os.name == "nt":
            spec = importlib.util.spec_from_file_location("coordica_test_job", Path(__file__).with_name("test_job.py"))
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            job = module.WindowsJob()
            job.assign(child.pid)
        else:
            if identity["session"] != child.pid:
                raise RuntimeError("test worker did not establish an independent session")
            child_identity["session"] = child.pid
        record["child"] = child_identity
        pending = root / (MARKER + ".pending")
        pending.write_text(json.dumps(record))
        pending.replace(root / MARKER)
        child.stdin.write(b"G")
        child.stdin.flush()
        deadline = time.monotonic() + timeout
        while True:
            result_path = root / RESULT
            if result_path.exists():
                if result_path.stat().st_size > 4096:
                    raise RuntimeError("oversized test exit receipt")
                result = json.loads(result_path.read_text())
                if result.get("child") != child_identity or type(result.get("code")) is not int:
                    raise RuntimeError("test exit receipt identity mismatch")
                exit_code = result["code"]
                break
            if child.poll() is not None:
                raise RuntimeError("test worker exited without a completion receipt")
            if received_signal:
                exit_code = 128 + received_signal[0]
                print("Test supervisor interrupted; stopping owned processes", file=sys.stderr)
                break
            if time.monotonic() >= deadline:
                exit_code = 124
                print("Test wall-clock deadline exceeded", file=sys.stderr)
                break
            if any(shutil.disk_usage(p).free < min_free for p in (root, REPO)):
                exit_code = 125
                print("Free disk reserve reached; stopping tests", file=sys.stderr)
                break
            if allocated_bytes(root) > max_temp:
                exit_code = 125
                print("Test temporary storage limit exceeded; stopping tests", file=sys.stderr)
                break
            time.sleep(.5)
    finally:
        try:
            stopped = stop_processes(root, child, child_identity, job)
            if read_owner(root) != record:
                raise RuntimeError(f"test ownership marker changed; retained {root}")
            remove_root(root)
            print("Owned test processes stopped; temporary root removed", flush=True)
            if stopped and exit_code == 0:
                exit_code = 1
                print("Tests left running processes; cleanup succeeded but the run fails", file=sys.stderr)
        except BaseException:
            # Closing the pipe releases the Unix guardian's crash cleanup;
            # closing a Job kills its Windows members. Reap our direct child
            # but retain evidence if shutdown could not be established.
            if child is not None:
                if child.stdin is not None and not child.stdin.closed:
                    child.stdin.close()
                if job is not None:
                    job.close()
                try:
                    child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    pass
            print(f"Cleanup incomplete; retained owned test root: {root}", file=sys.stderr)
            raise
        finally:
            if child is not None and child.stdin is not None:
                child.stdin.close()
            if job is not None:
                job.close()
            for sig, handler in old_handlers.items():
                signal.signal(sig, handler)
    return exit_code if exit_code >= 0 else 128 - exit_code


def positive(value):
    number = float(value)
    if not math.isfinite(number) or number <= 0:
        raise argparse.ArgumentTypeError("must be finite and positive")
    return number


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go", help="Go executable")
    parser.add_argument("--temp-parent", type=Path, default=DEFAULT_TEMP_PARENT)
    parser.add_argument("--timeout-seconds", type=positive, default=1200)
    parser.add_argument("--min-free-gib", type=positive, default=8)
    parser.add_argument("--max-temp-gib", type=positive, default=4)
    parser.add_argument("args", nargs=argparse.REMAINDER, help="arguments after -- go to go test")
    args = parser.parse_args()
    go_args = args.args[1:] if args.args[:1] == ["--"] else args.args
    command = [args.go, "test", *(go_args or ["-race", "-timeout=10m", "./..."])]
    try:
        return run(command, args.temp_parent, args.timeout_seconds,
                   max(1, int(args.min_free_gib * GIB)), max(1, int(args.max_temp_gib * GIB)))
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"Test safety failure: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    if sys.argv[1:2] == ["--worker"]:
        if sys.stdin.buffer.read(1) != b"G":
            sys.exit(1)
        root = Path(os.environ["GOTMPDIR"])
        record = read_owner(root)
        identity = record.get("child") if record else None
        if not identity or identity["pid"] != os.getpid() or os.name != "nt" and os.getsid(0) != os.getpid():
            sys.exit(1)
        def parent_gone():
            sys.stdin.buffer.read(1)
            if os.name != "nt":
                # We created this process group/session ourselves. Parent death
                # must not let ordinary external commands outlive the guardian.
                stop_processes(root, child_identity=identity)
        # Start the command before the crash watcher: the guardian must not
        # fork a new child while that watcher is taking its shutdown snapshot.
        command = subprocess.Popen(sys.argv[2:], stdin=subprocess.DEVNULL)
        watcher = threading.Thread(target=parent_gone, daemon=True)
        watcher.start()
        code = command.wait()
        pending = root / (RESULT + ".pending")
        pending.write_text(json.dumps({"child": identity, "code": code}))
        pending.replace(root / RESULT)
        # Remain a live process/session anchor until supervisor cleanup. EOF
        # also releases this guardian after a crashed supervisor's command ends.
        watcher.join()
        sys.exit(0)
    sys.exit(main())
