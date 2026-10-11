# Runner 与受管 Agent

Runner 是独立 Go 进程，配对后不依赖浏览器常开。它从中心领取有限租约，在每个 Run 的独立 Git 工作区执行工具，提交固定对象和结果；中心负责授权、验收、候选版本、集成和发布。

## 运行方式

中心和 Runner 都需要 Git。Runner 所在电脑另行安装并登录要使用的官方工具；Coordica 不安装工具、不索取模型账号密码。中心及 Runner 本身不需要 Node.js；dsh 的官方发行方式可能需要本机 Node.js，Web 源码构建也需要 Node.js。构建后的 Web 静态资源由 Go 中心提供。

macOS、Windows、Linux 的主要接入方式统一为：安装 `coordica mcp --bootstrap`，向客户端下达初始化指令，提供中心 URL、一次性配对码和必要的公共 CA 路径。MCP 自动探测本机环境、配对并启动用户级 Runner 服务，流程见 [统一初始化指南](windows-onboarding.md)。Runner 独立于 MCP 和聊天客户端运行。

以下命令保留为前台诊断入口。局域网使用 HTTPS，证书应覆盖实际使用的主机名/IP；把中心证书/CA 文件复制到 Runner 电脑：

```sh
./bin/coordica doctor
./bin/coordica pair --center https://192.168.1.10:8443 --ca center.crt --data .coordica-runner --code-file pairing.code --name laptop --capacity 2
./bin/coordica runner --data .coordica-runner
```

Windows 使用 `coordica.exe` 运行相同子命令。`doctor`、`pair` 和 `mcp --bootstrap` 可用可重复的 `--tool-executable ID=PATH` 指定已注册工具的实际可执行文件，用 `--tools-file` 加载可信本机定义；未指定时按注册候选从 PATH 查找。旧 `pair --codex/--claude/--dsh` 参数保留兼容。只支持 `capacity=1` 或 `2`，实现者和评审者共同占用并发槽。一次性码也可从标准输入读取。配对后持久保存节点 ID、节点令牌、中心地址和 CA 配置；重新启动使用同一数据目录。HTTP 只允许 loopback 本机开发，LAN 地址不会降级为明文，也没有跳过证书验证选项。

每个数据目录只有一个 Runner 所有者。中心同样使用独占文件锁；运行中不要删除锁文件。模型工具必须在实际运行 Runner 的用户及环境中能够找到并完成登录；后台服务的 PATH/HOME 可能与交互式终端不同。

初始化只保存 MCP 进程的 PATH 和探测到的注册工具路径，不整包导出环境变量或模型凭据。后台 Runner 在创建 Git/Agent 客户端之前恢复这个 PATH，使 Git 和 npm 工具的 Node shebang 使用同一查找路径。首次服务启动失败后，修好本机环境并再次初始化会复用同中心身份；Runner 未运行时刷新 PATH、工具路径及本次提供并校验的公共 CA，保留节点令牌、名称、容量和待回传 spool。Runner 数据目录仍被占用时，返回 `directory_in_use`，保持配置与服务不变，停止当前执行后再重试；不会由初始化偷偷重启正在工作的进程。

用户级服务按 OS 生成固定配置，只运行当前 Coordica 可执行文件的 `runner --data`，不接受 MCP 提供的 shell 命令或其他程序。macOS 使用当前登录用户的 LaunchAgent；Windows 使用当前用户、最低权限、交互登录令牌的计划任务。二者面向已登录会话，不承诺注销后继续运行。Linux 使用 `systemd --user` 并尊重 `XDG_CONFIG_HOME`，要求可用的用户服务管理器及 user bus；无 systemd 或会话环境不可用时明确失败，首版不自动提权或开启 linger。Windows 的服务程序与数据目录路径若包含 `%` 会明确拒绝，因为计划任务会展开环境变量；空格和中文路径受支持。

服务结果 `started` 仅代表管理器报告活动，`activation_requested` 仅代表启动请求已接受；它们都不能证明中心连接、工具登录或双机验收完成。服务生成、路径转义、安装失败和无损重试采用 fixture 测试，本机测试不会真实安装服务。原生三 OS 的登录、注销、重启和实际中心连接仍需各自验证。

## 工具能力

`doctor` 与注册/领取前的探测只运行 `--version`，不会调用模型或验证账号余额。`available=true` 代表找到工具并取得版本；`protocol/structured/resume/cancel/review/read_only` 描述本机注册与驱动能力，不能据此宣称该 OS × 工具版本已经完成真实任务验收。工具升级后需要重新运行真实验收。中心接收工具版本和能力详情，Runner 的本机日志会明确显示工具缺失、能力降级及请求错误。

| 工具 | 结构化接口 | 审批/权限策略 | 恢复与取消边界 |
|---|---|---|---|
| Codex | `codex exec --json`，提示词经 stdin 输入 | `--ask-for-approval never`；实现使用 `workspace-write`、评审使用 `read-only` | 适配层支持精确 session 的 `exec resume`；通用进程监督负责取消，不假设存在可持久恢复的运行中进程 |
| Claude Code | `claude -p --output-format stream-json --verbose`，提示词经 stdin 输入 | `dontAsk` + 显式工具列表；评审仅 `Read,Glob,Grep`；不使用无条件 bypass | 适配层支持精确 `--resume`；取消使用 OS 监督。原生 Windows 的 Claude 内置 sandbox 能力与 macOS/Linux 不相同 |
| dsh | `dsh --profile acp`，共享 ACP stdio 驱动 | typed read/search/edit 请求校验实际路径与工具许可；opaque / 无法验证的请求拒绝 | `session/resume` 仅在声明支持时使用；cancel 后有限等待并确认进程树；close 需能力声明 |
| Cursor CLI | `agent acp` 或 `cursor-agent acp`，共享 ACP 驱动 | 验证已有 cursor_login；评审要求 advertised ask mode；可验证权限按固定目录约束 | load 需能力声明；没有 close 时用 EOF 和有限停止 |
| Kimi Code | `kimi acp`，共享 ACP 驱动 | 新旧版本均不默认开启 yolo；不可验证权限拒绝，实际版本可能因此阻塞 | load/close 均按服务能力；协议 cancel 与受管进程监督结合 |
| pi | `pi --mode rpc`，共享 JSONL 进程通道；要求 ≥1.1.0 | 评审移除写入/执行工具、extensions、skills、prompt templates 和 MCP | ACK、agent_end 不代表成功；settled 后核对 get_state idle/队列空、stopReason=stop；clear_queue + abort + 有限停止；resume 仅允许同一规范工作目录 |

工具定义和扩展方式见 [工具注册](tool-registry.md)。Cursor、Kimi Code、pi 的协议实现及故障 fixture 可测试，但尚未完成各自真实模型和 OS 组合的验收。ACP 的路径验证是批准单次可验证操作的条件，私人开发环境允许修改已经发生；最终集成仍以真实 Git diff 判断范围。

适配器只从 stdout 消费官方结构化协议。stderr 会被排空，不能成为结构化结果、评审结论或授权；终端显示文本不参与协议解析。退出码 0 仍必须具备有效会话及明确成功的协议终态。Codex 的 `error` 重连诊断本身不会否定随后成功的 `turn.completed`；`turn.failed` 或缺失成功终态仍失败。未知结果、错误终态、错误 JSON-RPC 回复 ID、协议输出超限均失败。

Runner 每个 Run 新建 Agent 会话，包括返修及主线移位后的重新评审。接口具有 resume 能力不等于首版自动恢复旧会话；重启首先核对旧 Run 和进程，保留证据，由中心安排后续执行。这样不会把已失效的上下文和授权带入新一轮评审。

dsh 的适配协议和测试已实现，当前开发电脑尚未安装 dsh，因此没有真实 dsh 开发任务的验收证据。若已安装版本请求了无法验证的工具权限，该步骤会明确拒绝，实际能力随该版本记录降级。

## 租约、停止及恢复

Runner 以中心 HTTP `Date` 计算本地单调时钟截止，扣除时间精度和往返误差，不假设两台电脑的系统时间一致。心跳最多每 10 秒一次；本地 watchdog 每 100ms 检查租约、Run 硬期限和 30 秒成功心跳间隔。任一到期就停止，不会等待挂起的网络请求返回。中心撤销、取消或返回 401/403/409 也停止。

macOS/Linux 用独立进程组 TERM → 有限等待 → KILL，随后确认普通后代已退出并排空管道。正常退出的父进程留下普通子进程时也清理。Linux 把不再执行的僵尸与活跃进程区分。Windows 在启动时挂起进程，先分配到不可继承、kill-on-close 的 Job，再恢复主线程；Job 分配失败时杀死尚未运行的进程并报错，不回退到无监督执行。通用 Windows 取消使用 Job 终止，具体工具的协议取消单独实现。

普通进程树和管道读写都有有限停止路径。无法证实终止时返回 `quiescent=false` 并保留记录；旧进程仍占本机并发槽。Unix 主动脱离进程组的进程不在此保证内；Runner 被强杀后，旧进程可能还在，重启会核对已记录的进程组而不会伪造已停止。PID 缺失、权限不足或身份复用造成的不确定性会保留为未知状态，需结合本机进程核对后由 owner 处理。Windows owner 退出会关闭 Job handle 并触发普通子进程终止，仍须原生机器验收。

领取请求在 `claim.json` 持久保存相同请求及幂等 key；ACK 丢失、进程重启后也重放同一请求。每个 Run 的完成请求在 `spool/` 持久保存，固定内容和 key 后重试；ACK 未确认之前不删除。中心收到结果不等于本机进程已停止，已 ACK 的未知停止记录仍保留。工作目录及未提交修改保留用于诊断，不自动清理。

启动恢复需要能够连接中心核对旧 Run；无法核对时显式报错并保留数据，恢复中心连接后可重启。收到已撤销节点的 401/403 后退出并提示重新处理节点身份。

## Git 与评审

输入来自认证中心同源的完整 Git bundle；Runner 在独立目录 checkout 固定 `Run.Base`，不从本机其他项目复制工作树，不跟随任意下载重定向。bundle 最多 64MiB。每个 Run 目录支持空格及非 ASCII 路径。

本机注册的 `commit_mode` 明确分工：空值或 `agent` 要求实现者自行提交有意修改，Runner 读取真实 HEAD 并验证干净；`runner`（内置 ACP 工具）由 Runner 在进程树已静止、许可仍有效后，将独立 Run 工作区的完整修改制成快照。后者先用临时 index 验证真实全部差异、任务范围、实体文件及对象类型，拒绝 ignored、symlink 和越界，才建立提交并 CAS 更新该受管工作区的 HEAD / index。前置验证失败保留私人文件且不改变原 HEAD / index；成功也不操作用户原项目的工作区。评审永不自动生成提交。两种模式最终产物都是相对固定 base 的 bundle；中心仍独立核验范围和门禁。Runner 不更新项目主线、不 push、不拥有中心发布授权。

评审前通过受控 Git 获取精确 `Candidate.Main → Candidate.Commit` 的完整变化清单和 patch，注入评审提示词；因此只读 Claude 也能查看实际差异。评审输入累计超过 1MiB、固定对象缺失或无法取得完整差异时，返回明确的 `blocked` Review，不截断差异。评审后验证 HEAD 及工作区仍是固定候选。

模型结论必须是单个严格 JSON 对象，包含匹配候选的 `candidate`、`verdict`（`approve`、`changes_requested`、`blocked`）、非空 `summary`、字符串数组 `findings`、空 `run_id`。不接受 Markdown、未知字段、第二个对象、坏尾文本或其他候选。自然语言报告、执行成功及模型声称已通过均不能代替中心集成门禁。

## 本机证据与凭据

`runner.json`、spool、claim 及每个 Run 同级的 `events.jsonl` 均私有保存；Unix 模式 0600，Windows 设置 protected owner DACL。写入持久状态采用临时文件、fsync 和原子替换。结构化事件日志最多 4MiB，溢出写出明确 `runner.log_truncated` 标记，不留下半条 JSON。最终输出保留最多 64KiB 并记录 `output_truncated`；失败详情附最多 2KiB 脱敏输出，便于显示申请范围或阻塞原因。

本机日志保存已清洗的结构化 stdout 事件、最终结果、完成状态；原始 stderr 不作为协议保存。日志不整包上传到中心，中心证据是版本化 API 的固定完成请求、评审及产物对象。结构化敏感字段和已知环境令牌/API key、节点令牌会脱敏；无法识别的仓库私密内容仍可能出现在模型输出，因此日志按私人开发资料处理。

节点令牌只用于同源中心 HTTP 请求，不传给模型子进程。子进程环境移除 GitHub/GitLab 发布令牌、中心授权变量、SSH agent 和继承的 Git 配置；保留官方模型账号需要的环境，Git 使用无全局配置的默认环境。这里是私人开发权限边界：拥有本机用户权限的工具仍可能主动读取 HOME 现有凭据。远端“仅本人能上传/合并”仍需要远端账号/权限规则及发布凭据隔离，不能由本机提示词或环境过滤单独证明。

## 验证证据

普通测试不调用模型：真实子进程及普通后代、TERM 升级 KILL、父进程先退出仍持管道、JSONL 全量排空、stdout 超限、stdin 塞满后的 ACP 取消、结构化成功/失败、精确 resume、拒绝 opaque 权限、TLS/同源令牌、领取和完成 ACK 丢失重放、租约/撤销停止、严格评审 JSON、完整 diff/超限、私有证据上限、未知进程保留容量、未提交工作区拒绝回传。

开发环境已在 macOS arm64 运行这些模块的 race 测试及 vet，并交叉编译 Windows/Linux amd64 Runner 测试二进制。交叉编译仅证明构建通过，不证明原生 OS 行为、后台部署或真实工具能力。原生 Windows/Linux 的 Job/进程组、取消、重启、权限 ACL，以及第二台电脑 LAN 连接仍需真实机器验证。

真实模型 smoke 默认跳过。明确选择工具后在临时 Git 目录执行一次 60 秒以内的只输出 JSON 任务，检查结构化结果、session 和已停止进程树；该测试可能消耗模型额度：

```sh
COORDICA_LIVE_AGENT=codex go test ./internal/agent -run '^TestLiveAgentSmoke$' -v
COORDICA_LIVE_AGENT=claude go test ./internal/agent -run '^TestLiveAgentSmoke$' -v
```

2026-10-11 的本机 smoke：Claude Code `2.1.114` 简短任务通过；Codex `0.162.0-alpha.17.2` 返回 `Reconnecting... 2/5 (request timed out)`，60 秒内未成功完成，属于服务连接超时，测试失败但确认进程树停止，尚不能视为真实 Codex 任务通过；dsh 未安装。简单 JSON smoke 也不代表实现/提交/独立评审的完整任务验收。发布认证应记录 OS、架构、实际工具版本和测试日期，并完成真实多工具开发任务、断网、重启、旧租约、主线变化和预算耗尽场景。
