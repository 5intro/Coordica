#!/usr/bin/env python3
"""Run the center's structural gate on CI commits or the local worktree."""
import os
from pathlib import Path
import subprocess


def resolve_base(root, env):
    base = env.get("QUALITY_BASE")
    if base and set(base) != {"0"}:
        return base

    def read(*args):
        return subprocess.check_output(
            ["git", *args], cwd=root, text=True, timeout=10,
            stderr=subprocess.DEVNULL
        ).strip()

    try:
        return read("rev-parse", "--verify", "refs/remotes/origin/main^{commit}")
    except subprocess.CalledProcessError:
        pass
    if not env.get("CI"):
        try:
            current = read("symbolic-ref", "--quiet", "HEAD")
            if current != "refs/heads/main":
                return read("rev-parse", "--verify", "refs/heads/main^{commit}")
        except subprocess.CalledProcessError:
            pass
    raise SystemExit("Quality gate requires target main or an explicit QUALITY_BASE; refusing to self-baseline HEAD")


def main():
    root = Path(__file__).resolve().parent.parent
    env = os.environ.copy()
    env.setdefault("GOCACHE", str(root / ".cache" / "go-build"))
    env.setdefault("GOMODCACHE", str(root / ".cache" / "mod"))
    env.setdefault("GOPATH", str(root / ".cache" / "gopath"))
    base = resolve_base(root, env)
    go = root / ".tools" / "go" / "bin" / ("go.exe" if os.name == "nt" else "go")
    command = [str(go) if go.is_file() else "go", "run", "./cmd/qualitycheck", "--base", base]
    if env.get("CI"):
        head = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=root, text=True, timeout=10
        ).strip()
        if base == head:
            raise SystemExit("CI requires a different trusted baseline; refusing to compare HEAD with itself")
        command += ["--candidate", head]
    else:
        command += ["--working-tree"]
    raise SystemExit(subprocess.run(command, cwd=root, env=env, timeout=240).returncode)


if __name__ == "__main__":
    main()
