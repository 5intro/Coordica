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
        "AGENTS.md", "AGENT.md", "QUALITY.md",
        "LICENSE", "THIRD_PARTY_NOTICES.md", "README.md", "go.mod", "go.sum",
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


def start_here(version, platform, binary):
    instructions = f"""Coordica v{version} — {platform}

这是 v{version} 开发预览版。
Owner 独立验证并发布后的版本页：https://github.com/5intro/Coordica/releases/tag/v{version}
既有 v0.2.0-dev macOS 本机 Phase B 端到端验收已通过；多机 Phase B 与 Linux 原生部署仍待验证。
实际可下载版本与对应验证记录见 Releases/Actions；Owner 独立验证后部署与发布。
检出或更新源码不会自动安装、发布或部署。
源码回归不代表本版本真实模型验收或目标系统原生部署认证。
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

0.2.3-dev 新增必需项目初始化：所有旧数据库与无 Goal 项目也需要 Leader 只读分析和 Owner 确认精确基线。
中心主机先用 host-config 保存可信固定角色/路径，再 setup --host-role center --host-config 固定配置 --runner-data 独立固定Runner目录；initialize_center/start_center/get_host_status 均无路径参数。
同一个中心 MCP 保留 initialize_runner，无第二个入口；中心 limited profile 保持原身份，不发送到提供的其它配对端点。
checks-file 可省略，先建立暂停的空检查草案并访谈；Owner 保存真实非空检查政策后重新分析确认，空检查不能执行。
初始化捕获 Owner 的 PlanningTimeoutSeconds 与 PlanningMaxRetries；默认300秒、两次重试，无需开启自动化。
原始回答、覆盖和源 SHA 保留；new 访谈可以在首次提交前开始。分析不执行检查、不生成开发任务、不启用自动化。
Owner token 不经 MCP 返回；受限 MCP 不隔离聊天的其它工具。
源码版本不意味着已经发布或原生部署通过。

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
统一接入：默认 setup 创建唯一的 coordica（mcp --interaction）；每台物理主机共享一个有限 interaction profile。
中心本机：{binary} chat-init --data /绝对路径/中心数据 --url http://127.0.0.1:8080 --profile /绝对路径/interaction.json --name "Shared center host"
节点 initialize_runner 自动复用/签发有限节点对话凭据；已保存注册的重试不会再次配对。
也可显式签发：{binary} chat-init --runner-data /绝对路径/已有Runner数据 --profile /绝对路径/interaction.json --name "Shared Runner host"
然后预览 {binary} setup --chat-profile /绝对路径/interaction.json，核对后加 --apply。
只迁移可确认归属且路径匹配的旧 coordica/coordica-chat；冲突保留，客户端选项与禁用状态保留。
旧显式 --mode bootstrap/chat 仅为兼容入口；默认统一接入，不安装多个角色。
中心停机或缺失 profile 时本机工具可用；在线工具每次校验凭据和监听范围。
能力变化在客户端活动时通知；不支持动态刷新时手动重新发现或重连。
dsh 只迁移安装器生成的文件；已加载旧 overlay 须自行从启动命令移除。
固定 Run MCP 仍仅有 get_context/report_block/request_scope，无主机管理或对话权限。
开发者邮箱、索引集成、模型/effort/并发设置与资源扩展均属后续功能。
既有 v0.2.2-dev 统一 MCP 实现及评审修复已独立通过完整 macOS Go race 套件，包括全部监听测试。
v0.2.2-dev 真实原生可执行文件 stdio 与隔离双监听中心验收通过：同一 MCP 进程从缺失 profile 获得十一项中心工具；
节点 profile 仅七项工具，不能访问 Leader/全局任务池；Run 仅三项范围内工具并拒绝跨角色调用。
中心实际重启时 MCP 进程保持存活；凭据撤销后缓存读取与需求提交被拒绝。
实际原生 Codex CLI 配置安装迁移为唯一 coordica，保留路径、启用/禁用状态、超时、无关设置、备份和幂等性。
评审修复覆盖 TOML 末尾分隔/无关表、项目级 MCP 名称状态、隐式目录环境/文件别名和本机 Runner 保留中心 profile。
开发侧离线 stdio 与五项 Python 打包测试通过；fixtures 不代表所有客户端或平台原生验收。
v0.2.2-dev 验收没有运行模型，没有派发开发、规划 Run 或任务；节点注册为逻辑 fixture，没有激活用户服务。
CLI 配置安装已测试；GUI 动态工具刷新取决于客户端支持。该 v0.2.2-dev 验收未新增 Windows/Linux 原生部署认证。
v0.2.3-dev 的原生服务、统一 MCP 与只读 Leader 就绪验证记录以对应 Releases 的发布说明为准，由 Owner 独立验证后记录。
可执行文件、数据和私有 profile 使用稳定绝对路径；setup 后重新加载或重启对应客户端。
中心凭据仅在中心回环地址允许 Leader 咨询；节点凭据仅允许安全公共事实、需求及本节点 Agent/Run。
节点不能查询全局任务、阻断、Leader 对话或其他节点执行上下文。凭据范围由中心持久化记录决定。
profile 不含 Owner/node token，不可放入源码或公开包；旧无范围凭据须显式 --renew。
Leader 纯咨询/建议不创建任务或修改授权；页面切换保留未确认请求，重试使用原内容与同一键。
项目 total_budget_seconds 与 ChatOverview budget_seconds 均为忽略的弃用字段，项目支出仅作统计。
开发/评审/规划/咨询单 Run 最多七天；任务/目标内规划累计最多 365 天且不少于单 Run，重试仍有限。
旧 budget 阻断不自动迁移；Owner 核对停止结算、剩余额度和当前授权后以当前任务版本恢复。
恢复不清零支出，不绕过重试上限，不改变已有发布意图；具体兼容步骤见 README.md。
需求版本化撤销/编辑与实时开发者邮箱尚未实现。
工具注册示例：examples/tools/custom-acp.json
独立验收小项目：examples/acceptance
Windows batch 路径限制：internal/process/README.md

包内不含个人凭据、中心地址或配对信息。解压不会自动安装或启动系统服务；
initialize_center 请求启动所选中心用户服务；initialize_runner 配对并请求启动 Runner 用户服务。
已完成 Mac 中心与 Windows 节点连接、Windows dsh 实现及 Claude 独立评审的小项目闭环。
运行中取消的 Phase A 验收已通过；该 Phase A 的实际 Agent 验收事实限于 Claude / dsh。
既有 v0.2.0-dev macOS 本机 Phase B 实测：原生 Codex gpt-6.1-sol Leader 规划 → 自动授权一个任务 →
原生 Codex scope_requested 且无编辑 → 实际进程停止 → Leader 在目标字面允许根目录内
自动批准普通范围 → 同一任务以新授权恢复并保留累计支出 → 原生 Codex 开发 →
独立本机 Claude 节点只读评审 → 中心对不可变固定候选执行 node:test 验证 →
精确固定候选条件更新本地测试 Git 的 main。Owner 仅批准目标一次，没有手动批准范围
或提交替代请求。结束时所有 Run 进程树均已停止，项目与目标预留预算均为零。
既有 v0.2.0-dev 原生 Leader 运行中取消验收也已通过：观察到结构化会话，取消前确认真实存活 PID，
取消后确认进程消失、静止与结算，保留实际耗时支出。
本机 Claude 与 dsh 原生协议冒烟测试已通过，不代表完整 Phase B dsh 协作或多机部署已通过。
完整测试与各平台 CI 结果以当前发布版本关联的 GitHub checks 为准，本地局部检查不替代完整测试或平台 CI。
普通 LAN 测试不覆盖断线、重启、重复消息、旧租约及进程树停止语义。
跨平台编译不等于目标系统原生认证；仅取得实际测量证据后标记通过。

Phase B：在中心本机 Owner 页面建立项目与需求，保存完整目标、非目标、验收、
关联需求、架构、目录大纲、字面允许根目录及有限累计预算，再由 Owner 一次授权。
“目标规划与 Leader”的评估通过持久化原生 Codex 只读执行完成；Leader 是配置的
CLI 执行，不是桌面聊天会话。所有受管 Codex 显式使用 gpt-6.1-sol，无模型回退。
目标准入后，中心本机“系统自动化”开关才允许边界内普通任务与范围自动授权；默认关闭。
新目标、架构、检查、受保护变化与外部权限仍需 Owner 批准，测试不替代授权。
原生严格执行意图协议 v1（native execution intent v1）最终输出仅为严格 JSON：
version=1、status、非 null 字符串数组 requested_paths、字符串 reason、非空 summary；
拒绝额外、重复或缺失字段及 JSON 外文字。completed 是意图，不是完成证据；
Runner 仍核对实际变化、范围、固定基线及进程树静止，独立评审、中心检查与固定候选
条件发布仍须通过。意图与请求回执均不授予权限。
范围不足时须在任何编辑前停止并保持固定工作区无变化，以 scope_requested 提出准确
新增路径和原因；Runner 确认实际进程树停止并复核无变化后持久化、提交原 Run 请求。
中心核对停止与结算，再交给只读 Leader 评估；只有自动化启用、目标批准与准入仍有效、
请求属于目标字面允许根目录且政策及剩余累计预算允许，普通范围才可自动批准。
同一任务以新授权和新执行代次恢复并保留累计支出，不创建替代任务或清零预算。
重大工作或其他阻断返回 blocked，保持工作区无变化；重大工作不会自动批准，须 Owner 决策。
scope_requested 与 blocked 是停止交接，不是成功完成。
项目累计支出只作统计，无生命周期额度；目标/任务/Run 与规划重试仍有限。
请求投递、规划重试及目标/任务累计预算有限；旧版本、无变化核对失败或未知停止状态不会放行，
须保留阻断并核对进程树停止证据。
节点能力按实际报告发现，由 Owner 管理实现/评审角色、排空、禁用及容量。
详细池、来源导航、决策、规划与任务范围历史，以及真实 API/CLI 路径见 README.md。
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
