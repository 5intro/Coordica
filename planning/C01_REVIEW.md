# C01 只读复核与 C02–C08 首个切片

日期：2026-11-02（Asia/Shanghai）。需求基线：R2026-11-02.3。
任务状态以 [ROADMAP](../ROADMAP.md) 为准；本文件细化同一批任务，不另建任务编号或实施授权。
本次完成源码阅读、官方资料核查和方案准备；没有修改运行代码、调用 harness、启动任务或部署服务。

## 1. 固定基线与证据边界

| 来源 | 固定身份 / 事实 | 本次覆盖 |
| --- | --- | --- |
| 本地源码 | 分支 `codex/review-protocol-recovery`；HEAD `9de987f303e77a624adeb9b3feaceb2bb12c41ce` | 执行、协议、事务、事件、权限和开发绑定路径 |
| 本地主线引用 | main / origin/main 均为 `09d6b5f8ed1f576833e8017cf7137b11db0b6dfb` | 本地引用陈旧，不代表 GitHub 最新 main |
| GitHub main | `43f208327372fda8db94babda41e4a7d38a0e8dc` | 核对提交和比较结果，补读 #27 的 coordination、鉴权和游标实现；未完整审计全部最新源码 |
| 远端差异 | 本地 HEAD 与 #28 原 head `635fd217ace79fdc4cb38fd58462e1242a02f824` tree 相同；后者到远端 main 有 42 个差异文件 | 本地包含 #28 修复，但缺 #27 增量；该比较未改动 agent/runner/process，不能推导其他路径已全部复核 |
| 初始工作区 | `AGENT.md`、`AGENTS.md`、`README.md`、`SECURITY.md` 已修改；`ROADMAP.md` 和 `planning/` 未跟踪 | Owner 已有规划保留；没有 checkout、reset、fetch、提交或推送 |

源码结论使用 [AUDIT 的 S/D/F/L/G 分级](AUDIT.md#证据分级)。本轮只有 S/D/G；
官方接口文档也只是 D 级接口证据，不是当前安装版本兼容、夹具通过或真实 harness 实测。
实现前先在保留这些文档的前提下选定最新集成基线，复核对应差异；不得直接在陈旧本地主线发布。

## 2. 已有实现、声明和缺口

以下行号对应本地固定 HEAD；链接中的源码仅用于定位，后续工作区变化须重新核对。

| 路径与证据 | 当前事实 | 切片的采用边界 |
| --- | --- | --- |
| S：[agent.go:17](../internal/agent/agent.go)、[jsonl.go:17](../internal/agent/jsonl.go) | Request/Result 是一次 Execute；JSONL 写一条输入后关闭 stdin，并等待进程树停止 | 复用协议编解码和监督；新增 Session/Turn 操作，保留旧 Execute 的停止契约 |
| D/S：[registry.go:251](../internal/agent/registry.go)、[execution.go:175](../internal/runner/execution.go) | 有 resume 参数，但生产 Runner/Leader 请求未填 ResumeSession；Codex/Claude 当前禁交互审批 | 参数支持不等于持续会话或人工回答链已接通 |
| S：[acp.go:14](../internal/agent/acp.go)、[acp_permission.go:74](../internal/agent/acp_permission.go) | 每次启动、创建/恢复、一次 prompt 后关闭；按结构化范围可 allow_once，否则取消 | 不是“ACP 全部拒绝”，也不是 C06；新驱动须保留请求并回传具体答复，不放宽旧政策 |
| S：[pi.go:18](../internal/agent/pi.go) | 一次 prompt，结束后退出；原生交互目前取消；结果 SessionID 使用 sessionFile | 原生 ID 要注明种类，不把文件路径、thread ID 和 session ID 混作可互换身份 |
| S：[catalog_session.go:17](../internal/agent/catalog_session.go)、[model_protocols.go:275](../internal/agent/model_protocols.go) | 目录探测保存专用原生会话身份供后续恢复；每次 metadata 连接仍关闭 | 只证明元数据会话复用路径，不能算任务持续运行或第二轮实测 |
| S：[service.go:298](../internal/app/service.go)、[execution.go:21/378](../internal/app/execution.go) | 创建、领取、完成绑定项目、初始化、Git baseline、开发角色和候选 | 普通 Task 另走中立用例；不把现 Task 改成大量可选业务字段，不移除旧门禁 |
| S：[store.go:82/202](../internal/store/store.go)、[service.go:65](../internal/app/service.go) | 有事务、审计、幂等，以及 check→Replay→mutation 顺序 | 必须在缓存回执返回前重验持久权限；不能认为 mutate 本身已经鉴权 |
| G：[store.go:50/237](../internal/store/store.go) | 现 outbox 只有 delivered 标志；不是逐条领取、ACK 和未知效果对账 | C04 增加有界效果意图与领取/确认，不直接把 DrainOutbox 当可靠原生送达 |
| S/G：[leader_activity_capture.go:35/124](../internal/app/leader_activity_capture.go)、[evidence.go:15](../internal/runner/evidence.go) | 活动显示和本地诊断有容量上限，可丢失/记录 gap | 关键请求、身份和终态另走持久路径；输出可有界聚合，不从显示日志推导完成 |
| S：[recovery.go:16/251](../internal/app/recovery.go)、[runner.go:290](../internal/runner/runner.go) | 旧执行停止未知时保守占位；有 spool、旧 Run 围栏和有限恢复 | 保留这些不变式，区分 Turn 结束、后台工作停止和 harness 服务退出 |

远端 #27 的 [coordination.go](https://github.com/5intro/Coordica/blob/43f208327372fda8db94babda41e4a7d38a0e8dc/internal/app/coordination.go)
保存 WorkNote，回执为 `recorded_only`；[MCP 说明](https://github.com/5intro/Coordica/blob/43f208327372fda8db94babda41e4a7d38a0e8dc/internal/mcp/coordination.go)
明确没有 worker delivery，不能算 C05/C06 已实现。
可借鉴其持久鉴权先于 Replay、查询游标绑定 principal/query/epoch 和恢复时轮换 epoch；
其 project/chat 限制不能成为通用 runtime 的必填身份。

现有测试文件只作为后续回归入口：AtomicClaimAndReplay、撤销后重放拒绝、停止确认后改派、
中心重启恢复、慢活动订阅者、ACP/JSONL/pi 协议夹具及 opt-in live smoke。本轮未执行这些功能测试。
live smoke 本身是一条无工具提示词，不能证明多轮、审批、浏览器或故障恢复。

## 3. 原生接口与本机版本

官方资料检查日期：2026-11-02。资料可能更新；实施时要固定协议 schema/发行版本及摘要，
再对照目标二进制，不能直接把最新文档的方法当成本机已支持。

| Harness / 静态安装事实 | 官方接口与当前适配 | 首批建议 / 未完成项 |
| --- | --- | --- |
| Codex：App 内 CLI 包元数据 `0.166.0-alpha.5.1`、`aarch64-apple-darwin` | 现接 exec JSONL；官方 app-server 有 thread、turn、结构化事件和双向请求 | C07 优先 app-server；未启动核验该 alpha 包实际 schema、认证或能力 |
| dsh：桌面包元数据 `0.2.1-rc.5`；不能据此确定 ACP 服务端协议版本 | 现注册 `--profile acp`、session/resume；现驱动仍一次执行 | C08 的现有 ACP 候选；须补 vendor 版本/官方认证适用范围及真实请求证据；不等同参考原型 0.1.0 |
| Claude Code：本机 npm 包元数据 `2.1.114` | 现 stream JSON 一次执行；这轮未核查其持续双向接口版本 | 兼容旧路径，后续按能力纳入；版本元数据不是实测 |
| Antigravity：桌面包 `com.google.antigravity`、`2.28.2` | 官方有 ACP 服务端及独立 CLI 流式输入；本仓库未注册接入 | 纳入 C08 的 ACP 候选和 C09/C10 适配矩阵，先核实服务端发行包，不读取桌面私有 RPC |

首个实例建议选 Codex app-server；第二个选一个通过版本及认证核验的 ACP 实例。
Antigravity 优先核验官方 ACP 发行包，dsh 是现有候选；最终实例、模型、模式和认证引用须明确固定，
这只是选择建议，不能由失败回退静默切换两者。未安装必要服务端时先完成离线契约验证，不假称已接通。

### Codex 的采用边界

[官方 app-server 文档](https://learn.chatgpt.com/docs/app-server)提供 thread/start/read/resume、turn/start/steer/interrupt 和原生请求。
steer 必须匹配活跃 expectedTurnId；resume 不等于重新发送上一条输入。
问题回传及部分操作是 experimental，须按固定版本显式协商。
serverRequest/resolved 也可能表示请求被清理，不能直接记为“用户批准成功”。
结束 Turn 不证明后台终端停止；unsubscribe 不等于杀进程或删除原生历史。
复用官方原生认证机制；不抽取登录 token、不改造成模型 API 代理。

### ACP 的采用边界

按 [ACP v1 会话](https://agentclientprotocol.com/protocol/v1/session-setup)与
[prompt/cancel](https://agentclientprotocol.com/protocol/v1/prompt-turn)建立版本夹具，
session/request_permission 回传原生 option；session/cancel 等待适用的结束证据。
load/resume/config/elicitation 依协商和厂商扩展分别报告，不能因协议名称相同而推定所有方法都可用。
不用 v2 draft 约定冒充已装 v1 实现；普通 queue 可以由底座串行下一轮实现，steer 不可拿 queue 冒充。

### Antigravity 的具体接入路线

[Google 官方更新记录](https://antigravity.google/docs/changelog)记载 antigravity-acp 面向自定义 ACP 客户端，
涉及 permission/config/session；[官方 Zed 集成](https://antigravity.google/docs/ide/extensions/zed/)说明原生认证选项。
这些是厂商声明，不是桌面版 2.28.2 的服务端协议版本。

本机静态检查未在 PATH 以及 `~/.local/bin`、`/usr/local/bin`、`/opt/homebrew/bin` 的常用位置找到
agy、antigravity-acp 或 agy_acp_server；不据此断言所有磁盘位置都没有安装。
App 内 language_server 等私有组件不作为公开协议入口。未启动 App/CLI、安装组件或访问 Keychain。

优先用通用 ACP 驱动加版本化启动/认证配置；不在调度器加 Antigravity 品牌分支。
若最终选择 [官方 headless CLI](https://antigravity.google/docs/cli/headless/)，它的 stream-json stdin 可连续多轮，
但 control_request/control_response 明确不支持，不能套用 Claude 的同名模式。
该模式有需审批工具被拒绝却进程退出 0 的情形；退出码和 result 不能替代工具结果及业务验收。
不得通过跳过权限选项伪造审批闭环；没有原生 steer/请求回传证据就明确报 unsupported/unknown。
认证按原生 profile/官方选项引用；模型、effort、MCP、skills、浏览器分项核验，不复制订阅凭据。

## 4. 首个实施切片的范围

目标：一个无聊天、无 `.git` 的程序客户端提交 Task，指定实例，在同一逻辑 Session 中完成两轮；
能补取事件、回答原生问题及适用许可、区分 queue/steer/interrupt，并在结束后读取或释放资源。
Task 独立存续；Attention 只从原生请求、执行异常和恢复条件投影，不创建 Blocker 状态机。
先离线故障夹具，再一个真实 harness，最后第二种不同 harness 复用同一套契约。

不在本切片建设完整多节点负载平台、多应用业务 UI、父子任务编排、额度管理平台或一次性迁走开发应用。
C02 引入最小实例/权限/四态事实骨架；完整发现、多实例和路由仍分别归 C09–C11。
C03/C04 必须落实基本重启与身份围栏，不能等 C12 才补过期审批或启动重复执行保护。

### 模块责任

| 模块（建议位置） | 此切片责任 | 保持的依赖边界 |
| --- | --- | --- |
| `internal/runtime/`，按 task/session/turn/request/effect 拆职责 | 中立实体、应用用例、事务内规则；不继续扩张现开发 Service | 不依赖 Git、开发需求/候选；小接口由消费者拥有，不造通用工作流框架 |
| `internal/store/runtime_*` | 复用 SQLite 事务容器，保存输入、身份绑定、事件、请求、效果和幂等回执 | 不执行网络/进程；唯一约束处理竞态，不靠扫描 JSON 去重 |
| `internal/agent` 的独立协议驱动 | 原生会话操作、能力协商、事件归一和请求应答 | 不判断调度/业务授权；旧 Execute 保持兼容，不把新协议行为塞进品牌条件树 |
| `internal/process` | 现有进程 I/O、身份、监督及停止事实 | 不知道 Task、HTTP、SQLite；Turn 停止与服务进程退出分层 |
| `internal/runner` 的中立执行入口 | 执行已授权效果、持久 spool、身份/代际对账、事件回传 | 不无条件 CloneSnapshot；与原开发执行路径分开，资源容量仍真实占用 |
| experimental HTTP / CLI / MCP | 输入校验、流订阅及结果转换 | 调同一 runtime 用例；不复刻授权/状态机，不扩大旧 Owner/node/chat 凭据 |
| 现开发 `internal/app` 及后续兼容桥 | 原有初始化、Goal、范围、候选、测试、审查和发布规则 | C16 再逐步桥接；单一调度归属，不影子双派发 |

### 最小接口与持久事实（草案，非现有 API）

| 用例 / 协议能力组 | 输入及回执重点 |
| --- | --- |
| SubmitTask / AppendTaskInput / RequestWake | app 身份、实例/节点范围、幂等键、内容 digest、输入种类、授权/策略版本；context 仅存材料，continuation 才按规则唤醒 |
| OpenSession / ReadSession / ResumeSession / ReleaseSession | 逻辑 ID、typed native ID、binding_generation、实例/profile/工作区指纹；resume 只恢复上下文，忙时释放需停止策略 |
| StartTurn / QueueInput / SteerTurn / InterruptTurn | Task/Run/Turn 和预期版本；原生接收/消费事实与“已保存”分开；stale 不默认转下一轮 |
| RespondRequest | 原生 request ID、binding/Turn/Run 代际、请求版本、答复主体及授权；记录回答与原生 ACK 分开 |
| ReadEvents / ListTasks / QueryAttention | 查询/权限/epoch 绑定游标；来源 ID、expected_version、allowed_actions；操作返回具体来源用例 |

不是要求每个适配器实现一个大接口；按调用方实际需要拆 session、turn、request 操作，不支持则结构化拒绝。
协议驱动不承载 Task 调度；运行管理在调用前验证 Task/Run 归属及固定执行选项。
首阶段契约标 experimental，C15 才冻结公共兼容承诺；旧 `/api/v1` 语义保持。

持久化先支持必需 Task/Run、SessionBinding/Turn、TaskInput/WakeIntent、Event、PendingRequest 和 Effect；
不因实体清单提前建完整 Agent 管理平台。低频对象可复用 records 分区，但输入、来源事件、效果领取和
单活跃 Turn 要有显式约束/索引。当前 schema=1，需设计版本升级、备份和失败回滚，不让旧程序接管新活跃会话。

- 同源输入按 app/source/event/task 去重并比较 digest，允许显式多任务扇出；同键异内容冲突。
- WakeIntent 消费与当前授权/状态/依赖复核、审计和效果同事务；按期待状态创建新尝试或向现有执行投递。
  继续现有 Run/Turn 时保留原身份、binding 和代际，不因回答或唤醒重复创建 Run；程序提交无 Leader 前置。
- 认证在 Replay 之前重验；新应用身份不能从旧 profile/source ID 推导。旧无 scope 凭据须换发。
  C02 由明确获准的管理主体登记应用/实例/node scopes；配置实例不授予执行权限，旧聊天凭据不能自行铸造新权限。
- Effect 带 command/dispatch ID、目标身份/代际、领取 owner/lease、attempt、重试策略和 ACK/evidence。
  数据库外调用原生端；不承诺跨进程 exactly-once，外部效果未知先对账，不盲重放。
- 原生进程已启动但 binding 未落盘时保留 unknown/占位；旧 Run/Turn 迟到消息只能补自己的事实。
- 关键事件与状态同事务，来源序号/ID 去重并校验内容；输出可聚合但有 gap 和字节上限。
  慢订阅者不能堵塞协议控制或丢失请求；磁盘满时停止接新工作并保留故障，不能假记事件已落盘。
- 请求 response_recorded 不等于 response_delivered；相反并发回答仅一次生效。
  原生回调随进程失效时过期；若声称可重新关联，须先获得原生请求仍有效的证据。
- Turn 完成、原生后台工作停止、整个 harness 进程停止分别记录；不为复用旧 TreeStopped 每轮杀服务。
  任务改派仍需旧工作停止证明；Session 空闲不自动判 Task 完成，释放也不重开/取消 Task。

### C02–C08 拆分、依赖与验证

| ID | 首个切片交付 | 验证重点 | 依赖 / 可并行边界 |
| --- | --- | --- | --- |
| C02 | 无 Git/聊天的 Task 提交、输入/唤醒、最小 scoped identity/instance；初步来源引用 | V02/V16/V17：取消后迟到输入、重复唤醒、同键异内容、撤销后缓存回执拒绝 | 先补 C01 固定源码和实例证据；需实施授权 |
| C03 | Session/Binding/Turn、单写者、read/resume/release；旧 Execute 兼容边界 | V03/V09：第二轮同绑定、Session 释放 Task 仍在、恢复不自动重发、启动落盘间崩溃 | C02 契约固定；与 C04 可并行，数据库迁移串行集成 |
| C04 | 关键事件和效果、来源去重、spool/ACK、游标/保留/背压 | V05/V09：重连重复/乱序/旧代际、慢消费者、游标过期、未知副作用 | C02 契约固定；对 C03 提供稳定事件/效果端口 |
| C05 | queue/steer/interrupt 分开的命令及送达回执 | V04：Turn 完成竞态、stale_target、未知消费、打断不建下一轮 | C03+C04；集成后做状态机竞态，不按品牌补条件 |
| C06 | PendingRequest、答复 CAS/原生回传、失效对账；最小 Attention 查询 | V06/V17：重复/相反回答、撤销/过期、丢 ACK、重启旧请求、多个来源；正常等待不要求人工 | C03–C05；不建设另一份 Blocker 生命周期 |
| C07 | Codex app-server 垂直连接；版本 schema 与 experimental 能力证据 | 上述同一场景；真实两轮、提问和适用审批及拒绝；无工具烟测不够 | C03–C06 的离线回归先通过，取得真实运行授权/原生认证 |
| C08 | 一个不同 ACP harness；Antigravity 官方 ACP 优先核验，dsh 保留候选 | 同一套用例；协议不支持项有证据及拒绝测试；不假造 steer 或 approval | 离线适配依赖 C07 离线契约/回归；真实验收仍依赖 C07 真实闭环；不静默换品牌/模型 |

先用单节点、单实例、无害数据做最小闭环；不把 C09–C14 的平台建设提前拉进来。
并行只用于稳定契约后的独立源码/夹具工作；共享迁移和最终集成串行。
真实 harness 并发验证前检查主机资源，不凭配置槽数宣称机器有余量。

## 5. 首个切片的交付门槛与未决项

1. 离线回归覆盖唯一领取、幂等冲突、持久权限、旧身份、事件补取、停止未知、相反答复和崩溃窗口。
2. 非 Git、无聊天程序端真实跑通；同会话两轮、原生问题和适用许可可回答；至少一家完成两类原生请求及拒绝。
3. 两家同契约验证，分别记录实际方法/版本、OS、实例和原生事实；能力不对称诚实呈现。
4. 旧开发链保留候选/main、授权、测试与独立审查门禁；新运行成功不替代业务验收。
5. 事件/诊断有界，测试进程退出和临时文件回收有证据；保留 fixture 与 live 结果的独立记录。

本轮未决：最新主线的完整集成复核、安装包与协议 schema 对应、第二个实例的可得性/认证适用范围、
原生提问扩展、有效配置及停止证据。Antigravity 桌面版已安装不能解除这些缺口。
这些缺口不妨碍交付当前只读方案，但不能据此标记 C01 全部完成或 C02–C08 可执行。
本轮验证及文档候选状态见 [PROGRESS](PROGRESS.md)；没有新功能或真实 harness 通过结论。

## 6. 云端接收与固定离线协议基线（2026-11-02 UTC）

以下是本次云端实际观测，不沿用上文的本机安装或认证证据。
接收 main `43f208327372fda8db94babda41e4a7d38a0e8dc` 与规划 #29 head
`6325566dde16e80e079c5cb03e673f39aeff5992`；两者仅有 14 个 Markdown 文件差异。
工作分支 `codex/task-runtime-c01-20261102` 从该规划 head 建立，初始 tracked 工作区干净。
已完整读取本组规划、README、AGENTS/AGENT、QUALITY、REVIEW、SECURITY；仓库没有 `.agents/skills`。

本轮在当前工作区重读 agent Request/Result、JSONL/ACP/pi 生命周期、registry、目录会话、
Runner 的 CloneSnapshot/Request/恢复路径，以及 store 事务、Replay/Audit/outbox 和 #27 coordination 路径。
原审计的相关 S/G 结论仍成立：单次 Execute 不等于多轮；生产 Request 未填 ResumeSession；
WorkNote 仅 recorded_only；旧 outbox 没有逐效果领取和原生 ACK。此为相关路径复核，不是全仓安全审计。

### 实测工具和资源

- Linux x86_64；cgroup CPU 配额 4 核、内存上限 16 GiB；检查时工作区可用磁盘约 29.5 GiB。
  共享迁移串行；不并发启动全仓 race。
- Node `v24.19.0`、Python `3.12.14`、Git `2.52.0`。
- 初始 PATH 的 `/usr/bin/go` 拒绝 `version`，常用位置和仓库 `.tools` 未找到 Go 编译器。
  从 [Go 官方下载元数据](https://go.dev/dl/?mode=json&include=all)取得 `go1.27.2.linux-amd64.tar.gz`，
  实际 70,590,635 字节，SHA-256 `ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5`。
  校验后安装到忽略目录 `.tools/go`；实测 `go version go1.27.2 linux/amd64`，`gofmt -h` 退出 0。
  没有修改系统同名程序；后续 Makefile 自动选择 `.tools/go/bin/go`。
- 云端已安装的 Codex `--version` 返回 `codex-cli 0.163.0`，不是历史 macOS alpha 包。
  二进制 SHA-256 `399ff69c431c066e40de318fba45c2b07f45a76b19939d247ee4828fc70c4215`。
  版本/生成命令提示 PATH alias 目录只读，但命令退出 0；没有改权限或启动模型。

### 两个目标实例与协议版本

下列实例名是离线适配目标，尚未登记为可执行或已认证实例。配置不授予真实执行权限。
实例模型和模式在调用时显式提供并按原生能力拒绝不支持项；不复用本次施工模型作为产品默认值。

| 目标 | 固定身份和证据 | 当前边界 |
| --- | --- | --- |
| `codex-app-server-linux` | 实测 CLI 0.163.0；[上游对应发行源码](https://github.com/openai/codex/tree/83086188ae0614bd882818ff1aa23f3194962012)，tag `rust-v0.163.0` | 只运行版本与本地 schema 生成；未读取登录、启动 server 或请求推理，认证/实际调用未知 |
| `antigravity-acp-linux` | [ACP 注册表固定版本](https://github.com/agentclientprotocol/registry/blob/6d274b4d433066bc1744c509356419c69667c695/antigravity-acp/agent.json)：Google LLC、1.4.5、proprietary | 未安装/启动；不把桌面 2.28.2 当 ACP 版本，dsh 不作失败回退 |

Antigravity Linux x86_64 的注册表声明包为
`https://dl.google.com/agy-extensions/releases/linux/agy-acp-server-1.4.5-linux-x86_64.zip`，
SHA-256 `923923a0d7dc8073f66cc6f78647a3107e3a7ee2b7e1a24df8b3927b5188013b`，
入口 `./agy_acp_server.par`、argv `--uid=`。这些是发行声明，未声称本轮下载归档或校验二进制。
注册表文件实际下载字节 SHA-256 `00955e5d71f60d2c8255847fd35966f7f27e25c3bc3c59b28d08b1df9fcd1fac`。

ACP 夹具基线选 v0.11.0 的稳定 v1 schema，解引用 tag 后固定源码
[`451c3cbc7df2f9bc164c25e25d36fa526003f085`](https://github.com/agentclientprotocol/agent-client-protocol/blob/451c3cbc7df2f9bc164c25e25d36fa526003f085/schema/schema.json)。
`schema/schema.json` 实际 141,370 字节，SHA-256
`11a4cc8ca894f0f8acb55dc85d1bdf30065bd58608e6a965e4fb9192ef14fe6f`，113 个定义。
该 schema 是客户端离线基线，不是 Antigravity 已完成握手的证据；扩展方法逐项协商，不以 v2 draft 替代。

Codex 运行 `codex app-server generate-json-schema --experimental --out <忽略目录>`，退出 0，生成 452 个 JSON 文件。
合并 schema `codex_app_server_protocol.schemas.json` 为 890,029 字节，SHA-256
`43e8c1495557ca91e33a63bbfae36c311c206253afae2974c979dbdf5d940c71`。
`ServerRequest.json` SHA-256 `3d7bc481f84dc042984a74420e65c5a8d3c423f37a91b7d5df2365c903a12ac6`。
此为实际二进制的离线生成证据；未把该二进制与上游发行产物做可复现构建比对。

- 生成 schema 中 `turn/steer` 必填 expectedTurnId/input/threadId，`turn/interrupt` 必填 threadId/turnId，
  `thread/resume` 必填 threadId；恢复不重发工作，steer 不转 queue。
- 原生服务请求包含 commandExecution/fileChange/permissions 审批、tool/requestUserInput 和 MCP elicitation；
  C06/C07 仍须落实回调身份、代际、版本及 ACK，不能从 resolved 通知推导批准。
- [当前官方文档](https://learn.chatgpt.com/docs/app-server)要求 experimentalApi 显式协商；
  其认证说明区分本地应用和 commercial/hosted service。未来真实验证须按实际部署及官方适用范围另核授权，
  本批仅离线开发，不以原生 Cloud 施工授权代替产品订阅运行授权。
- [Google 官方认证说明](https://antigravity.google/docs/ide/extensions/zed/)声明个人计划使用 oauth-personal，
  企业/API 认证单列；这是官方声明，云端认证状态仍为未探测，未迁移凭据或登录。

以上固定离线切片的协议和来源输入，可继续准备 C02；C01 的真实实例/认证验证保持待验证。
C07/C08 的真实阶段出口保持未完成，不能据 schema 生成宣布已支持多轮、审批或工具调用。

## 7. 恢复后的范围与第一纵向切片（R2026-11-02.4）

Owner 曾暂停当前方向，保留已推送的 `871c14db98cddcf2fa5a6222a3459e41f810eab5`；没有回滚或删除。
恢复后追加 R93–R100，保留 R01–R92 历史；上下文归 Task，调用方明确指定实例，跨厂商交接后置。
完整核读 DeepSeek 官方固定版本的 000/001/007 文档和相关源码，最小机制映射见 TASK_DRIVEN。
前台接收/显式路由、持久 Task/Input/Wake 和后台 Assignment 分工；无每 Agent 无限循环或第二状态机。

第一代码子范围选择 C07 的最小 Codex app-server 进程协议端口和离线夹具：初始化、请求/响应关联、
有界事件及原生请求、同连接两轮、补充/中断的准确方法与原生 ID、断开/未知结果和进程停止证据。
仅使用监督夹具子进程，不调用真实订阅 harness；不改旧 exec 驱动，不先建完整调度平台。
随后 C02 保存任务上下文/输入与显式执行绑定，C03–C06 随这条链路逐段集成，C08 再验证第二种协议。
第一端口本身不宣称 Task 持久化、人工授权门禁或完整持续会话已经完成；它提供这些用例所需的原生事实。

## 8. 首期只做聚合（R2026-11-02.5）

Owner 最新将多 Agent 协作、DeepSeek 自驱动编排及跨 harness 接力后置；研究与接口边界保留，
首期不做自主 Wake 派工、总管、复杂父子任务或协作策略。统一 Task 上下文和必要原生控制仍保留。
C07 先完成原生协议薄适配的离线切片，C08 用第二 harness 验证共性；TaskInput/命令意图仅按可靠送达实际需要实现。
当前本地 app-server 传输没有自主调度或 Wake 实现，不需要为这次收紧删除已完成的必要控制代码。
