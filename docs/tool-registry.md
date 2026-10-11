# 工具注册与能力发现

Runner 在本机建立工具注册表，中心只接收工具 ID、版本与声明能力。模型自然语言是任务输入；执行、取消、恢复和完成采用结构化协议。发现一个 CLI 不等于已认证或在该系统上验收成功。

## 内置注册与共享驱动

| 工具 ID | 本机候选命令 | 启动方式 / 协议 | 特殊条件 |
|---|---|---|---|
| codex | codex | exec --json / codex-jsonl | 成功需要 turn.completed；开发与评审使用不同 sandbox |
| claude | claude | print stream-json / claude-stream-json | 成功需要有效 result；评审仅 read/glob/grep |
| dsh | dsh | --profile acp / acp | session/resume、close 按 initialize 能力 |
| cursor | cursor-agent、agent | acp / acp | 官方 Cursor CLI；IDE 的 cursor 命令不适用；评审需 ask mode |
| kimi | kimi | acp / acp | 官方 Kimi Code CLI；按已安装版本能力处理 load/close |
| pi | pi | --mode rpc / pi-rpc | ≥1.1.0；settled 后查询状态，不能以 ACK 或 agent_end 判成功 |

内置定义集中于 `internal/agent/registry.go`。协议驱动不按工具品牌切换；任务领域、调度、Runner/MCP 探测与 Web 没有 Agent 品牌白名单。客户端 MCP 文件格式仍由独立 mcpconfig 模块适配，因为不同客户端的用户配置格式尚未统一。

## 注册新的同协议工具

复制 `examples/tools/custom-acp.json` 到本机固定目录，按该 CLI 官方文档修改 executable 与固定参数。示例注册 ID 是 `local-acp`；它只是待填写的配置示例，不会默认启用或下载工具。

```sh
coordica doctor --tools-file /固定目录/tools.json
coordica doctor --tools-file /固定目录/tools.json --tool-executable local-acp=/绝对路径/my-acp-agent
coordica setup --apply --tools-file /固定目录/tools.json
```

Windows 示例：

```powershell
& C:\Coordica\coordica.exe doctor --tools-file C:\Coordica\tools.json --tool-executable 'local-acp=C:\Tools\my-acp-agent.exe'
& C:\Coordica\coordica.exe setup --apply --tools-file C:\Coordica\tools.json
```

`--tools-file` 是 JSON 数组；不含密钥、中心凭据或 shell 表达式。ID 和别名必须唯一，形如小写字母起头的 `a-z0-9_-`，最长 64 字符。不可替换内置身份。任务 `tool` 使用节点报告的 canonical ID；本机启动和 executable override 可用别名，但中心不会将别名自动改为 canonical ID。注册定义与已发现的 executable 路径随 Runner 配置持久保存；停止后重新初始化会保留自定义注册。启动 bootstrap 时指定新的文件才显式更新注册。

启动参数是逐项 argv，唯一支持的模板为 `{session}` 和 `{allowed_tools}`；不会经 shell 展开。支持四种已实现协议。尚未支持的协议会拒绝注册，需要增加驱动与协议测试。恢复和 ACP 方法必须明确声明，不推测支持。pi resume 仅接受 session 文件头记录的规范 cwd 与本次固定工作目录相同，跨目录明确拒绝；Runner 正常重跑使用新会话。`cancel=true` 的承诺包括受管进程有限停止；ACP 的协议 cancel 不能代替进程树结束证明。

注册文件由使用者掌握，是可信本机执行配置。不要允许开发任务修改它，也不要把不熟悉的工具/参数直接列为可调度。中心及 MCP 调用只能选择已注册身份，不能传入 executable、argv 或修改注册表。自定义 executable 路径可用诊断 `pair` / `mcp --bootstrap` 的可重复 `--tool-executable ID=PATH` 固定；正常初始化自动保存已发现路径。

`commit_mode` 是可信本机注册中的提交分工：默认 `agent` 保留原有自行提交要求；`runner` 允许文件编辑型工具交由 Runner 在进程停止后严格校验完整范围并生成私有 Git 快照。内置 dsh/Cursor/Kimi 采用后者，不能因为提交需要而批准未验证 shell。它不授予主线发布权限；实现细节见 [Runner](runner.md)。

## 能力、调度与审批

版本探测返回 protocol、structured、resume、cancel、review、read_only 等声明能力；initialize / session 协商仍会验证目标版本实际支持的恢复和评审模式。实现领取需 available、structured、cancel，评审另需 review，优先该节点上与实现不同的可用工具。旧 Runner 未报告的新字段不会被默认置 true，升级后重新报告才可领取新活动；旧 Run 仍按已有代次和租约核对。此开发版需中心与 Runner 一起升级，旧中心的严格 JSON 解码不接受新能力字段。

ACP 的权限请求只有带明确 kind 和真实路径、能映射到固定工作目录内的 read/search/edit 与允许工具时才批准一次；缺少位置、路径越界、symlink 指向外部、opaque 标题或未经验证的命令不构成授权。Cursor 验证已有 CLI 登录；不自动开始交互登录。Kimi 不启用 yolo。同一会话此前的结构化 tool_call 元信息可用于补齐相同 ID 的部分权限请求，冲突或缺少可信元信息仍拒绝。某工具版本若只发 opaque 权限，普通开发可能被拒绝，应记录该版本缺口，不能宣称全部能力已验收。

Kimi 当前上游可能将部分内部失败映射为 ACP end_turn，协议侧无法独立区分这类失败。end_turn 仅是可观察的协议终态；真实产物、独立评审及中心检查仍必须满足，不把一次正常结束宣称为需求已经完成。

read_only 描述注册的评审限制，不统一承诺 OS 级沙箱。评审采用新会话、固定候选、只读工具或模式，执行后还核对真实工作树；开发最终范围以中心真实 Git diff 校验。私人电脑的用户权限不属于绝对隔离边界。

## 官方协议依据

- [Cursor ACP](https://cursor.com/docs/cli/acp)：官方 CLI、认证和会话。
- [Kimi Code ACP](https://www.kimi.com/code/docs/en/kimi-code-cli/reference/kimi-acp.html)：结构化接入与版本差异。
- [ACP 会话设置](https://agentclientprotocol.com/protocol/v1/session-setup)：能力协商与会话方法。
- [pi RPC](https://pi.dev/docs/latest/rpc)、[RPC 命令](https://pi.dev/docs/latest/rpc-commands)：settled、队列、状态及取消。

真实账号、目标系统进程终止和多电脑协作的证据见 [实施记录](implementation-status.md)。
