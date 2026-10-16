#!/usr/bin/env python3
"""Run Go tests in one owned temporary root, with bounded disk/time and cleanup.

No global cache or other temporary directory is ever removed. A failed cleanup
keeps its ownership marker so the next invocation can recover that exact root.
"""
import argparse
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

REPO = Path(__file__).resolve().parent.parent
MARKER = ".coordica-test-owner.json"
PREFIX = "ct-" if os.name == "nt" else "coordica-test-"
GIB = 1024 ** 3
DEFAULT_TEMP_PARENT = Path(os.environ.get("RUNNER_TEMP", str(Path.home()))) if os.name == "nt" else Path(tempfile.gettempdir())


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
            values[pid] = dict(pid=pid, parent=parent, start=started, exe=executable)
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
                os.kill(info["pid"], signal.SIGSTOP)
                os.kill(info["pid"], signal.SIGKILL)
            except ProcessLookupError:
                pass


def stop_processes(root, child=None, child_identity=None):
    stopped = 0
    for _ in range(6):
        owned = owned_processes(process_snapshot(), root, child_identity)
        if not owned:
            if child is not None:
                child.wait(timeout=5)
            return stopped
        terminate(owned, root)
        stopped += len(owned)
        if child is not None:
            try:
                child.wait(timeout=1)
            except subprocess.TimeoutExpired:
                pass
        time.sleep(.05)
    raise RuntimeError(f"test process shutdown not confirmed; retained {root}")


def owner_record(root):
    own = process_snapshot().get(os.getpid())
    if own is None:
        raise RuntimeError("cannot establish test supervisor identity")
    return dict(version=1, repo=str(REPO), root=str(root.resolve()),
                pid=os.getpid(), start=own["start"])


def read_owner(root):
    marker = root / MARKER
    if root.is_symlink() or not root.is_dir() or marker.is_symlink() or not marker.is_file():
        return None
    if marker.stat().st_size > 4096:
        return None
    try:
        record = json.loads(marker.read_text())
        if (record.get("version") == 1 and record.get("repo") == str(REPO)
                and record.get("root") == str(root.resolve())
                and type(record.get("pid")) is int and isinstance(record.get("start"), str)):
            return record
    except (OSError, ValueError):
        pass
    return None


def recover_stale(parent):
    snapshot = process_snapshot()
    for root in parent.glob(PREFIX + "*"):
        record = read_owner(root)
        if record is None:
            continue
        if snapshot.get(record["pid"], {}).get("start") == record["start"]:
            continue  # Another live invocation owns this root.
        stop_processes(root, child_identity=record.get("child"))
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
    root = Path(tempfile.mkdtemp(prefix=PREFIX, dir=parent))
    record = owner_record(root)
    (root / MARKER).write_text(json.dumps(record))
    child = None
    child_identity = None
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
        record["child"] = child_identity
        pending = root / (MARKER + ".pending")
        pending.write_text(json.dumps(record))
        pending.replace(root / MARKER)
        child.stdin.write(b"G")
        child.stdin.close()
        deadline = time.monotonic() + timeout
        while True:
            exit_code = child.poll()
            if exit_code is not None:
                break
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
            stopped = stop_processes(root, child, child_identity)
            if read_owner(root) != record:
                raise RuntimeError(f"test ownership marker changed; retained {root}")
            remove_root(root)
            print("Owned test processes stopped; temporary root removed", flush=True)
            if stopped and exit_code == 0:
                exit_code = 1
                print("Tests left running processes; cleanup succeeded but the run fails", file=sys.stderr)
        except BaseException:
            if child is not None and child.poll() is None:
                child.kill()
                child.wait(timeout=5)
            print(f"Cleanup incomplete; retained owned test root: {root}", file=sys.stderr)
            raise
        finally:
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
        if os.name == "nt":
            # Windows exec does not preserve the registered process identity.
            # Keep this worker alive as the parent of the Go command instead.
            sys.exit(subprocess.call(sys.argv[2:]))
        os.execvpe(sys.argv[2], sys.argv[2:], os.environ)
    sys.exit(main())
