# 通过 MCP 初始化执行电脑

Windows、macOS 和 Linux 使用同一流程：安装本机 Coordica MCP，在已安装并登录的 Agent 中发出初始化指令。MCP 会探测环境、与中心配对并启动独立的用户级 Runner；无需再手动导出 profile、执行 pair 或常驻打开浏览器。

## 安装一次本机 MCP

准备当前操作系统的 `coordica` 二进制，放在不会被临时清理的固定位置。执行电脑需要 Git；本机已有的注册 Agent CLI 继续使用自己的安装和登录状态。Coordica 不收集模型账号密码，也不代为安装这些工具。

先运行 `coordica setup` 预览，再运行 `coordica setup --apply`，自动为发现的客户端配置 MCP。Windows 解压到 `C:\Coordica` 时，对应命令为 `& C:\Coordica\coordica.exe setup --apply`。同一入口处理 Claude Code、Codex、Cursor、Kimi Code、pi 的配置差异，并为 dsh 生成本机 overlay；详细版本条件和配置冲突处理见 [统一客户端接入](mcp-clients.md)。

如果客户端没有内置注册适配，可通过 `setup --client generic --apply` 导出通用配置，或在其 MCP 设置中注册以下 stdio 服务：

| 字段 | 值 |
| --- | --- |
| 名称 | `coordica` |
| 可执行文件 | Coordica 二进制的绝对路径；Windows 为 `coordica.exe` |
| 参数 | `mcp`, `--bootstrap` |

Runner 数据目录默认位于当前用户的系统配置目录下 `Coordica/runner`。需要指定位置时，在 MCP 启动参数中增加 `--runner-data` 和绝对路径。这个目录由安装配置固定，初始化工具不会接受模型指定的写入目录。

不要使用 `go run` 或临时下载目录作为 MCP 命令。后台 Runner 会使用同一个固定二进制，MCP 退出后仍需能够找到它。

## Windows 上的 dsh 配置示例

从 [GitHub Releases](https://github.com/5intro/Coordica/releases) 下载 `coordica-0.1.1-dev-windows-amd64.zip`，核对同一版本的 `SHA256SUMS` 后，解压到固定目录 `C:\Coordica`。包中已有 `coordica.cordis.yml`；确认 `coordica.exe` 与它都直接位于该目录。使用其他目录时，修改 YAML 中 `command` 的绝对路径。源码构建的同名本地测试包也可使用。

下载和使用受包内 [LICENSE](../LICENSE) 约束：仅个人非商业使用和本地修改免费；商业用途、团队或公司使用以及发布修改版须另获授权。模板只包含本机 MCP 启动配置，不包含中心地址、配对码、节点令牌或模型凭据。

在 PowerShell 中检查程序和已有工具，再通过 dsh Web 加载配置：

```powershell
& C:\Coordica\coordica.exe version
git --version
dsh --version
dsh web --patch C:\Coordica\coordica.cordis.yml
```

首先给 dsh 下达：“调用 `mcp__coordica__inspect_environment`，报告本机系统、Git 和 Agent 工具的路径、版本与可用性，先不要配对或启动 Runner。”这一步不需要中心服务。真正初始化前，再准备中心的实际 HTTPS 地址、一次性配对码和公共 CA。下面的 IP 仅为示例，不能直接当作已部署地址使用。

以上命令选择 dsh Web profile；其他 profile 的安装与启动方式应以当前 dsh 安装版本为准。以后每次使用同一个 `--patch` 即可加载此 MCP；若要永久加入已有用户配置，只合并这条 `insert`，不要覆盖原配置。

dsh 官方 MCP 客户端通过 Cordis 配置连接 stdio 服务，工具名称为 `mcp__<serverName>__<tool>`。以下为包内 overlay；Runner 使用当前用户的默认配置目录，不必替换用户名。[dsh MCP 客户端配置](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/mcp/mcp-client/README.md)

```yaml
- insert:
    - id: coordica-bootstrap
      name: '@deepseek-ai/dsh-mcp-client'
      config:
        serverName: coordica
        transport: stdio
        command: 'C:\Coordica\coordica.exe'
        args:
          - mcp
          - --bootstrap
        env: {}
        toolCallTimeoutMs: 180000
```

源码中的配置模板位于 [examples/dsh/coordica.cordis.yml](../examples/dsh/coordica.cordis.yml)。手动创建时保存为 `coordica.cordis.yml`，用所用 profile 加载这个 overlay。例如 Web profile：

```powershell
dsh web --patch .\coordica.cordis.yml
```

官方示例支持通过 `--patch` 加载独立 overlay。等待 `mcp__coordica__initialize_runner` 出现在工具列表后再下指令；不同 dsh 发行版本的 profile 和插件安装情况应以本机版本为准。[官方 overlay 示例](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/mcp-memory.md)

macOS/Linux 推荐使用 `coordica setup --apply` 自动生成本机路径，免去修改 Windows 模板。上面的固定路径模板仍可用于快速试用。

## 下达一句初始化指令

中心的 owner 先生成一次性配对码。局域网使用 HTTPS；如果中心使用自签证书，先把中心提供的公共 CA 文件放到执行电脑，或使用已被系统信任的 CA。证书必须覆盖实际中心地址。

在已安装 MCP 的 Agent 中发送：

> 请调用 Coordica 的 initialize_runner，把这台电脑初始化为执行节点。中心地址是 https://192.168.1.10:8443，配对码是我刚提供的那一个，公共 CA 文件是 C:\Coordica\center.crt，节点名是 Windows-dev，并发容量为 2。报告注册和后台 Runner 的实际状态。

对应工具参数：

```json
{
  "center_url": "https://192.168.1.10:8443",
  "pairing_code": "本次生成的一次性配对码",
  "tls_ca": "C:\\Coordica\\center.crt",
  "name": "Windows-dev",
  "capacity": 2
}
```

`name`、`capacity`、`tls_ca` 可省略：名称默认本机主机名，并发默认 2，证书默认使用系统信任。初始化自动探测环境，不必先调用诊断工具。配对码只用于本次注册，不写入公开 profile，不进入后台服务启动参数；返回结果不含节点令牌。

成功注册后，中心的运行与节点页会显示真实节点与探测版本。`service.state=activation_requested` 表示操作系统已接受启动请求，`started` 表示服务管理器报告活动；与中心的实际连接仍需查看后续心跳。关闭 Agent 的 MCP 会话不会关闭独立 Runner。收到注册成功但服务启动失败时，按返回的步骤和错误修复，再用同一固定 Runner 数据目录重试；不能把注册成功当作常驻运行成功。

重新初始化同一中心会保留节点身份和未确认交接记录。Runner 未运行时，重试可以刷新本机探测到的工具路径、PATH 和提供的公共 CA；数据目录锁被占用时，返回 `registered=true`、`service.state=directory_in_use` 及部分失败说明，不改配置，也不调用服务管理器打断当前工作。需要更新运行中节点的配置时，先停止节点的当前工作、核对 Run 已静止，再通过该系统的用户服务管理器停止 Runner，随后重新初始化。

## 诊断与实际验收

`inspect_environment` 只读取 OS、架构、主机名，并探测 Git，并对本机注册表中的工具执行固定版本命令（默认 `--version`）。认证状态始终为 `unknown`；找到版本不代表已登录，也不代表该系统上的取消、恢复和 ACP 执行已经验收。Git 不可用会阻止初始化。未发现的 Agent 明确显示不可用，不伪造能力。

Windows 的 npm 工具入口可能是 `.cmd` / `.bat`。Coordica 支持带空格和中文的受限本机路径，但会拒绝 shell 元字符、括号等不安全路径；例如 `Program Files (x86)` 中的 batch 入口需要改用满足约束的安装位置。失败会返回明确的 preflight 原因，不能据此判断账号登录失效。具体约束见 [进程与 Windows batch 入口](../internal/process/README.md)。

Windows 上请保持中心和 Runner 的数据目录较短，例如 `C:\Coordica\runner-data`。受控 Git 已开启长文件路径支持，但 Git 仓库目录与 Windows 子进程工作目录各有独立限制。Coordica 在创建仓库或启动 Git 前展开现有路径的短文件名和符号链接，要求实际仓库根路径不超过 215 个 UTF-8 字节，并同时满足 258 个 UTF-16 单位的进程工作目录上限；超限会明确提示缩短数据目录。215 字节为 Git 的独立目录上限预留了 `/.git` 空间，中文和其他非 ASCII 字符可能占多个字节。这不限制仓库内文件或 Git 引用的总路径为 215 字节；超过 260 字符的对象与引用路径另有真实回归测试。需要自定义 Runner 数据目录时，在 MCP 配置的 `args` 中追加 `--runner-data` 和固定绝对路径。[Microsoft 对工作目录与进程启动限制的说明](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-setcurrentdirectory)

`save_runner_profile` 仅是诊断辅助：同一 MCP 会话先成功探测后，默认保存到固定 Runner 数据目录下的 `discovery.json`；可在 MCP 启动参数中用 `--profile` 指定其他固定位置。配置无 token，不会替换已配对的 `runner.json`，也不是主流程的必需步骤。

接入完成后，再由中心建立有限预算的真实任务进行验收。Windows 上已安装并登录 dsh 是前置条件；仍需实际验证受管执行、提交、独立评审、停止与失联恢复。当前开发电脑上的测试与 Windows 交叉编译不能替代第二台 Windows 电脑的这些结果。
