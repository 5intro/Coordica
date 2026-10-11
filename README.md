# Coordica

多电脑、多 Agent 开发协调底座。采用 Go 中心与 Runner、SQLite、React/TypeScript 控制台；Web 产物嵌入中心二进制，无 Redis、消息队列或额外后台运行时。

当前是 **[v0.2.0-dev 开发预览版](https://github.com/5intro/Coordica/releases/tag/v0.2.0-dev)**。已实现固定 Git 候选、独立评审、程序检查和条件发布闭环；构建与测试结果见 [GitHub Actions](https://github.com/5intro/Coordica/actions)。协议模拟测试、跨平台编译和托管 CI 不代表用户电脑上的真实部署验收。

## 许可与下载

**源码可见，个人非商业使用免费。** 允许个人非商业修改并在自己的设备上使用；个人商业用途、团队或公司使用及二次开发、向他人发布修改版均须事先另获书面授权。这不是 MIT 或 OSI 开源许可。正式条款见 [LICENSE](LICENSE)，授权申请可向本仓库维护者提交 Issue。

第三方组件保留各自许可，见 [第三方声明](THIRD_PARTY_NOTICES.md)。本许可不会仅因使用 Coordica 就要求你公开独立开发的项目。

预编译开发包通过 [v0.2.0-dev Release](https://github.com/5intro/Coordica/releases/tag/v0.2.0-dev) 提供。通过 `coordica setup --apply` 统一配置本机 Agent 的 MCP 接入，不必逐家手写配置；见下方初始化步骤。Windows amd64 包内还附 `coordica.exe` 和 dsh 配置模板 `coordica.cordis.yml`。发布包附 `SHA256SUMS`，当前仍为开发预览版；实际验收范围见下方“验证状态”。

## 构建

需要 Go 1.26+（本次使用 1.27.2）、Node 24、npm、Git。Node 只用于构建 Web；部署只需要对应系统的二进制、Git 和各 Agent 官方 CLI。

```sh
make build
make test
make check
```

Windows 可在 PowerShell 分别运行：

```powershell
npm --prefix web ci
npm --prefix web run build
go build -trimpath -o bin/coordica.exe ./cmd/coordica
go test -race ./...
```

`make generate` 从 Go 协议记录生成 `api/v1.schema.json` 与 Web 类型；不手改生成文件。网络输入仍由中心做严格 JSON 解码、领域校验和授权检查。

维护者准备好 `bin/coordica`（macOS arm64）、`bin/coordica-windows-amd64.exe` 和 `bin/coordica-linux-amd64` 后，可运行 `make package`，用 Python 3 标准库生成三个分发包和 `SHA256SUMS`。脚本校验许可、包内文件及二进制字节；它不构建程序、不上传发布，普通下载用户无需安装 Python。

## 运行目录与 macOS 权限

首次安装时自行选择固定可执行路径，macOS 建议使用 `~/Library/Application Support/Coordica/bin/coordica`，再从该位置执行 `setup`。`setup --runner-data 绝对路径` 可自定义 Runner 数据目录；中心和 Runner 均支持 `--data 绝对路径`。MCP 用户配置默认保存在用户配置目录，项目仓库建议放在 `~/Projects` 等 Desktop、Documents、Downloads 之外的目录。macOS 对 Desktop、Documents 和 Downloads 另有文件访问授权，仅改变启动工作目录不会授予权限。[Apple 文件访问说明](https://support.apple.com/en-ca/guide/security/secddd1d86a6/web)

当前开发版没有稳定的 Developer ID 分发签名。构建产物变化会改变 ad hoc 签名的 `cdhash`，系统可能重新请求权限，不能保证一次授权跨版本有效；请按实际需要授权，不要为减少弹窗而授予全磁盘访问。[Apple 分发签名说明](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac/)、[DR 身份说明](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements)

已有节点升级保持原数据目录和节点身份，不直接搬动已有数据；材料和仓库记录可能使用绝对路径。Phase B 使用 v0.2.0-dev 中心与 Runner；升级并重启由用户实施，代码合并不会更新运行中的服务。

## 本机启动

```sh
./bin/coordica serve --data .coordica-center
```

在运行中心服务的电脑上打开 `http://127.0.0.1:8080`，将 `.coordica-center/owner.token` 中的令牌输入登录页。控制台及 owner API 只监听本机回环地址；令牌不打印到服务日志，请不要把 owner 令牌交给 Agent。

8080 是统一管理入口，包含需求池、任务池、运行与节点、政策与恢复，以及“中心配置与接入”。登录后可直接访问 `http://127.0.0.1:8080/#connection`，即使尚未建立项目也能查看真实监听地址、版本、数据目录、公共 CA 和证书 DER 的 SHA-256 指纹。

为每个远端节点分别生成一次性接入指令，再发给那台电脑的 Agent；指令包含公共 CA 和配对码，不包含 owner/node 令牌、私钥或中心私有数据路径。复制失败时使用“复制已有接入配置”或手动复制文本，保留原配对码；到期后再重新生成。

Web 中建立项目、明确有限累计总预算和最低检查，录入需求，再按下方 Phase B 流程保存、批准和评估目标。仓库地址支持不含凭据的 HTTPS URL，或中心本机 Git 仓库绝对路径。远端需要已有目标分支与至少一个提交。阶段 A 针对 Coordica 自举，中心强制保护授权、验证、集成、适配器与初始化服务模块；用户还可追加保护路径。路径模式按仓库根目录、大小写敏感的 `/` 路径匹配，支持 `*` 和完整路径段 `**`；符号链接、submodule 等当前不支持的变化会拒绝集成。

## Phase B：目标授权、Leader 与有限自动执行

在中心本机 Owner 控制台的“项目概览”建立项目，填写仓库、目标分支、有限累计项目预算、受保护路径和最低检查；在“需求池”录入需求。随后进入“目标规划与 Leader”，创建完整目标文档：objective（目标）、non_goals（非目标）、acceptance（验收标准）、关联 requirement_ids、architecture（架构）、directory_outline（目录大纲）、字面 allowed_roots（允许根目录）和正整数 budget_seconds（有限累计目标预算）。空架构或缺失边界仍属于规划，不表示可执行。

保存后由 Owner 点击“一次授权”，批准当前文档与授权版本。再向 Leader 输入说明并点击“评估规划”；“发送消息”与“评估规划”都提交版本化规划执行请求，最终以实际运行及中心准入记录为准。Leader 是中心配置的原生 Codex CLI 执行，固定显式使用 `gpt-6.1-sol`、只读规划材料；它与桌面聊天会话不同。规划请求、执行代次、租约、截止时间、进程树停止证据、结算、结构化结果和任务映射均持久化。模型文字中的 ready 或“完成”不能替代中心准入、授权或实际进程停止。所有受管 Codex 执行均显式指定 `gpt-6.1-sol`，不回退到其他模型。

目标获批准并经实际 Leader 评估准入后，启用系统自动化才允许在已批准架构、允许根目录、政策及剩余累计预算内自动批准普通任务与普通范围。中心本机“系统自动化”页面默认关闭开关，可设置规划超时、有限规划累计预算及最大重试次数（0–2）。关闭开关阻止新自动授权，不取消已经授权的任务；目标一次授权独立于开关。新目标、架构变化、检查变化、受保护变化和外部权限仍需要 Owner 明确批准；受保护变化针对固定候选 C 特别批准。测试通过不能替代这些授权，决策处理也不能批准目标或绕过预算。保存目标修改会使旧目标批准与准入失效。

原生严格执行意图协议 v1（native execution intent v1）要求最终输出仅为严格 JSON 对象，字段为 `version=1`、`status`、非 null 的字符串数组 `requested_paths`、字符串 `reason` 与非空 `summary`；不接受额外字段、重复字段、缺失字段或 JSON 外的文字。`completed` 只表达执行意图，不是完成证据；Runner 仍须核对实际变化、范围、固定基线和进程树静止，随后由独立评审、中心检查与固定候选条件发布决定结果。意图与请求回执均不授予权限。

任务范围不足时，执行 Agent 必须在任何编辑前停止，保持固定工作区无变化，返回 `scope_requested`、准确的新增仓库路径及原因。Runner 确认实际进程树停止并复核无变化后，持久化并提交绑定原 Run、执行代次和任务的请求；中心核对停止与结算，再交给只读 Leader 评估。仅在自动化启用、目标批准与准入仍有效、请求属于目标字面允许根目录、政策与剩余累计预算允许时，普通范围才可自动批准。同一任务随后以新授权和新执行代次恢复，保留累计支出，不创建替代任务或清零预算。请求投递及规划重试均有限；旧版本、无变化核对失败或未知停止状态不会放行。

重大工作或其他阻断返回 `blocked`，保持工作区无变化并说明原因；重大工作不会自动批准，仍需 Owner 决策。`scope_requested` 与 `blocked` 是停止交接，不是成功完成，不能通过模型文字或测试通过绕过边界。

这些 UI 使用同一套本机 Owner API，前缀为 `/api/v1`；CLI `ctl --path` 省略该前缀。真实操作路径如下：

| UI 操作 | HTTP / CLI 路径 | 请求内容 |
| --- | --- | --- |
| 建立项目 | POST `project` | name、repository_url、target_branch、total_budget_seconds、protected_paths、checks |
| 录入需求 | POST `requirements` | title、body |
| 保存目标 | POST `planning/save` | expected_version、document |
| Owner 一次授权 | POST `planning/authorize` | expected_version |
| Leader 消息 / 评估 | POST `planning/messages` / `planning/evaluate` | expected_version、text |
| 取消规划 | POST `planning/cancel` | expected_version |
| 系统自动化 | POST `system/automation` | expected_version、settings |
| 节点调度配置 | POST `nodes/节点ID/configure` | expected_version、name、capacity、draining、disabled_new_work、tools |
| 处理决策 | POST `decisions/决策ID/resolve` | expected_version、expected_source_version、action、evidence |

例如用 `coordica ctl --method POST --path planning/evaluate --body-file evaluation.json` 提交当前版本与说明；用 `coordica ctl --path state` 读取真实版本和结果。JSON 字段以 [公开 API Schema](api/v1.schema.json) 为准，不猜测版本。所有写操作携带幂等键，旧版本请求会冲突。

规划失败只允许有限重试，持续累计已用与预留预算；不能通过重新请求清零。未知进程停止状态保留阻断和证据，确认进程树停止并结算后才允许后续执行。预算耗尽、重试用尽、重大决策或越界范围进入 Owner 决策流程，不由普通测试或模型回复自动放行。

## 池、来源与节点能力

中心 Owner 页面是私有本机控制台。需求池保存输入，任务池显示授权、范围、预算及状态；运行池记录执行与停止事实，候选池显示固定代码、独立评审、检查和受保护批准，阻断池汇总待处理来源与决策。来源按钮返回对应任务、运行、候选或规划记录；规划文档与决策本身不是缺失的任务。决策页保留来源版本、证据和处理历史，规划页保留 Leader 对话、实际规划运行、结构化任务/范围/重大决策提案；任务详情保留范围请求、授权版本及相关历史，审计页保留事件。请核对这些记录，不从汇总文案推断授权。

“Agent 池”和“运行与节点”按实际节点报告能力展示工具，不保证固定 Agent 名单。Owner 通过节点配置控制工具 enabled、implement（实现）、review（评审），节点容量为 1 或 2，并可排空、禁用新工作或撤销节点。能力发现与调度权限分开；排空、禁用或撤销不表示在途进程已经停止。工具支持以节点实际报告、可信工具注册表和协议能力为准，安装或版本探测不代表已认证、可承担某角色或已验收。其他 Agent 可按报告能力承担已允许的角色；Leader 与自动开发政策的原生 Codex 模型固定如上。

远端执行电脑的 `http://127.0.0.1:8081` 只展示该 Runner 身份可见的本机能力、任务、运行与需求提案，不提供 Owner 目标批准、扩围、预算或发布权限。远端浏览器不能通过局域网节点接口访问中心 Owner UI；同机导航也仍要求 Owner 独立登录。

## 验证状态

既有 Phase A 已完成真实 Mac 中心与 Windows 节点连接、Windows dsh 实现、Claude 独立评审的小项目闭环，以及运行中取消验收。该 Phase A 的实际 Agent 验收事实限于 Claude / dsh，不把支持定义或版本探测说成其他工具已实测。

**macOS 本机 Phase B 端到端验收已通过。** 实测流程为原生 Codex CLI `gpt-6.1-sol` Leader 规划 → 自动授权一个任务 → 原生 Codex 返回 `scope_requested` 且无编辑 → 实际进程停止 → Leader 在目标字面允许根目录内自动批准普通范围 → 同一任务以新授权恢复并保留累计支出 → 原生 Codex 开发 → 独立本机 Claude 节点只读评审 → 中心对不可变固定候选执行 `node:test` 验证 → 精确固定候选条件更新本地测试 Git 的 `main`。Owner 仅批准目标一次，没有手动批准范围或提交替代请求。结束时所有 Run 进程树均已停止，项目与目标预留预算均为零。

**原生 Leader 运行中取消验收也已通过。** 已观察到结构化会话，取消前确认真实存活 PID，取消后确认进程消失、静止与结算，保留实际耗时支出。本机 Claude 与 dsh 原生协议冒烟测试已通过；这不代表完整 Phase B dsh 协作或多机部署已通过。

最终根目录汇总检查与 GitHub CI 结果仍待确认，不将局部测试结果视为最终通过。多机 Phase B 与 Linux 原生部署仍待验证。既有普通局域网连通与任务测试不覆盖断线、重启、重复消息、旧租约及全部进程树停止语义；单元/协议模拟、CI 和跨平台编译分别记录，跨编译不等于目标系统原生认证。

## 统一 MCP 初始化（主要接入方式）

安装到固定目录后，先运行 `coordica setup` 预览，再用 `coordica setup --apply` 为发现的客户端注册同一个 MCP。支持 Claude Code、Codex、Cursor、Kimi Code、pi 的已核验配置格式，并为 dsh 生成本机 overlay；有版本或配置冲突会明确报告。需要指定客户端时使用 `--client`。Runner 按本机工具注册表发现能力，内置 Codex、Claude Code、dsh、Cursor CLI、Kimi Code、pi 的执行定义；新工具可以复用已有协议驱动注册，本机注册示例见 [custom-acp.json](examples/tools/custom-acp.json)。版本探测不代表已登录或已经完成真实任务验收。

macOS、Windows、Linux 都把对应平台的 `coordica` 可执行文件安装为 stdio MCP，参数为 `mcp --bootstrap`。在中心“中心配置与接入”生成一次性接入指令，交给执行电脑的 Agent；也可在“运行与节点”创建配对码，另行提供中心 URL 和必要的公共 CA 路径。MCP 自动发现 Git 与工具路径，交给 Runner 配对，并安装/启动用户级后台服务。无需手工执行 `pair` 或 `runner`。每台电脑使用独立节点身份，容量为 1 或 2。

本地探测不读取 Agent 认证文件；已有登录继续由各官方工具使用。用户级后台服务与聊天客户端分离；macOS 使用 launchd、Linux 使用 systemd --user、Windows 使用当前用户的计划任务。服务启动失败会明确报错并保留已配对状态，不伪装成已上线。

## 执行电脑的本机页面

Runner 启动后，在该电脑打开 `http://127.0.0.1:8081`，查看本机节点、工具能力、已领取任务、Run 状态和自己提交的需求。页面可向中心提交需求提案；提案先进入需求池，不因提交文字而获得目标或任务授权。Phase B 的目标批准与自动化设置由中心本机 Owner 完成。

本机页面使用 Runner 已配对的节点身份，无需输入 owner 令牌；节点令牌保留在 Runner 中，不发给浏览器。节点身份只能查看自己的运行与需求，不能批准、扩围、改预算或发布。页面仅监听回环 HTTP，前台启动可通过 `runner --console-listen 127.0.0.1:端口` 自定义端口。关闭页面或聊天客户端不影响后台执行。

若中心也在这台电脑，Runner 会核对已配置的中心地址是否属于本机及中心返回的连接信息，再显示跳转到中心需求池、任务池、运行与配置页的入口。中心页面仍须使用 owner 凭据登录，Runner 不会代为读取或授予 owner 身份；其他电脑的 Runner 页面不显示这些入口。

已有安装需将中心与执行电脑上的 Coordica 二进制升级到 `0.1.3-dev`，再重启服务，才能使用同机中心导航和新的接入配置页；保留原数据目录和节点身份。

## 两台电脑通过局域网连接

在中心生成证书，`--hosts` 必须包含第二台电脑实际使用的中心 IP 或 DNS 名：

```sh
./bin/coordica tls --out .coordica-tls --hosts localhost,127.0.0.1,192.168.1.10
./bin/coordica serve --data .coordica-center --listen 192.168.1.10:8443 --console-listen 127.0.0.1:8080 --tls-cert .coordica-tls/center.crt --tls-key .coordica-tls/center.key
```

把 **center.crt** 复制到 Runner 电脑，向已安装 MCP 的客户端下达指令，例如：“初始化本机 Coordica Runner，中心为 `https://192.168.1.10:8443`，CA 文件为本机 `center.crt` 的绝对路径，使用我提供的一次性配对码。”初始化工具返回节点 ID 和服务状态；中心节点页的心跳记录用于确认实际上线。

也可在中心配置页下载公共 CA 并生成完整接入指令，让远端 Agent 将指令中的公共 CA 写入自己的固定路径。指纹核对使用证书 DER 的 SHA-256，不能直接计算 PEM 文件文本的哈希。

将示例 IP 替换为中心实际的局域网 IPv4。`https://192.168.1.10:8443` 只提供 Runner 配对与执行接口，不提供控制台或 owner API。控制台仍在中心电脑的 `http://127.0.0.1:8080` 打开，不需要浏览器信任局域网的自签证书。

`--console-listen` 默认 `127.0.0.1:8080`，仅接受回环地址，端口可自定义。默认启动时两个监听地址相同，在本机共享 HTTP 服务；指定局域网监听后分为本机控制台和 HTTPS 节点接口。节点接口也可使用 `--listen 0.0.0.0:8443`，此时需加上 `--runner-url https://192.168.1.10:8443`，向配置页明确可分享的实际地址；该地址必须使用 HTTPS，且主机名或 IP 须被 TLS 证书 SAN 覆盖。未配置实际地址时不会把 `0.0.0.0` 或 `::` 复制给远端 Agent。私钥只留在中心，非回环 HTTP 连接会被拒绝；不提供跳过证书验证选项。证书默认一年有效，公网中转尚未实现。

## GitHub 发布权限

开发 Runner 通过 bundle 收发固定代码材料，不接收中心发布凭据。中心 Git 客户端禁用系统/全局 Git 配置、交互凭据、仓库 hooks 和外部过滤器；平台不会借用 Runner 的 GitHub 写凭据。

对 HTTPS 仓库，可把完整 `Authorization` 值存到中心专用私密文件，再使用 `serve --git-auth-file 文件 --git-auth-remote 精确仓库HTTPS地址`。文件内容通常为 GitHub 支持的 Basic 授权值；不要把凭据放进 URL、项目配置、CLI 参数或仓库。授权严格绑定一个 HTTPS 地址且禁止重定向。

“只有本人/本人授权中心可写”还依赖 GitHub 仓库权限和分支规则。部署到你自己的项目时，需要自行设置仓库权限和中心专用凭据；下载本项目不授予本项目仓库的写权限。私人电脑原有用户凭据不属于本平台能绝对隔离的安全边界。

## CLI 与 MCP

以下是诊断入口，正常接入使用上面的 MCP 初始化。需要前台排障时，可手动配对和启动：

```sh
./bin/coordica pair --center https://192.168.1.10:8443 --ca center.crt --data .coordica-runner --code-file pairing.code
./bin/coordica runner --data .coordica-runner
```

配对码也可通过 stdin 输入；配对后删除临时码文件。`--tool-executable 工具ID=路径` 可重复指定注册工具的本机路径；旧三工具参数保留兼容。`--tools-file` 加载本机可信 JSON 注册表。`coordica doctor` 只做版本探测，不会把已安装误报为已认证或已验收。

owner CLI 在中心电脑使用本机控制台的 HTTP API，默认地址为 `http://127.0.0.1:8080`，默认令牌文件为 `.coordica-center/owner.token`。所有写操作都需要幂等键。自动生成的键打印到 stderr，丢失响应后可用 `--key` 重试：

```sh
./bin/coordica ctl
./bin/coordica ctl --path export > export.json
./bin/coordica ctl --method POST --path pairings --body-file pairing-request.json
./bin/coordica ctl --method POST --path backups
```

自定义控制台端口或中心数据目录时，分别设置 `ctl --center http://127.0.0.1:端口 --token-file 数据目录/owner.token`。此前指向局域网 HTTPS 地址的 owner CLI 需改用本机控制台地址；仅分离中心监听不要求升级已有 Runner，它们保持原 HTTPS 地址、CA 和节点身份，无需重新配对。执行电脑的本机页面需使用支持该页面的 Runner 版本。

`--body-file -` 读标准输入。暂停/恢复使用 `project/pause`、`project/resume` 或 `tasks/任务ID/pause` 等路径，JSON 为 `{"expected_version":当前版本}`。当前版本来自状态快照；旧版本写入返回冲突。协议记录见 [API Schema](api/v1.schema.json)。

受限 MCP 使用已配对的节点凭据，固定绑定一个 Run：

```sh
./bin/coordica mcp --runner-data .coordica-runner --run run_id --generation 1
```

提供 `get_context`、`report_block`、`request_scope`。范围申请只登记阻塞，不能放宽权限。最终代码与结构化评审由 Runner 在受管进程停止、固定材料校验后提交，MCP 不允许模型提前宣称进程已停止。stdio 遵循 [MCP 2025-11-25](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)，没有 owner 批准或发布工具。

## 暂停、恢复与备份

暂停停止新派发并要求在途进程结束。已经进入 `dispatching` 的外部 Git 操作继续核对真实远端结果，成功不会被当前暂停状态抹掉。失联 Run 不会自动当作已停止；Runner 回报受管进程已停止，或 owner 在运行页填写实际停止证据后，中心自动恢复原任务，无需再手动恢复。兼容且有空闲容量的 Runner 随后原子领取新 Run，使用新的执行代次和独立工作目录。

自动恢复消耗原任务的一次重试额度，保留累计支出；失联且实际用量未知时，按整个有限执行额度结算。预算不足或重试用尽会阻塞并等待 owner 决策。任务暂停、取消、授权变化和人工阻塞不会被自动清除；项目暂停期间保持待恢复，项目恢复后再推进。旧 Run 的迟到结果不会改变新执行、候选或预算。已经受理的候选继续原评审与验证流程，不重新开发。

中心重启会把旧验证进程标记为待核对，确认停止后重试同一个固定候选。Runner 保留工作目录和完成消息，恢复时先交接/核对，未知旧进程不会被盲目重派。macOS/Linux Runner 会复核遗留受管进程组并在确认静止后补报停止；Windows 的未知停止记录继续等待可靠确认，不能仅凭父进程消失放行。正常 Windows 执行通过 Job Object 确认受管进程停止后，可以自动恢复。重试与修复次数、预算均保留；增加任务预算/次数需要 owner 新版本批准。

Web 或 CLI 创建一致备份，返回中心上的备份目录。备份包含数据库快照、引用材料及必要 Git 对象，清单核验完成后才发布。凭据不进入备份。恢复到不存在的新目录：

```sh
./bin/coordica restore --from 备份目录 --data 新中心目录
./bin/coordica serve --data 新中心目录
```

恢复后项目暂停，旧节点撤销，重新配对并核对 Git 外部事实后恢复。旧电脑未上传的私人修改不属于恢复保证。升级前先备份，停止旧进程再启动新二进制。中心与 Runner 升级到 `0.1.3-dev` 后可使用新的接入配置页和同机中心导航；`0.1.2-dev` Runner 保留本机活动页，`0.1.1-dev` Runner 仍可连接新中心但没有本机页面。`0.1.0` 中心的严格解码不接受新能力字段，不能混用新 Runner；新中心会保守阻止缺少取消/评审声明的旧 Runner 领取新活动。在途旧 Run 按原租约核对。代码合并不自动升级服务。

## 维护

真实 Agent 验收可使用安装包中的 [独立小项目](examples/acceptance/README.md)，在隔离仓库执行，不影响本项目或用户实际项目。

没有开启后台自动升级或公开上传。Phase B 自动化默认关闭；启用也不提供全局无限授权。
