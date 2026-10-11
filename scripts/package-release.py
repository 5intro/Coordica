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
    "credentials*", "secrets*",
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
        "third_party/licenses/manifest.json",
        "internal/process/README.md", "examples/dsh/coordica.cordis.yml",
        "examples/tools/custom-acp.json", "examples/acceptance/README.md",
        "examples/acceptance/.gitignore", "examples/acceptance/src/greeting.mjs",
        "examples/acceptance/test/greeting.test.mjs", "examples/acceptance/tools/verify-acceptance.mjs",
        "examples/acceptance/task.json",
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


def start_here(version, platform, binary):
    instructions = f"""Coordica {version} — {platform}

这是开发预发布包，尚未完成阶段 A 全部真实部署与双机验收。
中心、MCP 与 Runner 使用同一个可执行文件，无需单独安装 Runner 或构建源码。

许可：仅个人非商业使用及私人本地修改免费。商业用途、团队或公司使用、
以及发布修改版须另获书面授权。完整条款以 LICENSE 为准。
这是附有限制的源码可见许可，不是 OSI 批准的开源许可。
第三方组件保留各自许可，详见 THIRD_PARTY_NOTICES.md 和 third_party/licenses/。

执行电脑：
1. 将 {binary} 放入固定目录，不要从临时目录或 go run 启动。
   安装路径可自定义；macOS 首次安装建议固定于 ~/Library/Application Support/Coordica/bin，数据与项目选用 Desktop/Documents/Downloads 之外的目录。
2. 本机需要 Git；要使用的 Agent CLI 应已安装并登录，沿用其自身配置。
3. 运行 {binary} setup 预览，再运行 {binary} setup --apply，统一为发现的客户端
   注册 MCP。Windows 使用 .\\coordica.exe，Unix 使用 ./coordica 或固定绝对路径。
   可用 --client 选择一个客户端；dsh 会生成独立 overlay 并给出加载方式。
4. 先调用 mcp__coordica__inspect_environment 检查本机环境，不需要中心服务。
5. 使用中心“中心配置与接入”为这台电脑生成的一次性接入指令，
   让 Agent 保存公共 CA、注册 MCP，再调用 initialize_runner 配对并启动用户级 Runner。
   也可自行提供真实中心 HTTPS 地址、配对码和必要的公共 CA 绝对路径。
6. 在中心“运行与节点”核对后续心跳。关闭 MCP 会话不影响独立 Runner。

安装与 MCP 初始化：README.md 的“统一 MCP 初始化（主要接入方式）”
中心首次启动及局域网证书：README.md 的“本机启动”和“两台电脑通过局域网连接”
中心统一管理入口只在中心电脑打开，默认 http://127.0.0.1:8080；
需求池、任务池、运行与节点及政策均在这里。登录后打开 /#connection，
可在建项目前查看实际部署与公共 CA，为每个远端节点分别生成一次性接入配置。
重试复制使用“复制已有接入配置”，保留原配对码。指令不含 owner/node 令牌或中心私有数据路径。
执行电脑使用中心另行提供的局域网 HTTPS 节点地址和公共 CA。
通配监听地址不能分享；中心使用 --runner-url 指定 HTTPS 实际地址，且须由 TLS 证书 SAN 覆盖。
支持本机页面的 Runner 启动后，可在该电脑打开 http://127.0.0.1:8081，
查看自己的运行并向中心提交需求提案；提案不会自行创建或批准任务。
仅当中心也在本机且连接信息匹配时显示中心各池与配置入口，中心仍须 owner 登录。
工具注册示例：examples/tools/custom-acp.json
独立验收小项目：examples/acceptance
Windows batch 路径限制：internal/process/README.md

包内不含个人凭据、中心地址或配对信息。解压不会自动安装或启动系统服务；
只有调用 initialize_runner 时才配对及请求启动用户服务。
已完成 Mac 中心与 Windows 节点连接、Windows dsh 实现及 Claude 独立评审的小项目闭环。
本机页面与接入配置的 Windows 原生部署、取消与断线等故障场景仍待实测；Linux 原生部署待实测。
"""
    if platform == "windows-amd64":
        instructions += """
Windows dsh Web 快速接入：
解压后确认 C:\\Coordica\\coordica.exe 和 C:\\Coordica\\coordica.cordis.yml 存在。
PowerShell：dsh web --patch C:\\Coordica\\coordica.cordis.yml
安装在其他目录时，先修改 YAML 的 command 绝对路径。
这份配置是可选 overlay，不会覆盖已有 dsh 配置。
"""
    else:
        instructions += """
macOS/Linux 的 dsh 接入：
运行 ./coordica setup --client dsh --apply，使用结果给出的本机 overlay 路径：
dsh web --patch /结果中的路径/coordica.cordis.yml
examples/dsh/coordica.cordis.yml 保留作 Windows 路径示例。
"""
    return instructions.encode("utf-8")


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
    parser.add_argument("--version", default="0.1.4-dev", help="archive version (default: %(default)s)")
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
