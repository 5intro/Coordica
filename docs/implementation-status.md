# 阶段 A 实施与验证记录

用户于 2026-10-11 授权实施，要求模块高内聚、低耦合、可维护；同日补充三系统统一 MCP 初始化入口。当前为 `0.1.1-dev`，代码可构建，尚未完成所有发布验收。

## 已实现的模块

| 模块 | 职责 | 不能承担的职责 |
|---|---|---|
| domain | 版本化记录、纯校验 | SQL、HTTP、进程执行 |
| store | SQLite 事务、迁移、一致快照 | 授权政策、进程执行 |
| app | 授权、状态机、预算、租约、候选集成、恢复 | Agent 协议、HTML |
| httpapi / MCP / CLI | 解码、认证传输、入口映射 | 复制业务规则 |
| gitops | 固定 Git 对象、范围、候选、条件更新 | 任务状态、SQL、HTTP |
| process / filelock / localfile | 进程监督、互斥、私密文件和原子替换 | 调度和模型提示词 |
| agent | 本机工具注册、能力发现与四种共享协议驱动 | 授权和集成放行 |
| runner | 有限租约执行、材料交接、持久重放 | 主线发布、中心授权 |
| onboarding / nodeservice | 节点初始化与各系统用户级服务 | 任务批准、模型协议 |
| web | 人工操作和状态展示 | 成为门禁的可信来源 |

中心为 Go 模块化单体，使用 SQLite；React/TypeScript 构建产物嵌入中心。没有 Redis、独立消息队列或通用编排框架。初始化 MCP 和 Run 业务 MCP 权限分开，配置后的 Runner 独立运行。

## 已有验证证据

- macOS arm64 上的最终 `go test -race ./...` 全部通过，覆盖中心、真实本地 Git、Runner、适配器协议、进程监督、备份及初始化；`go vet ./...` 通过。
- Web 生产构建通过，11 项测试覆盖幂等重试、轮询竞态、断线状态、精确版本批准和恢复/预算交互。Safari 中使用隔离测试中心实际完成登录、项目创建、需求创建及关联草稿任务保存，随后退出并关闭测试服务。
- 真实 Git 集成测试覆盖代码 bundle 交接、固定双父候选 C、新 Run 独立评审、程序检查和精确 C 条件发布；模型步骤使用结构化替身，不能据此宣称真实多工具开发已验收。
- 主线变化会清空旧评审/检查并重建候选；越界材料拒绝集成；丢失发布 ACK 后，远端新提交包含精确 C 时核对为成功。
- 并发领取、重复请求、旧代次/租约、失联占槽、暂停/取消竞态、预算结算、撤销、完成消息重放均有故障测试。
- macOS 实际父/子/孙进程测试覆盖 TERM/KILL、退出确认、管道排空和输出上限；Windows Job 和 Linux 分支已交叉编译，不能代替对应系统运行。
- SQLite/Git 材料闭包备份可恢复；损坏摘要拒绝恢复；秘密记录从快照删除并 VACUUM，测试检查原始字节没有测试凭据。恢复撤销旧节点并暂停待核对工作。

## 真实 Agent 与平台矩阵

| 项目 | 证据 | 当前结论 |
|---|---|---|
| macOS arm64 / Claude Code 2.1.114 | 真实非交互结构化调用成功，约 3.09 秒，进程树结束 | 简单模型调用通过；完整开发协作未验收 |
| macOS arm64 / Codex 0.162.0-alpha.17.2 | 两次调用在 60 秒截止前仅建立线程并报告服务重连/请求超时；取消后进程树结束 | 外部服务连接阻塞；没有成功任务证据 |
| macOS / dsh | 本机未安装 | 未实测 |
| Windows / dsh | 用户确认第二台电脑已安装并登录 | 等待该机 MCP 初始化和真实运行 |
| Windows amd64 / Linux amd64 | Go 应用与相关平台代码交叉编译；已启用 [GitHub 托管机器原生 CI](https://github.com/5intro/Coordica/actions) | 编译通过；各提交的原生测试结果见 CI；用户电脑上的部署、服务管理及模型执行未验收 |
| macOS / Linux / Windows 用户级服务 | 配置、路径转义、重试与失败注入测试 | 不在开发机自动安装服务；真实安装/登录生命周期待实测 |

`doctor` / MCP 版本探测只证明工具可找到并返回版本，不读取认证文件，也不证明已登录或能完成模型任务。`activation_requested` 只表示操作系统接受启动请求；以中心节点心跳确认上线。

## 仍需完成的发布验收

1. 在用户 Windows 电脑通过 MCP 初始化，确认节点心跳、dsh 实际接任务、交接固定材料并通过相同门禁；在该电脑核对进程终止与真实用户服务行为。托管 CI 的进程测试不替代目标电脑部署验收。
2. 恢复 Codex 服务连接后，完成 Codex 与 Claude Code 跨两台电脑的真实功能任务，包含独立评审、程序检查和精确候选集成。
3. 在 Linux 原生环境验证 systemd 用户服务、含空格/中文路径、子进程终止、断线与重启恢复；macOS 也需要真实用户服务安装验证。
4. GitHub 仓库已创建于 [5intro/Coordica](https://github.com/5intro/Coordica)，用于源码及开发包分发；通过 Coordica 中心专用凭据完成真实 GitHub 候选集成的部署验收仍待进行。仓库发布不等于中心运行闭环已验收。
5. 三系统、各工具组合分别保留结果；不能用某一系统的协议替身和交叉编译覆盖其他组合。

公网中转、阶段 B 自动计划与系统级自动批准不在本次实现中。Git 合并不会自动升级正在运行的中心或 Runner。

## 初始化增量最终检查

2026-10-11 完成：

- 8 项 Bootstrap MCP fixture 测试覆盖自动探测、固定本机配置、严格参数/重复字段、合法客户端元信息、秘密脱敏以及完整/部分失败结果。
- onboarding/nodeservice 测试覆盖先配对后激活、启动失败重试、不重复注册、停止后刷新 PATH/工具/有效 CA、身份/容量/spool 保留、占用时完全不调用服务管理器、非法 CA 不覆盖旧配置、三平台配置转义、Linux 用户配置目录及 Windows 环境变量路径拒绝。
- 最终全量 Go race、Go vet、契约生成和 Web TypeScript 检查通过。当前 macOS arm64、Windows amd64、Linux amd64 应用重新构建通过。
- 重新构建的 macOS 二进制完成真实 stdio MCP 握手、工具列表及 `inspect_environment`；Git、Codex、Claude 版本可探测，dsh 未发现。未执行 `initialize_runner` 的真实服务安装。
- Windows batch 命令纯构造/拒绝测试通过；Windows amd64/arm64 进程测试及 Windows/Linux amd64 初始化测试已交叉编译。原生 stdin、退出码、取消和服务启动仍须目标机器执行。

测试包位于本地 `bin/`，通过 [GitHub Releases](https://github.com/5intro/Coordica/releases) 分发，附初始化指南、模块文档、许可、第三方声明及 SHA-256 清单。它们是开发预览包，不是三系统、三工具均完成实际部署验收的稳定版本。

## 首次远端 CI

首次 GitHub 托管 CI 的 macOS/Linux 全套检查通过，Windows 的进程、Runner、MCP、初始化及服务配置测试通过，但备份与候选构建触发 Git for Windows 默认的长路径限制。后续在统一 Git 边界修复了命令配置、本地传输子进程与 clone 路径处理，Windows 原有集成、备份和恢复测试随后全部通过。

额外的超深目录回归还确认了 Git 自身的目录字节限制以及 Windows 子进程工作目录的系统限制。实现对规范化后超过 215 个 UTF-8 字节的仓库根路径，或超过 258 个 UTF-16 单位的工作目录，在创建/启动前明确拒绝并提示缩短数据目录；215 字节为 Git 的独立上限预留 `/.git`，规范化会展开已有祖先的短文件名和符号链接。测试验证超限拒绝前不创建目标，同时保留约 200 字节工作目录内超过 260 字符的对象与引用路径的真实 clone/fetch/备份回归；Unix 继续验证超过 280 字符的工作目录。最终提交的完整检查状态以 [Actions](https://github.com/5intro/Coordica/actions) 和对应预发布说明为准。

上述修复的 [三系统原生 CI](https://github.com/5intro/Coordica/actions/runs/38106557395) 已全部通过，提交为 `2db808258e0a03cdcc2ca0448c96b51fa695a28b`。这验证了托管环境中的测试和构建；用户级服务部署、真实 Agent 调用与双机验收仍按前述清单推进。

## 客户端 MCP 配置与任务执行适配

新增 `coordica setup` 预览、`setup --apply` 应用的统一客户端注册入口。客户端格式及支持条件见 [接入说明](mcp-clients.md)。配置模块与 bootstrap、Runner 和任务状态机分离，不包含中心凭据，不会自动配对或启动服务。已有配置保留、同名冲突、重复执行、损坏文件和注册命令失败纳入局部回归。

Claude Code、Codex、Cursor、Kimi Code、pi 的 MCP 配置支持不等于这些客户端在各系统上均已实测；Cursor、Kimi Code、pi 的 Runner 协议驱动已加入本次增量，但尚无真实账号执行验收。真实客户端握手与完整任务执行分别验收，不能用配置生成结果代替。

macOS 本机已在临时 `CLAUDE_CONFIG_DIR` 中使用新二进制完成注册和重复注册验证：第二次保留原文件字节不变，Claude Code 官方 `mcp get coordica` 显示用户 scope 与 `Connected`。该检查未消费模型调用，也未修改正在使用的客户端配置；其他客户端的原生握手仍待实际验证。

## 分发许可

用户确认个人非商业使用及本地修改免费；个人商业用途、团队或公司使用及二次开发、发布修改版须另获书面授权。正式条款为根目录 `LICENSE` 中的 Coordica 个人非商业使用许可协议 1.0，属于源码可见许可。第三方组件适用原有许可，不受本项目额外限制。

## 工具注册增量（0.1.1-dev）

用户要求按工具注册和能力发现扩展，内置 Codex、Claude Code、dsh、Cursor CLI、Kimi Code、pi 六种定义。dsh/Cursor/Kimi 共用 ACP，pi 使用 JSON RPC；工具身份只在注册层定义。任务语法校验、调度、评审、Runner 探测、MCP 初始化、CLI 和 Web 按注册与能力工作。自定义工具通过可信本机 JSON 注册，不能由任务/MCP 调用参数注入命令。

实现和评审要求显式可用、结构化、可取消能力；评审另需 review。新增能力字段由 Go 契约生成，旧记录缺失字段不会被默认授权；旧 Run 继续按代次和租约核对。ACP/JSONL/RPC 测试覆盖实际结构化消息、失败终态与有限停止，不证明第三方账号或某一 OS 已验收。

Windows dsh 双机实测使用 [独立验收计划](two-machine-acceptance.md) 和安装包中的 `examples/acceptance`，先验证固定小任务和故障，再用本项目的一项小改动检验实际开发流程。Cursor、Kimi Code、pi 在用户安装版本上的真实执行仍须分别记录；本机未安装的工具不会伪报通过。


本次补齐了真实协议的联合类型消息：pi user content 字符串与 assistant content 数组、ACP tool_call 数组内容、Kimi 的部分权限请求及先前同会话元信息；取消后禁止迟到响应继续发起新工作。pi 会话恢复核对 header.cwd，跨工作目录拒绝。文件型 ACP 工具采用可信注册的 runner 快照模式，避免为模型 git commit 放开未知 shell；独立评审和中心检查规则不变。


0.1.1-dev 本机最终检查：全库 `go test -race ./...` 与 `go vet ./...` 通过；契约已重新生成，Web 13 项测试、TypeScript 检查和生产构建通过。独立验收项目的基线两项测试通过，新增需求 verifier 在基线上正确失败，在隔离的参考实现副本上通过。新的实际 Cursor/Kimi/pi 模型调用尚未运行；双机 dsh 仍等待目标电脑接入。三系统原生 CI 以该版本的 Release 说明链接为准。
