#!/usr/bin/env python3
"""Run the center's structural gate on CI commits or the local worktree."""
import os
from pathlib import Path
import subprocess


def main():
    root = Path(__file__).resolve().parent.parent
    env = os.environ.copy()
    env.setdefault("GOCACHE", str(root / ".cache" / "go-build"))
    env.setdefault("GOMODCACHE", str(root / ".cache" / "mod"))
    env.setdefault("GOPATH", str(root / ".cache" / "gopath"))
    base = env.get("QUALITY_BASE")
    if base and set(base) == {"0"}:
        base = None  # A newly pushed branch has no previous branch commit.
    if not base:
        # Include already committed branch work, not only edits since HEAD.
        refs = ["refs/remotes/origin/main", "refs/heads/main"]
        if not env.get("CI"):
            refs.append("HEAD")
        for ref in refs:
            try:
                base = subprocess.check_output(
                    ["git", "rev-parse", "--verify", ref + "^{commit}"],
                    cwd=root, text=True, timeout=10, stderr=subprocess.DEVNULL
                ).strip()
                break
            except subprocess.CalledProcessError:
                continue
        if not base:
            raise SystemExit("Quality gate requires an existing baseline commit")
    go = root / ".tools" / "go" / "bin" / ("go.exe" if os.name == "nt" else "go")
    command = [str(go) if go.is_file() else "go", "run", "./cmd/qualitycheck", "--base", base]
    if env.get("CI"):
        head = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=root, text=True, timeout=10
        ).strip()
        command += ["--candidate", head]
    else:
        command += ["--working-tree"]
    raise SystemExit(subprocess.run(command, cwd=root, env=env, timeout=240).returncode)


if __name__ == "__main__":
    main()
