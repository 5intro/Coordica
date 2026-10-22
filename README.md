<p align="center">
  <img src="web/public/favicon.svg" width="64" height="64" alt="Coordica" />
</p>

<h1 align="center">Coordica</h1>

<p align="center"><strong>在一个工作台里，协调多台电脑上的 AI 编程 Agent。</strong></p>

<p align="center">整理需求 · 分配工作 · 跟踪执行 · 审查交付</p>

<p align="center">
  <a href="#getting-started">开始使用</a> ·
  <a href="#compatibility">运行环境</a> ·
  <a href="#handbook">使用指南</a> ·
  <a href="https://github.com/5intro/Coordica/releases">下载</a> ·
  <a href="https://github.com/5intro/Coordica/issues">反馈</a>
</p>

[![checks](https://github.com/5intro/Coordica/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/5intro/Coordica/actions/workflows/ci.yml)

Coordica 把需求、开发任务、代码审查和阻断事项放在同一个工作台。你负责确定目标与授权，Agent 在各自电脑上完成工作；中心汇总进展、检查代码，并按照项目规则集成结果。

**开发预览版。** 本文介绍当前主干源码；下载包请使用对应版本的随包说明。[选择版本](#validation-reference) · [个人非商业源码可见许可](#许可与下载)

## 你可以用它做什么

- **集中管理开发工作**：从需求进入任务，到审查、集成和阻断处理，查看每项工作的状态与来源。
- **协调多台执行电脑**：通过 Runner 接入本机 Agent 工具，在中心查看节点活动、能力与正在执行的任务。
- **控制自动化范围**：为目标和任务设定允许修改的范围、预算与重试次数，关键决策由你确认。
- **检查每次代码交付**：对固定代码候选运行项目检查和独立 Agent 评审，保留结果与历史。
- **继续被中断的工作**：核对执行状态、处理阻断，在原有授权和预算内恢复任务。

### 从需求到交付

1. **添加项目与需求**：选择仓库，设置项目检查，整理需要完成的工作。
2. **确认基线与目标**：审阅项目分析，确定架构、验收标准、允许范围和预算。
3. **接入 Runner**：为每台执行电脑配对，选择可用工具与角色。
4. **启动任务**：查看规划与执行进展；需要人工决定的事项进入阻断处理。
5. **审查与集成**：检查固定候选及独立评审结果，按项目配置集成代码。

自动化默认关闭。启用后仍受已批准的目标、范围和预算约束；新目标、重大变化、受保护改动与外部权限需要单独授权。

## 如何组织你的电脑

| 角色 | 用途 |
| --- | --- |
| **中心（Center）** | 管理一个项目，保存任务与历史，协调检查、评审和代码集成 |
| **执行节点（Runner）** | 在每台电脑上调用本机 Agent 工具，领取工作并回传结果 |
| **Web 控制台** | 在中心电脑上管理项目、授权、节点与运行状态 |
| **统一 MCP** | 从聊天客户端查询项目、提交需求、完成接入或发起咨询 |

中心与 Runner 使用同一个可执行文件。可以先在一台电脑上配置两种角色，再接入其他电脑。Web 控制台嵌入程序，无需另部署 Web 服务、Redis 或消息队列。

<a id="getting-started"></a>

## 开始使用

### 1. 选择版本与安装位置

在 [Releases](https://github.com/5intro/Coordica/releases) 下载适合电脑的包，核对 `SHA256SUMS`，解压到固定目录，并阅读包内 `START-HERE.txt`。

下载包自带程序，不需要安装 Go 或 Node。准备好 Git、所需 Agent 官方 CLI 及其登录配置。已有安装请保留原数据目录、节点身份和配置；升级步骤见[运行维护](#operations-reference)。

想使用主干源码中的功能，可按[构建指南](#build-reference)自行构建。主干与发布包即使显示相同版本号，内容也可能不同。

### 2. 配置中心

选择一台电脑作为中心，指定项目仓库、数据目录和原生 Codex 路径，再注册统一 MCP。详细命令见[可信中心、项目初始化与 MCP 安装](#setup-reference)。

初始化并启动中心后，在这台电脑打开 [本机控制台](http://127.0.0.1:8080)，使用中心数据目录中的 `owner.token` 登录。该令牌仅供你管理中心，不要发送给 Agent 或写入仓库。

### 3. 接入执行电脑，开始项目

在中心控制台生成一次性接入指令，交给对应电脑上的 MCP 客户端完成 Runner 配对。每台电脑使用自己的配对码；同机也需要登记 Runner。

在控制台配置项目检查、确认共享基线和目标，然后启动任务或按需启用自动化。首次使用可从[独立示例项目](examples/acceptance/README.md)开始。

<a id="compatibility"></a>

## 运行环境

| 项目 | 配置 |
| --- | --- |
| 下载包 | Apple Silicon macOS、Windows x64、Linux x64 |
| 运行依赖 | Git、所需 Agent 官方 CLI 及其认证配置 |
| 规划与开发 | 原生 Codex；默认模型为 `gpt-6.1-sol` |
| MCP 客户端配置 | Codex、Claude Code、Cursor、Kimi Code、pi；dsh 使用生成的 overlay |
| 连接方式 | 同机回环连接；跨机使用 HTTPS、公共 CA 与一次性配对码 |
| 数据 | 中心使用 SQLite；运行数据与源码目录分开保存 |

Agent 管理页按工具实际报告的能力提供可选角色、模型与思考强度。安装 MCP 客户端与授予 Runner 执行角色是两项独立配置。

macOS 请使用固定安装路径，按需要授权文件访问；开发版没有稳定的 Developer ID 分发签名，更新后系统可能重新请求权限。Windows 建议使用较短的数据路径。中心管理控制台只在中心本机开放，远端 Runner 不取得管理权限。

<a id="validation-reference"></a>

## 版本与下载

- **发布包**：下载页提供版本说明、平台包和校验文件。使用该包内的 README 与 `START-HERE.txt`。
- **主干源码**：本文随主干更新，包含发布之后的改动。需求修订/撤回、模型与思考强度配置、GitHub PR 集成等主干功能，应使用包含对应改动的源码构建。
- **升级**：中心和 Runner 使用匹配版本。更新源码或合并代码不会替换正在运行的程序。

当前最新发布为 [v0.2.5-dev](https://github.com/5intro/Coordica/releases/tag/v0.2.5-dev)。主干与该发布的具体差异可查看[版本比较](https://github.com/5intro/Coordica/compare/v0.2.5-dev...main)。

<a id="handbook"></a>

## 使用指南

按需要展开配置与操作说明。

<a id="setup-reference"></a>

<details>
<summary><strong>可信中心、项目初始化与 MCP 安装</strong></summary>

### 保存中心配置

先选择固定安装路径、项目仓库与独立数据目录。下面的路径是占位示例，请替换为本机实际路径；带空格的路径需要加引号。

准备项目的真实检查命令，例如 Go 项目的 `checks.json`：

```json
[{"name":"Go tests","command":"go","args":["test","./..."],"timeout_seconds":300}]
```

下面以已有 HTTPS 仓库为例：先把你自己的仓库克隆到 `/projects/my-project`，确认本地和远端都有 `main` 分支及基础提交。将 `YOUR-ACCOUNT/my-project` 替换为实际目标，中心的读取与写入凭据须单独配置；仓库 URL 本身不授予权限。然后保存配置并注册 MCP：

```sh
/installed/coordica host-config --host-role center \
  --path /chosen/config/center.json --binary /installed/coordica \
  --codex-executable /official/codex --data /chosen/runtime/center \
  --chat-profile /chosen/profiles/interaction.json \
  --repository /projects/my-project \
  --repository-url https://github.com/YOUR-ACCOUNT/my-project.git \
  --branch main --name my-project \
  --checks-file /chosen/config/checks.json \
  --console-listen 127.0.0.1:8080 --runner-listen 127.0.0.1:8080
/installed/coordica setup --host-role center \
  --host-config /chosen/config/center.json \
  --runner-data /chosen/runtime/developer-runner \
  --client generic --output /chosen/mcp --apply
```

`--client generic --output` 导出通用客户端配置。也可先运行 `coordica setup` 预览已发现的客户端，再按实际客户端选择配置并使用 `--apply`。配置冲突需要先处理，不应覆盖其他项目或已有身份。

通过已注册的统一 MCP 调用 `initialize_center {}`，或使用 CLI：

```sh
coordica host --host-config /chosen/config/center.json --action initialize
coordica host --host-config /chosen/config/center.json --action status
```

初始化创建项目草案并请求用户级服务启动。以状态返回和本机控制台确认服务已就绪。启动后审阅项目分析、配置非空真实检查，并由 Owner 确认基线；Goal 和任务另行批准。

### 仓库与数据

`--repository` 指定本地源码输入，`--repository-url` 指定后续集成的 HTTPS 仓库或本机 bare Git 仓库绝对路径。省略 `--repository-url` 会把源码输入同时用作集成目标；普通非 bare clone 不能作为集成目标，因此上述示例显式指定远端地址。初始化不会自动 clone/fetch；新项目首次开发前需准备对应基础提交。

一个中心登记一个项目。新项目使用新的配置、data 和 profile；不要用新项目覆盖既有中心。macOS 建议将项目放在 `~/Projects` 等自行选择的目录，避免依赖聊天客户端的临时工作目录。

受限 MCP 不能确认基线或取得 Owner 权限，也不会隔离聊天客户端自身的其他终端、文件工具或本机权限。

</details>

<a id="network-reference"></a>

<details>
<summary><strong>Runner 本机页面与局域网接入</strong></summary>

Runner 配对后，在对应电脑打开 `http://127.0.0.1:8081`，查看本机工具、任务与运行状态，或提交需求。需求提交不会自动启动开发；节点令牌保留在 Runner 中，不发送给浏览器。

在中心控制台的“中心配置与接入”中，为每台电脑生成一次性接入指令。跨机连接使用中心实际 HTTPS 地址与公共 CA，核对证书 DER 的 SHA-256 指纹；不要分享中心私钥或 Owner 令牌。

手动配置局域网监听的示例：

```sh
./bin/coordica tls --out .coordica-tls --hosts localhost,127.0.0.1,192.168.1.10
./bin/coordica serve --data .coordica-center \
  --listen 192.168.1.10:8443 --console-listen 127.0.0.1:8080 \
  --tls-cert .coordica-tls/center.crt --tls-key .coordica-tls/center.key
```

将示例 IP 替换为中心真实地址。此命令用于手动前台运行，不要与已启动的用户级中心服务同时占用端口。

将公共 `center.crt` 复制到 Runner 电脑，再通过其 MCP 客户端提供中心 HTTPS 地址、本机 CA 路径和一次性配对码。以中心节点页的心跳确认接入。配对码到期后重新生成；复制失败可重试同一条接入配置。

远端地址只提供节点接口，中心控制台仍在中心本机打开。使用 `0.0.0.0` 监听时，还需指定 `--runner-url https://实际地址:端口`；该地址必须被证书 SAN 覆盖。不要关闭证书验证。

</details>

<a id="workflow-reference"></a>

<details>
<summary><strong>目标授权、需求生命周期与工作台</strong></summary>

- **需求池**：保存输入和来源；主干支持内容修订、撤回及历史查看。变更涉及在途工作时，先停止并核对相关执行，再重新规划。
- **任务池**：查看允许范围、预算、执行与重试；任务不会因为一条聊天消息就获得新权限。
- **审查池**：查看固定候选、检查、独立评审与集成结果。主线或检查政策变化后，需要重新核对候选。
- **阻断池**：处理范围、预算、停止确认和其他需要 Owner 决定的事项。
- **Agent 管理**：查看节点能力，设置允许角色、容量和工具执行选项；模型与思考强度按可用目录选择。
- **测试中心**：配置快速合并检查、项目回归和发布验证套件，查看绑定代码版本的报告。示例见[测试配置](examples/testing/README.md)。

在确认共享基线后，保存目标、架构、验收要求、允许范围和有限预算，再批准精确版本。自动化只在这些边界内运行；受保护路径的改动与重大变化仍由 Owner 决定。

</details>

<a id="github-reference"></a>

<details>
<summary><strong>GitHub PR 集成与发布权限</strong></summary>

本节适用于包含 GitHub PR 集成的主干源码构建。项目默认使用 Git 模式；切换到 GitHub PR 前，先暂停项目，确认执行停止且没有待处理候选或未知发布操作。

在“代码集成”中配置目标仓库与必需检查。中心为任务维护 PR，完成固定候选检查、独立评审及 GitHub 必需检查后再请求合并。额外的 GitHub 审批要求仍须满足。

中心使用独立私密凭据文件，不借用 Runner 的账号：

```sh
coordica serve --data /private/coordica-center \
  --github-repository owner/repository \
  --github-token-file /private/coordica-secrets/github.token
```

凭据文件只保存原始 API token，与源码、Owner token 和交互配置分开存放。Unix 禁止组和其他用户访问；Windows 使用当前用户的私密 ACL。轮换凭据后重启中心，不要把 token 放进 URL、命令参数或日志。

仓库配置需满足：

- 专用凭据具有该仓库 Contents、Pull requests、Commit statuses 写权限，以及 Checks 和分支保护读取权限。
- 分支保护要求指定检查通过、分支保持最新，并约束管理员；包含中心上报的 `coordica/review` 状态。
- 使用 merge commit 保留固定候选，不使用 squash、rebase 或 merge queue。
- 绑定 GitHub App 的检查使用可核验 App 身份的 Check Run；同名多个检查来源均需通过。

Coordica 不会修改仓库规则或跳过额外审批。暂停项目时只核对已有 PR 的事实，不创建或合并 PR。下载 Coordica 不授予本仓库的写权限；你的项目需要自己的仓库权限与凭据。

</details>

<a id="operations-reference"></a>

<details>
<summary><strong>暂停、恢复、备份与维护</strong></summary>

**暂停与恢复。** 暂停停止新派发，并请求在途执行结束。失联不等于已停止；在恢复前核对旧进程及远端 Git 状态。确认停止后，任务可在原有授权、剩余预算和重试次数内恢复；预算不足、授权失效或人工阻断需要 Owner 处理。

**备份。** 从 Web 或 CLI 创建一致备份，保存到安全位置。备份包含数据库快照、引用材料及必要 Git 对象，不包含凭据；尚未上传的节点私人修改需要另行保存。

恢复到一个不存在的新目录：

```sh
./bin/coordica restore --from 备份目录 --data 新中心目录
./bin/coordica serve --data 新中心目录
```

恢复后的项目保持暂停，旧节点撤销。重新配对、核对 Git 外部状态后再恢复工作。

**升级。** 先备份，确认旧执行停止，再更新中心与 Runner 并使用匹配版本。保留既有数据目录与身份，按控制台提示重新确认因政策变化失效的基线和授权。源码更新不会自动更新服务，也不会重新批准旧任务。

**权限。** 只授予实际需要的文件访问权限，不为减少弹窗授予全磁盘访问。Owner token、节点凭据、私钥与模型认证不应进入仓库、反馈附件或 Agent 提示词。

</details>

<a id="build-reference"></a>

<details>
<summary><strong>构建、测试与打包</strong></summary>

源码构建需要 Go 1.26+、Node 24、npm、Git；macOS/Linux 可运行：

```sh
git clone https://github.com/5intro/Coordica.git
cd Coordica
make build
./bin/coordica version
```

Windows PowerShell：

```powershell
git clone https://github.com/5intro/Coordica.git
cd Coordica
npm --prefix web ci
npm --prefix web run build
go build -trimpath -o bin/coordica.exe ./cmd/coordica
.\bin\coordica.exe version
```

当前源码版本号为 `0.2.5-dev`。Node 只用于构建前端，不是运行下载包的依赖。

仅查看控制台时，可用新的独立目录前台启动：

```sh
./bin/coordica serve --data .coordica-center
```

该命令不会配置 Leader、配对 Runner 或开始开发。使用完按 Ctrl+C 停止，再配置正式中心，避免端口冲突。

维护者可运行 `make test` 与 `make check`；测试还需要 Python 3.10+。协议生成使用 `make generate`。打包脚本 `scripts/package-release.py` 使用已有平台二进制生成分发包与校验文件，不负责构建或上传发布。

</details>

<a id="interface-reference"></a>

<details>
<summary><strong>API、CLI 与 MCP 参考</strong></summary>

- 中心生命周期使用 `coordica host`，管理 API 可通过 `coordica ctl` 调用；完整子命令见 [CLI 入口](cmd/coordica/main.go)。
- 公共协议结构见 [API schema](api/v1.schema.json)。
- 工具注册示例见 [custom-acp.json](examples/tools/custom-acp.json)。
- 测试套件与调用示例见 [测试配置](examples/testing/README.md)。

### 执行意图与权限边界

原生执行意图协议（native execution intent v1）使用严格 JSON，包含 `version=1`、`status`、`requested_paths` 字符串数组、`reason` 和 `summary`。`completed` 不是完成证据；仍需核对实际代码变化、范围、固定基线和进程树停止，再进行独立评审与检查。意图与请求回执均不授予权限。

需要新增修改范围时，Agent 必须在任何编辑前停止，保持固定工作区无变化，以 `scope_requested` 提交新增路径及原因。确认停止与工作区状态后，中心在当前目标、政策和预算内评估；同一任务保留累计支出，规划重试有上限，不通过重新请求清零预算。

重大工作返回 `blocked` 并保持工作区无变化；重大工作不会自动批准，由 Owner 决定。`scope_requested` 与 `blocked` 均为停止交接，不能用模型文字或测试结果代替授权。

Web、CLI 与 MCP 遵守相同授权规则。普通聊天使用受限 profile；Owner 管理与确认操作在本机控制台或已授权的 CLI/API 完成。

</details>

## 许可与下载

**源码可见，个人非商业使用免费。** 允许个人非商业修改并在自己的设备上使用；个人商业用途、团队或公司使用及二次开发、向他人发布修改版均须事先另获书面授权。这不是 MIT 或 OSI 开源许可。正式条款见 [LICENSE](LICENSE)，授权申请可向本仓库维护者提交 Issue。

第三方组件保留各自许可，见 [第三方声明](THIRD_PARTY_NOTICES.md)。本许可不会仅因使用 Coordica 就要求你公开独立开发的项目。

## 反馈

通过 [Issues](https://github.com/5intro/Coordica/issues) 提交问题或建议。请附版本、操作系统、复现步骤与脱敏日志，不要提交令牌、私钥、个人配置或项目私密内容。

