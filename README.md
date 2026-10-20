# Coordica

多电脑、多 Agent 开发协调底座。采用 Go 中心与 Runner、SQLite、React/TypeScript 控制台；Web 产物嵌入中心二进制，无 Redis、消息队列或额外后台运行时。

当前源码版本是 **0.2.5-dev 开发预览版**。已实现固定 Git 候选、独立评审、分层测试与条件发布闭环，以及四池、底部 Dock、Agent 管理和测试中心工作台；实际可下载版本与对应验证记录见 [Releases](https://github.com/5intro/Coordica/releases) 和 [GitHub Actions](https://github.com/5intro/Coordica/actions)。协议模拟测试、跨平台编译和托管 CI 不代表用户电脑上的真实部署验收。

## 许可与下载

**源码可见，个人非商业使用免费。** 允许个人非商业修改并在自己的设备上使用；个人商业用途、团队或公司使用及二次开发、向他人发布修改版均须事先另获书面授权。这不是 MIT 或 OSI 开源许可。正式条款见 [LICENSE](LICENSE)，授权申请可向本仓库维护者提交 Issue。

第三方组件保留各自许可，见 [第三方声明](THIRD_PARTY_NOTICES.md)。本许可不会仅因使用 Coordica 就要求你公开独立开发的项目。

检出或更新源码不会自动安装、发布、部署或更新已安装服务。预编译包由 Owner 完成独立验证后另行发布；实际可下载版本以 Releases 为准。`coordica setup --apply` 注册本机唯一的 Coordica MCP；中心角色先保存可信主机配置，节点角色沿用所选 Runner 目录。Windows 分发包附 `coordica.exe` 与 dsh 模板；分发包附 `SHA256SUMS`。

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
python scripts/test-go.py
```

维护者测试需要 Python 3.10+，无需第三方 Python 包。`make test` 与 CI 共用 `scripts/test-go.py`：每次使用独立临时目录，正常结束、失败、超时或中断后先确认测试进程停止，再回收该目录。强制结束监督器留下的目录会在下次运行时按归属和进程创建身份恢复；不能确认停止时保留目录并让检查失败。默认保留 8 GiB 空闲空间，测试临时文件达到 4 GiB 或整轮超过 20 分钟时停止；可用 `--min-free-gib`、`--max-temp-gib`、`--timeout-seconds` 显式调整为正的有限值。源码、全局缓存及正式运行数据不在回收范围内。

监督器为每轮测试保留独立守护进程；Unix 以经过创建身份核对的会话追踪父进程退出后的普通系统命令，Windows 在任务启动前加入禁止脱离、关闭即终止的 Job Object。目录创建与旧目录恢复使用同一跨进程锁，回收前重新核对最新所有者身份与标记。缺少新版进程归属证据的旧版标记保留，不自动删除。

Windows 默认使用 CI 的 `RUNNER_TEMP` 或用户目录中的短前缀测试目录，避免叠加系统长临时路径触发 Git 限制；可用 `--temp-parent` 自选更短路径。回收仅处理本轮自有目录中的 Git 只读文件，不改变系统长路径设置。

`make generate` 从 Go 协议记录生成 `api/v1.schema.json` 与 Web 类型；不手改生成文件。网络输入仍由中心做严格 JSON 解码、领域校验和授权检查。

构建后运行 `./bin/coordica version` 应输出 `0.2.5-dev`；各 MCP 角色的初始化元数据也使用同一发布版本。

维护者准备好 `bin/coordica`（macOS arm64）、`bin/coordica-windows-amd64.exe` 和 `bin/coordica-linux-amd64` 后，可运行 `make package`（默认版本 `0.2.5-dev`），或显式运行 `python3 scripts/package-release.py --version 0.2.5-dev`，用 Python 3 标准库生成三个分发包、各平台 `START-HERE.txt` 和 `SHA256SUMS`。脚本校验许可、包内文件及二进制字节；它不构建程序、不上传发布，普通下载用户无需安装 Python。

## 可信中心设置与项目初始化（0.2.3-dev）

一个中心仍只登记一个项目。已有验收项目的仓库身份与历史不能替换成 Coordica；自托管请显式选择新的中心数据实例和独立 profile。主机角色、可执行文件、配置、数据、profile 和监听端点只由本机人类或获授权终端设置，MCP 调用不能选择它们。

已有明确检查政策时，可准备可信检查文件（分析期间不执行这些检查）：

```json
[{"name":"Go tests","command":"go","args":["test","./..."],"timeout_seconds":300}]
```

保存新中心的固定配置，再注册一个 MCP。下面的绝对路径由安装者替换为自己选择的路径；带空格的路径应加引号。

```sh
/installed/coordica host-config --host-role center \
  --path /chosen/config/center.json --binary /installed/coordica \
  --codex-executable /official/codex --data /chosen/runtime/center \
  --chat-profile /chosen/profiles/interaction.json \
  --repository /projects/Coordica --repository-url https://github.com/5intro/Coordica.git \
  --branch main --name Coordica --checks-file /chosen/config/checks.json \
  --console-listen 127.0.0.1:8080 --runner-listen 127.0.0.1:8080
/installed/coordica setup --host-role center --host-config /chosen/config/center.json --runner-data /chosen/runtime/developer-runner --client generic --output /chosen/mcp --apply
```

`--checks-file` 可省略：新中心会登记 `checks=[]` 的暂停草案，允许无提交的新项目先访谈。空有效检查绝不能确认基线或执行任务。Owner 在控制台“项目初始化”的检查政策表单或 `POST project/policy` 保存非空真实检查后，重新分析并确认；不得使用空转命令作为代替。政策请求为 `{"expected_version":项目version,"checks":[真实检查],"protected_paths":[当前保护路径]}`，每项检查超时 1–3600 秒。

`--repository` 是明确选定的本地源码输入；可选 `--repository-url` 是后续开发的固定目标 HTTPS 或 bare 仓库，省略时使用本地输入作为目标。已有 HTTPS 项目可登记 `source_repository` 为预先准备的本地 clone；初始化只读取该 clone 的选定分支，不自动联网 clone/fetch。发布到普通非 bare 本地仓库不是支持的集成目标。新项目访谈可使用尚无提交的明确新目录；首次开发前由 Owner 在固定目标与源码输入中建立对应基础提交，随后单独批准 Goal/任务。此处的目标 URL 不授予集成或外部发布权限。

`--runtime-path` 固定用户服务的工具搜索路径；可用 `--https-proxy http://127.0.0.1:7890` 保存不含身份信息的标准代理。认证仍沿用原生 Codex 官方配置，版本发现不证明已登录。Leader 与开发使用原生 Codex，默认模型为 `gpt-6.1-sol`；Owner 可在 Agent 管理页为 Runner 选择工具实际报告的模型及思考强度。禁用嵌套 Agent，没有空闲模型终端。中心与 Runner 不需要 Redis、额外 daemon 或可选 AOCI。

普通聊天使用统一 stdio：

```sh
/installed/coordica mcp --interaction --host-role center --host-config /chosen/config/center.json --chat-profile /chosen/profiles/interaction.json
```

离线可调用 `inspect_environment`、`initialize_center`、`start_center`、`get_host_status`，均接受 `{}`。初始化登记暂停的草案项目、保存本机 Owner 凭据、发行有限中心聊天 profile，再请求 macOS launchd、Linux `systemd --user` 或 Windows 当前用户 Task Scheduler 激活；Owner token 不经 MCP 返回。也可调用同一生命周期 CLI：`coordica host --host-config /chosen/config/center.json --action initialize|start|status`。Owner API 的 `GET system/host` 与 `POST system/host/initialize`、`system/host/start` 使用相同已绑定配置。

用户服务激活请求、经过认证的数据实例健康、Runner 登记/心跳及模型版本发现分别报告。离线未知状态为 `null`，不能把管理器接受请求当作中心健康或项目已暂停。任一固定端口被其它实例占用、saved config 冲突或未知进程拥有数据锁时，拒绝替换/重启。重试保留身份、证书、profile、暂停与自动化状态。远端 Runner 可显式使用不同的 `--runner-listen` 与固定 `--runner-url https://中心地址:8443`；首次初始化生成对应 TLS，Owner 控制台始终只在 loopback HTTP 上。

同一中心 MCP 同时保留 `initialize_runner`、`save_runner_profile` 和唯一的 `inspect_environment`，使用安装者选定的独立 `--runner-data`（省略时为用户配置目录的 `Coordica/runner`）。直接 stdio 启动：

```sh
coordica mcp --interaction --host-role center --host-config /chosen/config/center.json --runner-data /chosen/runtime/developer-runner --chat-profile /chosen/profiles/interaction.json
```

同一会话先 `initialize_center {}`，再由 Owner 创建配对码后调用 `initialize_runner {"center_url":"http://127.0.0.1:8080","pairing_code":"Owner签发的短期码"}`；无需第二个 MCP。初始化 Runner 保留中心 limited profile，仅在其自身保存的固定中心验证，不向调用提供的其它端点发送中心 token。

节点安装使用 `setup --host-role node --runner-data /chosen/runner --chat-profile /chosen/node-profile.json`。保留 `initialize_runner` 的配对、工具注册和节点聊天发行。节点 profile 必须匹配所选 Runner 登记；拷贝中心 token 不能升级节点的 MCP 角色。旧 launcher 省略角色时保留其已有选择；明确的角色、配置或路径冲突不自动迁移。disabled、timeout 与其它客户端设置保留。遗留未限定或撤销凭据不能静默续发；明确检查后使用已有 `chat-init --renew`。要改变主机身份，请显式选择新的配置/data/profile 和客户端注册，而不是覆盖旧项目。

服务启动后，中心普通聊天可调用：

```json
{"name":"open_project","arguments":{"expected_version":0,"kind":"existing","idempotency_key":"project-open-1"}}
{"name":"get_project_initialization","arguments":{}}
{"name":"answer_project_initialization","arguments":{"expected_version":1,"question_id":"purpose","original_text":"原始用户回答","source":{"session_id":"session-1","message_id":"answer-1"},"idempotency_key":"answer-1"}}
```

`kind=new` 只适用于服务端独立核验的无提交输入。分析从固定 SHA 的 tracked/public blobs 生成只读 scratch 与 manifest；忽略私有 docs、环境文件、凭据、运行目录、生成产物和大锁文件。相关源码单文件超过 512 KiB 或总计超过 16 MiB 时拒绝分析，不截断后声称完成。结果分别记录可用文件、报告检查的覆盖、未报告检查文件与省略原因；事实只能引用报告检查的文件，每个源码目录必须有检查证据才能进入待确认状态。分析不执行仓库检查/脚本、创建任务、改写 Goal、开自动化或解除暂停。新项目用已确定依赖的 frontier 轮次，自动研究已知事实，只问缺失决定，保留每次原始回答与来源。

控制台“项目初始化”展示 `draft`、`analyzing` / `interview`、`awaiting_owner`、`initialized`、SHA、覆盖、建议、问题和反馈。只有 Owner 可确认审阅的精确版本与摘要：

```sh
coordica ctl --center http://127.0.0.1:8080 --token-file /chosen/runtime/center/owner.token --path initialization
coordica ctl --center http://127.0.0.1:8080 --token-file /chosen/runtime/center/owner.token --method POST --path initialization/confirm --key confirm-reviewed-1 --body-file /chosen/reviewed-confirm.json
```

确认文件是 `{"expected_version":1,"digest":"读取并实际审阅的当前64位摘要"}`。`initialization/analyze`、`initialization/answer` 为相同 Owner API；普通聊天仅能在 `chat/initialization`、`chat/initialization/analyze`、`chat/initialization/answer` 使用有限对应方法，没有确认接口。检查与保护路径须与当前实际政策一致；Owner 可在暂停且安全静止时通过 `project/policy` 明确改变政策并重新分析，建议不会隐式改变有效检查。

共享基线是所有任务、claim、上下文、Run、候选验证和 landing 的必需门禁；没有 Goal 也不能绕过。Goal 存在时仍要独立批准。普通代码提交不要求每次重新初始化，初始证据保持固定；重新初始化会使旧任务授权和结果失效，且拒绝活跃/未知存活进程、规划或候选。每个分析尝试使用请求登记时 Owner 保存的 `PlanningTimeoutSeconds`（默认 300 秒，1–604800 秒），重试使用 `PlanningMaxRetries`（默认两次，0–2），停止后输入核验另有 10 秒上限。Owner 可在 `system/automation` 设置较长有限分析超时（规划累计预算须不少于该超时），无需启用自动化；初始化不受项目生命周期额度限制。没有停止证据时保留未知存活与预留支出；重启不推断停止。Owner 实际核验后可使用 `initialization/confirm-stopped`，在线取消用 `initialization/cancel`。项目累计统计不是生命周期额度。

Runner 在既有领取循环中先通过 `POST /api/v1/runner/presence` 上报节点活动和能力快照，再请求领取。该接口使用与 claim 相同的 `{capabilities:[...]}` 请求体，只接受当前已配对且未撤销的节点身份，不需要幂等键；暂停、等待基线确认或旧执行尚未确认停止并占满容量时仍可上报。上报不能批准基线、创建任务、续租或证明旧进程停止；能力快照也不等于工具已通过原生执行验收。升级时先升级中心再升级 Runner；旧 Runner 仍可通过原有 claim/Run 心跳上报，但等待基线确认时须升级 Runner 才能独立刷新活动时间。

模型目录探测不提交提示词。ACP 工具需要会话才能返回模型选项时，复用每个本机工具启动配置独有的探测会话，并在查询后关闭运行资源；`session/close` 不等于删除对话历史。会话身份和固定空工作目录存于系统用户配置目录的 `Coordica/catalog-sessions/`，可由可信本机环境变量 `COORDICA_CATALOG_STATE_DIR` 指定；多进程互斥，普通缓存过期或重启不会另建会话。工具须实际公布恢复/加载能力；恢复失败、创建回执丢失或状态损坏时标记目录不可用，不自动新建替代、不接管用户会话，工具默认执行仍由其原有执行能力决定。不要把该目录当一次性缓存删除；其中的状态用于避免重复创建持久对话。Codex 只初始化及查询模型目录，Claude 与 pi 的探测保持不保存会话。

0.2.3-dev 首次打开旧数据库执行一次版本化、原子审计的 authority policy migration v1，补齐 `internal/mcp/**`、`internal/mcpconfig/**`、`internal/chatprofile/**`、`internal/centerhost/**` 等必需平台保护。若有变化，暂停项目、请求旧活动停止、使旧任务/共享基线失效，保留项目身份、历史和实际支出；不自动批准旧库或复活旧授权。原生目标部署与真实模型验收由 Owner 另行完成；跨编译不是原生部署证明。

有限聊天的初始化读、请求、原始回答及幂等重放统一使用脱敏应用投影：凭据、本机 workspace 路径、检查命令和参数不返回。原始回答与完整检查政策私有保留给 Owner 审阅；聊天不能用脱敏投影批准规范。profile 的固定端点与 CA 在发送 token 前与启动配置 / 已登记 Runner 核对，然后在相同加载 profile 上验证服务器持久化的身份与范围。兼容独立 chat launcher 在启动时固定既有 profile 端点与 CA，不借用 Owner/node 凭据。

用户服务激活前查询对应固定 label/unit/task 的实际定义，核对固定可执行文件、参数及身份；不识别的登记、缺失的 owned 定义或不一致的动作一律拒绝，不 bootout 其它 label，不用 Task Scheduler `/Create /F` 覆盖。Windows 使用固定只读的本机 Task Scheduler 数值 HRESULT 探针，不依赖中文或英文错误文本；访问拒绝、未知错误或身份不符仍拒绝激活。macOS 管理域预检只查询 disabled 状态，不读取整域服务清单。

受限 MCP 仅限制自己的工具、路由与 token，不隔离普通聊天其它终端/文件工具或本机权限。不得把 Owner token 交给普通聊天；Owner Web/CLI/API 已足够，无最高权限聊天通道。

## 运行目录与 macOS 权限

首次安装时自行选择固定可执行路径，macOS 建议使用 `~/Library/Application Support/Coordica/bin/coordica`，再从该位置执行 `setup`。`setup --runner-data 绝对路径` 可自定义 Runner 数据目录；中心和 Runner 均支持 `--data 绝对路径`。MCP 用户配置默认保存在用户配置目录，项目仓库建议放在 `~/Projects` 等 Desktop、Documents、Downloads 之外的目录。macOS 对 Desktop、Documents 和 Downloads 另有文件访问授权，仅改变启动工作目录不会授予权限。[Apple 文件访问说明](https://support.apple.com/en-ca/guide/security/secddd1d86a6/web)

当前开发版没有稳定的 Developer ID 分发签名。构建产物变化会改变 ad hoc 签名的 `cdhash`，系统可能重新请求权限，不能保证一次授权跨版本有效；请按实际需要授权，不要为减少弹窗而授予全磁盘访问。[Apple 分发签名说明](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac/)、[DR 身份说明](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements)

已有节点升级保持原数据目录和节点身份，不直接搬动已有数据；材料和仓库记录可能使用绝对路径。本次源码版本为 v0.2.5-dev，中心与 Runner 使用匹配版本；升级并重启由用户实施，代码合并不会更新运行中的服务。

## 本机启动

```sh
./bin/coordica serve --data .coordica-center
```

在运行中心服务的电脑上打开 `http://127.0.0.1:8080`，将 `.coordica-center/owner.token` 中的令牌输入登录页。控制台及 owner API 只监听本机回环地址；令牌不打印到服务日志，请不要把 owner 令牌交给 Agent。

8080 是统一管理入口，包含需求池、任务池、运行与节点、政策与恢复，以及“中心配置与接入”。登录后可直接访问 `http://127.0.0.1:8080/#connection`，即使尚未建立项目也能查看真实监听地址、版本、数据目录、公共 CA 和证书 DER 的 SHA-256 指纹。

为每个远端节点分别生成一次性接入指令，再发给那台电脑的 Agent；指令包含公共 CA 和配对码，不包含 owner/node 令牌、私钥或中心私有数据路径。复制失败时使用“复制已有接入配置”或手动复制文本，保留原配对码；到期后再重新生成。

Web 中建立项目、明确最低检查，录入需求，再按下方 Phase B 流程保存、批准和评估目标。仓库地址支持不含凭据的 HTTPS URL，或中心本机 Git 仓库绝对路径。远端需要已有目标分支与至少一个提交。阶段 A 针对 Coordica 自举，中心强制保护授权、验证、集成、适配器与初始化服务模块；用户还可追加保护路径。路径模式按仓库根目录、大小写敏感的 `/` 路径匹配，支持 `*` 和完整路径段 `**`；符号链接、submodule 等当前不支持的变化会拒绝集成。

## Phase B：目标授权、Leader 与有限自动执行

在中心本机 Owner 控制台的“项目概览”建立项目，填写仓库、目标分支、受保护路径和最低检查；在“需求池”录入需求。随后进入“目标规划与 Leader”，创建完整目标文档：objective（目标）、non_goals（非目标）、acceptance（验收标准）、关联 requirement_ids、architecture（架构）、directory_outline（目录大纲）、字面 allowed_roots（允许根目录）和正整数 budget_seconds（有限累计目标预算）。空架构或缺失边界仍属于规划，不表示可执行。

保存后由 Owner 点击“一次授权”，批准当前文档与授权版本。再向 Leader 输入说明并点击“评估规划”；“发送消息”与“评估规划”都提交版本化规划执行请求，最终以实际运行及中心准入记录为准。Leader 是中心配置的原生 Codex CLI 执行，默认显式使用 `gpt-6.1-sol`、只读规划材料；它与桌面聊天会话不同。规划请求、执行代次、租约、截止时间、进程树停止证据、结算、结构化结果和任务映射均持久化。模型文字中的 ready 或“完成”不能替代中心准入、授权或实际进程停止。Runner 将 Owner 选择的模型与强度固定到新 Run，原样传入原生执行进程；未选择时沿用默认模型，不静默回退到其他模型。

目标获批准并经实际 Leader 评估准入后，启用系统自动化才允许在已批准架构、允许根目录、政策及剩余累计预算内自动批准普通任务与普通范围。中心本机“系统自动化”页面默认关闭开关，可设置规划超时、有限规划累计预算及最大重试次数（0–2）。关闭开关阻止新自动授权，不取消已经授权的任务；目标一次授权独立于开关。新目标、架构变化、检查变化、受保护变化和外部权限仍需要 Owner 明确批准；受保护变化针对固定候选 C 特别批准。测试通过不能替代这些授权，决策处理也不能批准目标或绕过预算。保存目标修改会使旧目标批准与准入失效。

原生严格执行意图协议 v1（native execution intent v1）要求最终输出仅为严格 JSON 对象，字段为 `version=1`、`status`、非 null 的字符串数组 `requested_paths`、字符串 `reason` 与非空 `summary`；不接受额外字段、重复字段、缺失字段或 JSON 外的文字。`completed` 只表达执行意图，不是完成证据；Runner 仍须核对实际变化、范围、固定基线和进程树静止，随后由独立评审、中心检查与固定候选条件发布决定结果。意图与请求回执均不授予权限。

任务范围不足时，执行 Agent 必须在任何编辑前停止，保持固定工作区无变化，返回 `scope_requested`、准确的新增仓库路径及原因。Runner 确认实际进程树停止并复核无变化后，持久化并提交绑定原 Run、执行代次和任务的请求；中心核对停止与结算，再交给只读 Leader 评估。仅在自动化启用、目标批准与准入仍有效、请求属于目标字面允许根目录、政策与剩余累计预算允许时，普通范围才可自动批准。同一任务随后以新授权和新执行代次恢复，保留累计支出，不创建替代任务或清零预算。请求投递及规划重试均有限；旧版本、无变化核对失败或未知停止状态不会放行。

重大工作或其他阻断返回 `blocked`，保持工作区无变化并说明原因；重大工作不会自动批准，仍需 Owner 决策。`scope_requested` 与 `blocked` 是停止交接，不是成功完成，不能通过模型文字或测试通过绕过边界。

这些 UI 使用同一套本机 Owner API，前缀为 `/api/v1`；CLI `ctl --path` 省略该前缀。真实操作路径如下：

| UI 操作 | HTTP / CLI 路径 | 请求内容 |
| --- | --- | --- |
| 建立项目 | POST `project` | name、repository_url、target_branch、protected_paths、checks（total_budget_seconds 已弃用并忽略） |
| 录入需求 | POST `requirements` | title、body |
| 需求详情与历史 | GET `requirements/{id}?before=<内容修订号>` | 返回当前内容、影响任务及最近 20 个版本 |
| 修订需求 | POST `requirements/{id}/revise` | expected_version、title、body、reason |
| 撤回需求 | POST `requirements/{id}/withdraw` | expected_version、reason；保留历史 |
| 重评被阻断的变更 | POST `requirements/{id}/reassess` | expected_version 使用变更记录版本 |
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

“Agent 池”按每台 Runner 实际探测到的安装清单展示工具，未安装的注册模板不占位；已安装但探测失败仍可查看异常，在途或未确认停止的运行不会因工具消失而隐藏。工具注册与安装发现分开，不保证固定 Agent 名单。Owner 通过节点配置控制工具 enabled、implement（实现）、review（评审）、模型和思考强度，节点容量为 1 或 2，并可排空、禁用新工作或撤销节点。模型与强度只接受工具实际报告的选项，Codex 不限制为单一模型；选择仅影响新 Run，失效选项拒绝派发。能力发现与调度权限分开；排空、禁用或撤销不表示在途进程已经停止。工具支持以节点实际报告、可信工具注册表和协议能力为准，安装或版本探测不代表已认证、可承担某角色或已验收。其他 Agent 可按报告能力承担已允许的角色。

远端执行电脑的 `http://127.0.0.1:8081` 只展示该 Runner 身份可见的本机能力、任务、运行与需求提案，不提供 Owner 目标批准、扩围、预算或发布权限。远端浏览器不能通过局域网节点接口访问中心 Owner UI；同机导航也仍要求 Owner 独立登录。

## 分层测试与验证任务（0.2.4-dev）

开发 Agent 自测后，中心对固定预合并候选执行必需快速检查，再交独立 Agent 审查；候选、主线与规则不变时直接复用检查证据合并。项目基础检查始终保留，按版本化测试套件增加相关检查；未知影响和受保护变化扩大检查范围。全量回归、黄金旅程、端到端与长生命周期测试独立执行，不为每个任务重复运行。

Owner 通过 `tests/suites` 配置套件，通过 `tests/run` 请求有限验证任务。纯验证复用任务、Run、取消、重启恢复和累计记账，无代码候选；报告绑定提交、套件版本、平台和进程退出证据。首期执行位置是中心主机，命令不会下发到其他 Runner；其他平台仍须实测。测试代码开发仍使用普通任务，修复可通过 `test_run_id` 关联失败的验证 Run。

需求修订使用独立的内容版本，不因关联关系变化而失效。未完成的相关任务先冻结，旧执行确认停止后由 Leader 重排；任务和 Run 固定内容版本，迟到结果不能推进新版本。新增需求可以合并到未派任务，关联在途工作的增量使用依赖后续任务。撤回仅取消专属任务，共享任务保留其他有效需求；已集成内容生成补偿任务，继续经过原测试、审查与精确集成，不直接回滚主线。重新规划保留累计消耗、重试、修复及独立人工阻断。暂停或自动化关闭时变更只排队，重大决定仍由 Owner 处理。

正式修订、撤回和重评使用 Owner 的 Web/API（CLI 的 `ctl` 命令复用相同接口）。有限交互 MCP 可继续通过既有 `propose_change` 提交变更建议，由 Owner 核对后正式修改需求；它不能直接替代决策或取得 Owner 权限。

`tests/readiness?commit=<完整提交>` 查询该版本的项目/发布测试就绪情况，未执行、取消、套件更新或版本变化均不能复用旧通过结果。发布工具须显式要求 `ready == true`；查询不授予发布权限。需求通过 `requirements/{id}/link` 建立原子双向关联。输入示例与限制见 [测试配置说明](examples/testing/README.md)。

## 项目工作台（0.2.5-dev）

首页显示需求、任务、审查、阻断四池的具体活动列表、人工待办和按主机分组的真实运行实例。底部 Dock 进入完整池页面、Agent 管理、测试中心与设置；搜索、筛选、详情永久链接及浏览器前进/后退保留上下文。手机端合并工作池入口，保留可见的局部池切换。

测试中心管理版本化套件、固定提交验证、历史报告和发布就绪状态；失败修复可关联原验证 Run。尚无证据、取消、跳过或其它版本的成功均不能显示为当前版本通过。Agent 页区分真实执行与工具能力，右侧编辑器保存真实的模型与思考强度配置；设置集中已有项目、系统、接入、初始化、规划、审计与恢复入口。

界面参考 macOS 的中性工作面、成组设置行、分段视图与轻色 Dock；日常列表优先显示名称、状态和操作，内部编号、工具诊断与完整输出按需展开。功能和视觉实现通过本机真实 Coordica 的原生 `gpt-6.1-sol` 开发、程序门禁、独立 Claude 只读评审及固定候选集成链路完成。该本机自举记录不等于 Windows/Linux 原生部署或多机 Phase B 验收。

## 验证状态

既有 v0.2.1-dev 包含主机范围查询闭包、持久化节点签发校验、跨页面咨询重试信封、稳定阻断分页、延迟停止决策核销和有限长运行配置。2026-10-13 已通过 macOS 独立本机真实 stdio MCP 与原生 `gpt-6.1-sol` Leader 验收：两个逻辑节点分别使用受限凭据及实际中心/Runner 端口，核对共享 profile 复用、需求来源与去重、主机查询与监听入口权限，以及撤销后原连接拒绝重放；无目标和已批准准入目标两种场景下，自动化开启仍只产生咨询回复，目标、授权、范围、需求、任务、发布意图及 Git 主线保持不变，所有执行停止并结算。逻辑节点测试不代表真实跨机部署。需求版本化撤销/编辑与实时开发者邮箱尚未实现。

v0.2.2-dev 的统一 MCP 实现及评审修复已由独立验收通过完整 macOS Go race 套件（包括全部监听测试）、真实原生可执行文件 stdio 与隔离双监听中心测试：缺失 profile 后，同一 MCP 进程获得十一项中心工具；节点受限 profile 仅获得七项工具，不能访问 Leader 或全局任务池；Run 仅有三项范围内工具并拒绝跨角色调用。中心实际重启期间 MCP 进程保持存活；撤销凭据后，缓存读取与需求提交均被拒绝。

v0.2.2-dev 的原生 Codex CLI 安装配置验收确认旧 chat-only 接入迁移为唯一 `coordica`，保留所选路径、启用/禁用状态、超时、无关设置、备份与幂等性。评审修复另覆盖 TOML 文件末尾分隔及无关表、项目级 MCP 名称状态、隐式目录环境与文件别名检查，以及本机 Runner 保留真实验证的中心 chat profile。开发侧离线 stdio 测试及五项 Python 打包测试通过。

v0.2.2-dev 的统一接入验收没有运行模型，没有派发开发、规划 Run 或任务；节点注册是逻辑 fixture，没有激活用户服务。CLI 配置安装已测试；原生 GUI 客户端动态工具刷新仍取决于客户端支持，必要时重新发现或重连。该 v0.2.2-dev 验收未新增 Windows/Linux 原生部署或所有客户端 GUI 执行认证。

v0.2.3-dev 的原生服务、统一 MCP 与只读 Leader 就绪验证记录以对应 Releases 的发布说明为准，由 Owner 独立验证后记录。实时开发者邮箱、索引集成、模型/effort/并发设置与资源扩展仍属后续功能。

既有 Phase A 已完成真实 Mac 中心与 Windows 节点连接、Windows dsh 实现、Claude 独立评审的小项目闭环，以及运行中取消验收。该 Phase A 的实际 Agent 验收事实限于 Claude / dsh，不把支持定义或版本探测说成其他工具已实测。

**既有 v0.2.0-dev macOS 本机 Phase B 端到端验收已通过。** 实测流程为原生 Codex CLI `gpt-6.1-sol` Leader 规划 → 自动授权一个任务 → 原生 Codex 返回 `scope_requested` 且无编辑 → 实际进程停止 → Leader 在目标字面允许根目录内自动批准普通范围 → 同一任务以新授权恢复并保留累计支出 → 原生 Codex 开发 → 独立本机 Claude 节点只读评审 → 中心对不可变固定候选执行 `node:test` 验证 → 精确固定候选条件更新本地测试 Git 的 `main`。Owner 仅批准目标一次，没有手动批准范围或提交替代请求。结束时所有 Run 进程树均已停止，项目与目标预留预算均为零。

**既有 v0.2.0-dev 原生 Leader 运行中取消验收也已通过。** 已观察到结构化会话，取消前确认真实存活 PID，取消后确认进程消失、静止与结算，保留实际耗时支出。本机 Claude 与 dsh 原生协议冒烟测试已通过；这不代表完整 Phase B dsh 协作或多机部署已通过。

完整测试与各平台 CI 结果以当前发布版本关联的 GitHub checks 为准，本地局部检查不替代完整测试或平台 CI。多机 Phase B 与 Linux 原生部署仍待验证。既有普通局域网连通与任务测试不覆盖断线、重启、重复消息、旧租约及全部进程树停止语义；单元/协议模拟、CI 和跨平台编译分别记录，跨编译不等于目标系统原生认证。

## 统一 MCP 初始化（主要接入方式）

安装到固定目录后，先运行 `coordica setup` 预览，再用 `coordica setup --apply` 为发现的客户端注册同一个 MCP。支持 Claude Code、Codex、Cursor、Kimi Code、pi 的已核验配置格式，并为 dsh 生成本机 overlay；有版本或配置冲突会明确报告。需要指定客户端时使用 `--client`。Runner 按本机工具注册表发现能力，内置 Codex、Claude Code、dsh、Cursor CLI、Kimi Code、pi 的执行定义；新工具可以复用已有协议驱动注册，本机注册示例见 [custom-acp.json](examples/tools/custom-acp.json)。版本探测不代表已登录或已经完成真实任务验收。

macOS、Windows、Linux 都把对应平台的 `coordica` 可执行文件安装为 stdio MCP，参数为 `mcp --interaction`（默认 setup 创建一个名为 `coordica` 的接入）。在中心“中心配置与接入”生成一次性接入指令，交给执行电脑的 Agent；也可在“运行与节点”创建配对码，另行提供中心 URL 和必要的公共 CA 路径。MCP 自动发现 Git 与工具路径，交给 Runner 配对，并安装/启动用户级后台服务。无需手工执行 `pair` 或 `runner`。每台电脑使用独立节点身份，容量为 1 或 2。

本地探测不读取 Agent 认证文件；已有登录继续由各官方工具使用。用户级后台服务与聊天客户端分离；macOS 使用 launchd、Linux 使用 systemd --user、Windows 使用当前用户的计划任务。服务启动失败会明确报错并保留已配对状态，不伪装成已上线。

原生 CLI 继承启动它的中心或 Runner 父进程中的标准代理环境变量 `HTTPS_PROXY`、`HTTP_PROXY`、`ALL_PROXY`、`NO_PROXY`（也支持小写名称）。可通过 `NO_PROXY` 排除本机地址，避免本地 URL 经过代理。

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

## GitHub PR 集成

项目默认继续使用 Git 模式。可在项目设置的“代码集成”中切换为 GitHub PR；需要项目暂停、执行已停止且不存在待处理候选或未知发布操作。切换发布方式不恢复项目、不改变架构与执行授权。

GitHub 模式为每个任务保留一个 PR。开发自测后，中心构建固定候选、执行预检查，再同步工作分支并创建或更新 PR；独立 Agent 评审与 GitHub 必需 CI 通过后，中心调用 GitHub 合并接口。修复保留任务、累计计数和原 PR，主线变化则重建候选与检查。Git 模式继续使用原来的精确提交发布，不依赖 GitHub API。

中心通过一个专用私密令牌文件连接 GitHub，不借用开发 Runner 的账号。启动参数或可信 host-config 可使用：

```sh
coordica serve --data /private/coordica-center \
  --github-repository owner/repository \
  --github-token-file /private/coordica-secrets/github.token
```

该文件只保存原始 GitHub API token，必须为私密普通文件：Unix 禁止组和其他用户访问，Windows 要求当前用户持有且使用受保护的单用户 ACL。每次读取前复核权限并拒绝 FIFO 等特殊文件。中心可将同一凭据用于绑定到该仓库的 HTTPS Git 传输；已有 `--git-auth-file` 配置仍可单独使用。`host-config` 支持相同 GitHub 参数，统一 MCP 启动可信中心时沿用保存的配置。文件必须与中心 Owner token、交互配置和源码分开存放；令牌不会进入项目配置、导出、Runner 或安装包。Git 凭据在启动时载入，轮换后重启中心。

GitHub 仓库需要启用分支保护：

- 强制必需状态检查并要求分支保持最新，同时约束管理员，避免主线竞态与绕过检查。
- 包含项目设置中填写的完整 CI 检查名称，以及中心上报的 `coordica/review` commit status；该状态的来源应允许中心使用的身份。
- 允许 merge commit。首期不使用 squash、rebase 或 merge queue，以保留已检查的固定候选提交。

专用凭据需要该仓库的 Contents、Pull requests、Commit statuses 写权限，以及 Checks 和分支保护的读取权限。平台不会修改仓库规则或绕过额外的 GitHub 审批要求。已有独立评审及受保护候选的 Owner 批准复用为 `coordica/review`，不会再生成一个 Coordica 批准步骤。

绑定 GitHub App 来源的必需检查须使用带 App 身份的 Check Run。REST 的旧式 commit status 不提供可核验的 App ID，遇到此类绑定会明确提示并阻止合并，由 Owner 调整检查来源配置；不会从发布者名称推断来源或自动放宽规则。

同名的多个检查来源必须全部通过。CI 可来自固定 PR head/base 派生的测试合并提交：先核对其两个父提交，再读取对应检查并复核 PR 未变化；测试合并提交没有对应来源与名称的检查时沿用 head 的结果。每项检查保留提交来源链接，测试合并提交不会替换已批准的候选。发布模式切换复用初始化的静默门禁，待处理候选、未完成发布意图或待撤销评审状态存在时均拒绝切换。

审查池显示 PR、当前提交的 CI、标准 GitHub review 状态和同步时间；同步失败、关闭的 PR、缺少检查、取消或跳过的检查均不视为通过。第三方机器人的纯文本评论不会被推断为批准。项目暂停时同步已有 PR 的只读事实，并撤销尚未合并候选的远端批准；不推分支、创建 PR、修复或合并。需求修订、撤销及授权失效后，旧提交状态的撤销操作持久保留，断线或重启后继续对账；对账完成前不能发布新候选或切换发布配置。同步复用集成 worker 的持久重试时点，没有新增巡检服务。

本功能的自动化验证使用 GitHub HTTP 协议夹具和真实临时 Git 仓库；不等于已完成用户账号、真实 GitHub 规则及多机的部署验收。

升级时新增的 GitHub 适配代码归入受保护架构路径。已有项目会暂停并失效旧架构基线，需重新完成基线确认后才能恢复施工；不会自动派发。

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

项目没有生命周期累计时间上限，中心持续运行；项目 used_seconds / reserved_seconds 是保留全部历史支出的统计与证据，不参与派单、评审、规划、咨询或恢复门禁。兼容记录中的 total_budget_seconds 已弃用并忽略，不要求在创建项目时填写；旧 POST project/budget（ConfigureProjectBudget）明确返回 deprecated。目标预算仍有限（最多 365 天），目标内规划累计额度仍由 PlanningBudgetSeconds（1–31536000 秒且不少于超时，默认十五分钟）约束，单次规划/咨询/初始化超时由 PlanningTimeoutSeconds（1–604800 秒，默认五分钟）约束、重试为 0–2。咨询账户仅统计及预留，不是另一个项目总额度。任务累计额度可配置至 365 天（默认四小时），单个执行 Run 可配置至七天（默认一小时）；新任务自动修复/重试各 0–2，Owner 版本化扩展仍最多 100 次，历史计数不清零。租约续期与有限执行截止时间独立；最低检查仍保留单次一小时超时政策。

旧版本 `hold_reason=budget` 不会自动清除：记录没有保存原始耗尽原因。确认旧进程树已停止并已结算、目标/任务剩余额度为正、当前授权/政策/候选仍有效且重试可用后，Owner 可在任务恢复操作中提交当前任务版本，或调用 `POST /api/v1/tasks/{id}/resume`，正文为 `{ "expected_version": 当前版本 }`，带 Owner 凭据与新的 `Idempotency-Key`。此兼容恢复保留累计支出，失败执行的恢复消耗一次剩余重试；缺失停止证据、真实额度耗尽或过期授权仍拒绝。已有发布意图按原有核对路径处理，不因升级重建。

## One interaction MCP (`coordica`)

Default `coordica setup` previews one user-facing stdio entry named **coordica**; `--apply` writes it. Each physical host shares one startup-fixed Runner directory and one private limited interaction profile across its clients. Local `inspect_environment`, `save_runner_profile` and `initialize_runner` work before enrollment and without a live center. No center service startup, project onboarding interview, worker mailbox, AOCI initialization, model/effort/concurrency settings or resource expansion is provided by this change.

Normal conversation is the interaction role: local bootstrap, redacted project queries, explicit requirement intake and center-host-only advisory Leader conversation. It has no developer Worker, Owner, task creation, approval, scope grant or publication authority. Managed developer Run sessions use the same stdio implementation/server name with a fixed Run/generation and expose **only** `get_context`, `report_block`, `request_scope`; they never inherit interaction tools. Queries never invoke Leader, and only explicitly submitted text is stored.

Successful node registration provisions/reuses a separate node-bound LIMITED chat credential at the startup-fixed shared profile path. A saved registration is retained on partial service/chat failure; `initialize_runner` can retry with the saved center URL and without another pairing code. It does not pair again to finish chat provisioning. A conflicting center/other-node profile is retained, and inactive credentials require explicit human renewal. Bootstrap cannot mint center-host authority.

On the **Owner's center machine**, choose an absolute shared private profile path and create a limited credential through the loopback Owner API:

```sh
coordica chat-init --data /absolute/private/center-data \
  --url http://127.0.0.1:8080 \
  --profile /absolute/private/interaction.json --name "My familiar chat"
```

This reads `CENTER_DATA/owner.token` locally only to mint a limited **center-host** credential. It atomically publishes a private JSON profile and prints only the limited credential ID. The profile contains `center_url`, `credential_id`, a limited `chat_…` token and optionally fixed `tls_ca`; Owner and node tokens are never exported into it. Treat the profile as private and keep it outside product source, packages and Git. Unix mode privacy and Windows current-owner/protected user-only DACL privacy are checked independently; symlinks and non-regular profiles are rejected.

On an **already paired Runner host**, reuse its existing registration, without pairing again:

```sh
coordica chat-init --runner-data /absolute/private/existing-runner-data \
  --profile /absolute/private/interaction.json --name "Shared Runner host"
```

Runner mode reads `runner.json` locally solely for authenticated issuance of a separate limited **node-host** credential; its node token never appears in the chat profile, setup export or output. The server binds scope and NodeID from that authenticated live registration. Source client/session/message identifiers identify a conversation, never host authority. A copied center profile does not grant remote Leader access. No per-agent installation or single-browser-session restriction exists.

When unified interaction initializes a local Runner on the center machine, an existing valid Owner-issued limited center profile is retained after identity validation at its own saved URL/TLS configuration. Its center authority remains separate from the local Runner registration; initialization reads no Owner credential and does not replace the profile with node-chat or send its token to the Runner URL. Invalid, revoked, unscoped or different-node profiles fail explicitly and remain unchanged. `interaction_chat_ready` means the limited chat identity was verified or provisioning completed; it does not prove Runner service activation or model authentication.

Both modes serialize initialization with a process lock, reuse an existing verified active shared profile, and persist a private pending grant identity/envelope before issuance. A lost grant ACK or process restart retries the same key/body rather than minting orphan credentials. An existing revoked/unverifiable profile requires explicit `--renew` after checking connectivity; a mismatched host/center requires a different path. Renewal revokes the existing own credential before replacing its profile. Failed new publication or pending cleanup revokes only the newly issued own credential through the issuing authority. A revoked pending grant requires explicit `--renew` to retire the inactive pending identity before retrying. Old unscoped credentials are rejected and require explicit renewal, with no inferred upgrade. Restore clears credentials, pairings and idempotency; scope metadata cannot reactivate old authority.

Preview the single interaction registration, then apply:

```sh
coordica setup --client codex
coordica setup --client codex --apply
# Explicit stable paths, shared by every client on this host:
coordica setup --client generic --runner-data /absolute/private/runner \
  --chat-profile /absolute/private/interaction.json \
  --output /absolute/private/mcp-export --apply
coordica setup --client dsh --runner-data /absolute/private/runner \
  --chat-profile /absolute/private/interaction.json \
  --output /absolute/private/mcp-export --apply
# Direct stdio launch (normally done by the registered client):
coordica mcp --interaction --runner-data /absolute/private/runner \
  --chat-profile /absolute/private/interaction.json
```

Default paths are `USER_CONFIG_DIR/Coordica/runner` and `USER_CONFIG_DIR/Coordica/interaction.json`. If paths were omitted, setup preserves compatible owned launchers' configured paths. Setup supports `auto`, `claude`, `codex`, `cursor`, `kimi`, `pi`, `dsh`, `generic`; client discovery/version constraints still apply. Preview concretely describes migration: owned legacy `coordica` becomes interaction, and owned matching `coordica-chat` is removed. Ownership requires the selected executable, exact supported launcher identity and matching paths; names alone do not establish ownership. Conflicts leave the configuration unchanged. Client options, trust/disabled state and unrelated entries are retained, with private backups and file comparison before writes. Codex migration edits supported named TOML tables after an official user-scope query; ambiguous/unsupported syntax requires explicit manual migration. Safe unrelated TOML array tables such as `[[skills.config]]` are preserved verbatim; owned or ambiguous array grammar is rejected. Implicit legacy bootstrap Runner paths honor known platform HOME/XDG_CONFIG_HOME/APPDATA semantics from launcher `env`; uncertain identities fail before mutation. Unrelated environment options such as CODEX_HOME remain intact. Claude's known root and per-project MCP name arrays follow the merged name without duplicate Coordica entries; explicit disabling wins over enabling, while unrelated trust/settings values remain intact. Auto setup completes all client preflights and path checks before applying; a failed preflight leaves all selected configs unchanged. Discovery output, the shared chat profile and paired `runner.json` must use distinct paths, including aliases. Configuration fixtures do not establish native host compatibility.

Exports contain only executable and fixed paths, never credentials. Generic/dsh migration replaces only exact generated artifacts and backs up/removes the owned redundant export. For dsh, remove previously loaded legacy overlays from your own launch command; setup cannot remove overlays already loaded by another process or rewrite arbitrary profiles. Keep the executable and shared profile at stable absolute paths and reload/restart the client after setup.

The explicit `setup --mode bootstrap`, `setup --mode chat --chat-profile ABS`, `mcp --bootstrap`, and chat-profile-only `mcp --chat-profile ABS` remain deprecated compatibility paths with their original restricted roles. Existing Run launchers are unchanged. Default setup is the recommended single-entry procedure. All startup modes report server name `coordica`; compatibility chat setup retains its old registration key `coordica-chat` until migration.

Interaction construction and local tool discovery do not load online credentials. Missing profiles may appear later in the same process. Every discovery/call reloads the fixed protected profile and validates current identity, listener audience and application scope. Invalid, unsafe, revoked or unavailable profiles close online capabilities; no Owner or Runner credential substitutes for chat. Capability changes observed on client activity emit `notifications/tools/list_changed`, without a polling timer. Clients without dynamic refresh support must manually invoke `tools/list` or reconnect; automatic refresh is not supported by every host.

Tool discovery reads authenticated server scope, rather than a trusted profile flag. Shared tools are the four overview/list/item/requirement tools; the four Leader conversation tools below are **center-host only**:

| Tool | Structured arguments / behavior |
| --- | --- |
| `get_project_overview` | `{}`; redacted project and goal facts, versions, cumulative used/reserved statistics, query time |
| `list_project_items` | `kind`, `limit` (1–20), optional `cursor` (last returned item ID) |
| `get_project_item` | `kind`, `id`; one redacted central record with version and query time |
| `submit_requirement` | `title`, `original_text`, optional `assistant_summary`, `source`, `idempotency_key`; explicitly adds a requirement to the existing pool |
| `consult_leader` | `expected_goal_version`, `original_text`, optional `assistant_summary`, `source`, optional `reply_to`, `idempotency_key`; queues pure advice |
| `propose_change` | Same arguments as `consult_leader`; relays an architecture/rule suggestion as advisory content |
| `get_consultation` | `id`; reads either a consultation or proposal, including durable status/reply/evidence |
| `cancel_consultation` | `id`, `expected_version`, `idempotency_key`; requests cancellation of this client's own request |

Pages stop at 20 items or a 1 MiB item payload; continue with `next_cursor`. Text is redacted and bounded, task path/reference arrays are limited to 128 entries, and oversized requirement/task text or arrays carry `truncated: true`. Detail uses the same safe projection.

`kind` is the closed enum `requirements`, `tasks`, `blocks`, `agents`, `runs`, `consultations`, `proposals` (tasks, blocks, consultations and proposals are center-host only; node-host credentials allow requirements, own-node agents and own-node Runs). IDs/cursors/session/message identifiers are 1–128 ASCII letters, digits, `_` or `-`. `source` is `{ "session_id": "…", "message_id": "…" }`, with optional `client_id` that must match the authenticated credential. The application supplies the authoritative client ID. Original text is limited to 16 KiB of UTF-8 bytes, assistant summaries to 4 KiB of UTF-8 bytes, and chat titles to 512 bytes and the existing requirement rule of 200 characters. Write keys are required and bounded to 200 bytes; reuse a key when retrying the same operation. The authenticated client/session/message tuple also deduplicates each explicit tool intent even if a retry uses a new key; changed content for the same source is rejected. Use a new message identifier for a new explicit submission. Duplicate and unknown JSON fields are rejected. Requirements retain the original text, summary, and source separately; there is no approval, task creation, or goal rewrite.

For example, an explicit consultation tool call can use:

```json
{
  "expected_goal_version": 0,
  "original_text": "Please advise on a simple module boundary before I draft the goal.",
  "assistant_summary": "User wants advisory architecture discussion.",
  "source": { "session_id": "chat_session_1", "message_id": "user_message_17" },
  "idempotency_key": "consult_user_message_17"
}
```

Use goal version **0 only when no goal exists**; otherwise use the current overview's goal version. Long Leader work returns a durable request ID/status immediately. A center-host interaction agent may query `get_consultation` whenever it chooses; a submission acknowledgment is not a reply or completion. Replies may be stale after goal edits. Conversations are private to the authenticated ClientID; reply_to must also match session and compatible goal version. The Owner Web console may view all. An advisory prompt preserves the current question first, then up to eight authorized ancestral questions/replies, then incremental shared project facts; the final encoded prompt is at most 256 KiB and reports omitted context. It never imports general/private planning messages. Periodic querying is an individual chat-agent capability; Coordica adds no chat timer, monitor or mandatory polling workflow. The Web Planning/Leader page displays the same central requests, original text, separate summaries, source identifiers, versions, statuses, replies, and result digests, and provides **纯咨询 Leader（不派生任务）** and a separate architecture/rule proposal action. Existing Evaluate and send-for-evaluation remain execution planning operations that can derive tasks.

Consultations persist `PlanningRun.purpose = "consultation"`; an empty legacy purpose remains evaluation. Native execution stays explicit `codex --model gpt-6.1-sol`, read-only, isolated from Owner settings/credentials. The advisory schema accepts only `{ "reply": "…" }`; action fields are rejected. No completion, receipt replay, reconciliation, or recovery path adopts a consultation as a plan. Automation being enabled does not change this. Formal adoption still requires the existing Owner versioned save/authorization/decision flows. A proposal never saves a goal or holds/stops active development.

Center-host consultation is available before admission and without a goal. The durable consultation account is a cumulative statistics/reservation target independent of goal edits; its legacy budget field is ignored and spending is never reset. Every consultation attempt has the finite planning timeout (default 300 seconds) and bounded retry count (default two). Project lifetime spending never prevents consulting. The supervised worker slot, leases, generations, process-start evidence, private durable receipts, cancellation, project pause, credential scope/revocation and restart fences apply. An unknown-live consultation consumes its full grant and blocks further process admission; prose is never stop evidence. A delayed exact same-Run/purpose/account/request/generation/document/authorization trusted TreeStopped receipt can release only that barrier, without adopting its late result or refunding historical charges; duplicate recovery changes nothing. The Owner can also resolve it after independently proving the process tree stopped:

```sh
# stop-proof.json: {"expected_version": CURRENT_RUN_VERSION, "evidence": "Actual operator stop evidence"}
coordica ctl --center http://127.0.0.1:8080 --token-file /absolute/private/center-data/owner.token \
  --method POST --path consultation-runs/PLAN_ID/confirm-stopped --body-file /absolute/private/stop-proof.json
# Revoke a limited chat credential (also cancels its queued/running advisory work):
coordica ctl --center http://127.0.0.1:8080 --token-file /absolute/private/center-data/owner.token \
  --method POST --path chat-credentials/CLIENT_ID/revoke --key revoke-CLIENT_ID
```

HTTP chat routes are precisely GET `/api/v1/chat/{overview,identity}` and bounded JSON POST `/api/v1/chat/{items,item,requirements,consultations,proposals,consultation,cancel}`. The payloads match domain contracts (HTTP idempotency is a header). Owner-only additions are POST `/api/v1/chat-credentials`, POST `/api/v1/chat-credentials/{id}/revoke`, POST `/api/v1/planning/{consult,propose,consult-cancel}` and POST `/api/v1/consultation-runs/{id}/confirm-stopped`. AdminHandler accepts center-chat; shared loopback Handler supports both scopes. RunnerHandler on remote TLS accepts **only node-chat on every chat route**, including overview/items/item; a valid center-chat token is rejected there. The remote listener exposes no Owner console/API or direct Leader work. Its bounded paired-node POST `/api/v1/runner/chat-credentials` and POST `/api/v1/runner/chat-credentials/{id}/revoke` issue/revoke only credentials bound to that node; issuance replay also rechecks the cached granted credential is active and bound. Revocation/live node registration and persisted scope are rechecked transactionally before reads/writes/replay and by the Leader worker through start, watch, settlement and recovery.

Node-chat can submit ordinary requirements into the shared pool and read safe shared project/requirement/task/block facts. The server stamps SourceNodeID alongside chat client/session/message provenance. Normal authorized planning may later adopt pool requirements; this is not direct Leader chat. Runtime Run/agent queries on a Runner host expose only its authenticated node's safe facts: Run ID/state/tool/timings/limits/quiet status and truthful observed capabilities/LastSeen. No command, machine path, SDK session, private output, candidate content or full Project object is exposed. A historical Run cannot reveal its task's later assignment on another node or updated goal; task title/text accompanies runtime facts only while that exact current Run and authority remain bound. There is **no live developer messaging or runtime mailbox** in this implementation. Redirects and remote plaintext HTTP are refused.

Chat DTOs exclude repository/local paths, raw command lines, executable definitions, secrets, credentials, raw process outcomes, and Owner configuration. Task scopes contain only safe relative repository patterns. Natural-language fields receive token/path/command redaction; do not intentionally submit unrelated secrets as requirements or advisory context. Revocation is rechecked transactionally before reads and before idempotency replay. Backups retain advisory records/accounting and supervised receipts under the existing private backup policy while removing credentials and cached credential responses. Public JSON schema/TypeScript and the embedded advisory output schema are generated with `go run ./cmd/contractgen`.

### 分层执行上下文

Leader 的普通规划与需求影响分析会携带精确的已确认共享基线、目标版本、权限、任务索引及累计用量。长任务正文和同目标版本历史保存在每次只读执行独立的 `context/*.jsonl`，清单记录 Run、代际、基线、需求修订、文件摘要与记录数，可按需读取；旧 Leader 输出和原生会话标识不会重复充当目标规则。资料单文件上限 4 MiB，规划提示按 JSON 编码后的实际字节限制在 256 KiB；历史省略会标明，完整权限、验收和需求影响集合超限时明确阻断。

开发与审查任务收到关联需求的固定修订正文，不继承其他 Agent 的对话或私有经验。源码按需读取自固定私有工作区，审查仍须覆盖固定候选的完整差异。身份、共享基线或需求版本变化后，不能把新合同交给旧 Run。只读 Leader 资料复用既有进程退出和临时目录回收流程；无法确认进程停止时保留现场和原有阻断。

手动升级时先升级中心，再升级 Runner。中心只向明确声明支持当前固定需求上下文协议的 Runner 派发关联需求的开发和审查任务；旧 Runner 仍可报告在线状态，在升级前不会领取这些任务或占用其预算。新版 Runner 遇到关联需求缺少正文的旧中心会明确拒绝执行，避免仅凭需求 ID 猜测合同；这不自动升级、启动或批准任何项目。
