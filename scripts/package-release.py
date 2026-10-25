#!/usr/bin/env python3
"""Package existing release binaries; building or publishing is a separate step.

Uses only the Python standard library. Archive ordering, ownership, permissions,
and timestamps are fixed (or use SOURCE_DATE_EPOCH) for repeatable packaging.
"""

import argparse
import datetime
import fnmatch
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import stat
import sys
import tarfile
import tempfile
import zipfile

from startup_guide import start_here


ROOT = Path(__file__).resolve().parent.parent
PLATFORMS = (
    ("darwin-arm64", "coordica", "coordica", ".tar.gz"),
    ("windows-amd64", "coordica-windows-amd64.exe", "coordica.exe", ".zip"),
    ("linux-amd64", "coordica-linux-amd64", "coordica", ".tar.gz"),
)
# Distribution inputs are explicitly selected below. This second check prevents
# a future input expansion from silently including a local credential or state.
FORBIDDEN_NAMES = (
    ".env", ".env.*", "*.key", "*.pem", "*.p12", "*.pfx", "*.token",
    "*.db", "*.db-*", "*.sqlite", "*.sqlite-*", "*.sqlite3", "*.sqlite3-*",
    "id_rsa*", "id_ed25519*", "runner.json", "discovery.json", "owner.token",
    "credentials*", "secrets*", "*.init.pending", "*.init.lock", "*.write.lock",
    "coordica-chat*.json", "interaction*.json", ".chat-profile-pending-*",
)
FORBIDDEN_DIRS = {".git", ".cache", ".tools", ".ssh", ".aws", "node_modules"}


def checked_name(name):
    path = Path(name)
    if path.is_absolute() or ".." in path.parts or "\\" in name:
        raise ValueError(f"unsafe archive path: {name}")
    if path.parts and path.parts[0].lower() == "docs":
        raise ValueError(f"local-only documentation cannot be release input: {name}")
    if any(part.lower() in FORBIDDEN_DIRS for part in path.parts):
        raise ValueError(f"private directory in release input: {name}")
    if any(fnmatch.fnmatch(part.lower(), pattern)
           for part in path.parts for pattern in FORBIDDEN_NAMES):
        raise ValueError(f"possible credential or runtime state in release input: {name}")
    return name


def read_input(relative):
    """Reject links, including symlinked parents, and missing/empty inputs."""
    path = ROOT / checked_name(relative)
    for component in (path, *path.parents):
        if component == ROOT:
            break
        if component.is_symlink():
            raise ValueError(f"symlinks cannot be release inputs: {relative}")
    if not path.is_file():
        raise ValueError(f"required release input is missing: {relative}")
    data = path.read_bytes()
    if not data:
        raise ValueError(f"required release input is empty: {relative}")
    return data


def common_entries():
    required = [
        "LICENSE", "THIRD_PARTY_NOTICES.md", "README.md", "go.mod", "go.sum",
        "cmd/coordica/main.go", "api/v1.schema.json", "web/public/favicon.svg",
        "third_party/licenses/manifest.json",
        "internal/process/README.md", "examples/dsh/coordica.cordis.yml",
        "examples/tools/custom-acp.json", "examples/acceptance/README.md",
        "examples/acceptance/.gitignore", "examples/acceptance/src/greeting.mjs",
        "examples/acceptance/test/greeting.test.mjs", "examples/acceptance/tools/verify-acceptance.mjs",
        "examples/acceptance/task.json",
        "examples/native-scope/README.md", "examples/native-scope/goal.json",
        "examples/native-scope/task.json", "examples/native-scope/flow.json",
        "examples/native-scope/expected-scope-intent.json",
        "examples/native-scope/src/greeting.mjs",
        "examples/native-scope/test/greeting.test.mjs",
        "examples/native-scope/tools/verify-acceptance.mjs",
        "examples/testing/README.md",
    ]
    license_dir = ROOT / "third_party/licenses"
    if not license_dir.is_dir() or license_dir.is_symlink():
        raise ValueError("required third_party/licenses directory is missing or a symlink")
    license_files = []
    for path in sorted(license_dir.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"symlink in third-party licenses: {path.relative_to(ROOT)}")
        if path.is_file():
            license_files.append(path.relative_to(ROOT).as_posix())
    if not license_files:
        raise ValueError("third_party/licenses must contain the dependency license texts")
    required.extend(license_files)
    entries = {name: (read_input(name), 0o644) for name in sorted(set(required))}
    manifest = json.loads(entries["third_party/licenses/manifest.json"][0])
    records = manifest.get("files") if isinstance(manifest, dict) else None
    if not isinstance(records, list) or not records:
        raise ValueError("third-party license manifest must contain its audited files")
    audited_files = set()
    for record in records:
        if not isinstance(record, dict):
            raise ValueError("invalid third-party license manifest record")
        name, digest = record.get("file"), record.get("sha256")
        if not isinstance(name, str) or not name.startswith("third_party/licenses/"):
            raise ValueError("invalid third-party license path in manifest")
        if name not in entries or name in audited_files:
            raise ValueError(f"missing or duplicate audited license: {name}")
        if hashlib.sha256(entries[name][0]).hexdigest() != digest:
            raise ValueError(f"audited license checksum mismatch: {name}")
        audited_files.add(name)
    if audited_files != set(license_files) - {"third_party/licenses/manifest.json"}:
        raise ValueError("third-party license inventory differs from its audited manifest")
    for name, (data, _) in entries.items():
        if re.search(rb"-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----", data):
            raise ValueError(f"private key material detected in release input: {name}")
    return entries




def write_tar(path, entries, epoch):
    with path.open("wb") as output:
        with gzip.GzipFile(filename="", fileobj=output, mode="wb", mtime=epoch) as gz:
            with tarfile.open(fileobj=gz, mode="w", format=tarfile.PAX_FORMAT) as archive:
                for name, (data, mode) in sorted(entries.items()):
                    member = tarfile.TarInfo(checked_name(name))
                    member.size, member.mode, member.mtime = len(data), mode, epoch
                    member.uid = member.gid = 0
                    member.uname = member.gname = ""
                    archive.addfile(member, io.BytesIO(data))


def write_zip(path, entries, epoch):
    # ZIP timestamps cannot represent years before 1980.
    date = datetime.datetime.fromtimestamp(max(epoch, 315532800), datetime.timezone.utc)
    if date.year > 2107:
        raise ValueError("SOURCE_DATE_EPOCH exceeds the ZIP timestamp range")
    timestamp = (date.year, date.month, date.day, date.hour, date.minute, date.second)
    with zipfile.ZipFile(path, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for name, (data, mode) in sorted(entries.items()):
            member = zipfile.ZipInfo(checked_name(name), timestamp)
            member.create_system = 3
            member.external_attr = (stat.S_IFREG | mode) << 16
            member.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(member, data, compresslevel=9)


def verify_archive(path, entries):
    if path.suffix == ".zip":
        with zipfile.ZipFile(path) as archive:
            if archive.testzip() is not None or sorted(archive.namelist()) != sorted(entries):
                raise ValueError(f"archive validation failed: {path.name}")
            for name, (data, mode) in entries.items():
                if archive.read(name) != data or (archive.getinfo(name).external_attr >> 16) & 0o777 != mode:
                    raise ValueError(f"archive content or permission mismatch: {name}")
    else:
        with tarfile.open(path, "r:gz") as archive:
            if sorted(archive.getnames()) != sorted(entries):
                raise ValueError(f"archive member validation failed: {path.name}")
            for name, (data, mode) in entries.items():
                member = archive.getmember(name)
                if not member.isfile() or member.mode != mode or archive.extractfile(member).read() != data:
                    raise ValueError(f"archive content or permission mismatch: {name}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="0.2.5-dev", help="archive version (default: %(default)s)")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*)?", args.version):
        raise ValueError("version must be a numeric semantic version with an optional prerelease suffix")
    epoch = int(os.environ.get("SOURCE_DATE_EPOCH", "0"))
    if epoch < 0 or epoch > 0xFFFFFFFF:
        raise ValueError("SOURCE_DATE_EPOCH must fit the unsigned gzip timestamp range")
    entries = common_entries()
    # Read every binary before creating files, so missing inputs leave old outputs intact.
    binaries = {source: read_input(f"bin/{source}") for _, source, _, _ in PLATFORMS}
    output_dir = ROOT / "bin"
    with tempfile.TemporaryDirectory(prefix=".coordica-release-", dir=output_dir) as temporary:
        staging = Path(temporary)
        names = []
        for platform, source, binary, extension in PLATFORMS:
            package_entries = dict(entries)
            package_entries[binary] = (binaries[source], 0o755)
            package_entries["START-HERE.txt"] = (start_here(args.version, platform, binary), 0o644)
            if platform == "windows-amd64":
                package_entries["coordica.cordis.yml"] = entries["examples/dsh/coordica.cordis.yml"]
            name = f"coordica-{args.version}-{platform}{extension}"
            target = staging / name
            if extension == ".zip":
                write_zip(target, package_entries, epoch)
            else:
                write_tar(target, package_entries, epoch)
            verify_archive(target, package_entries)
            names.append(name)
        sums = "".join(f"{hashlib.sha256((staging / name).read_bytes()).hexdigest()}  {name}\n"
                       for name in sorted(names))
        (staging / "SHA256SUMS").write_text(sums, encoding="ascii")
        for name in [*names, "SHA256SUMS"]:
            os.replace(staging / name, output_dir / name)
    print(f"Validated {len(names)} packages in {output_dir}; SHA256SUMS updated.")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, tarfile.TarError, zipfile.BadZipFile) as error:
        print(f"Packaging failed: {error}", file=sys.stderr)
        sys.exit(1)
