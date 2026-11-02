# 源码审计与 T3 参考实现

日期：2026-11-02。静态审计基线：`9de987f303e77a624adeb9b3feaceb2bb12c41ce`。
本次审计聚焦新方向涉及的入口、适配、执行、能力和持久化；不代表全仓库安全审计。
已有测试文件是可复用验证线索，不是本次测试通过或实际 harness 可用性的证据。
云端 HEAD 改变后，C01 必须重查对应路径，不将本文当永久事实。
2026-11-02 C01 只读补充已记录到 [复核与首个切片](C01_REVIEW.md)：本地仍为上述 HEAD，
远端 main 已为 `43f208327372fda8db94babda41e4a7d38a0e8dc`，只对相关增量补读，未完整审计远端全部文件。

## 证据分级

- S：已读实现，能说明当前路径；没有实际执行认证。
- D：注册/配置声明，或仅有协议能力标记；不能视作完整接入。
- F：fixture/mock 回归通过，必须有固定提交和命令输出才能标记。
- L：真实 harness 验证，绑定平台、版本、实例配置和时间，不能跨平台继承。
- G：在已审路径中缺少新需求闭环；不是声称整个仓库绝对没有同名功能。

本次只生成 S/D/G。历史文档中其他平台或版本的实测不自动继承到新会话架构。

## Coordica 当前事实与缺口

| 领域 | 已读事实 / 证据 | 新方向缺口与落点 |
| --- | --- | --- |
| 适配器 | S：[registry](../internal/agent/registry.go) 注册 Codex JSONL、Claude stream JSON、ACP、pi RPC | 复用驱动，新增持久会话操作，不在调度器按品牌分支；C03、C07–C09 |
| 能力 | S/D：[domain model](../internal/domain/model.go) 多为布尔能力；Probe 区分安装与可用，但不认证登录成功 | 四态、条件、认证及实测证据分开；C09 |
| 模型目录 | S：[execution_options](../internal/domain/execution_options.go) 要求 reported 且目录含选项；明确选择不回退 | 新契约增加 explicit_unverified；旧 API 不直接改语义；C09 |
| 多实例 | S：注册工具 ID，Runner Executables 以工具为键 | 类型、实例、认证域、节点分别建模；C02、C10 |
| 执行抽象 | S：[agent Request/Result](../internal/agent/agent.go) 单次 Execute，Event 为 type+raw data | Session/Turn/Run 分离，支持交互句柄；C02–C06 |
| 会话恢复 | D/S：适配器有 ResumeSession；[Runner 请求](../internal/runner/execution.go) 当前不填该字段 | 通用会话恢复链未接通；不能宣称持续会话已实现；C03 |
| ACP 生命周期 | S：[acp](../internal/agent/acp.go) 初始化、创建/加载、prompt 完成后 shutdown | 单次执行不能承载连续输入；C03、C05、C08 |
| ACP 权限 | S：[acp_permission](../internal/agent/acp_permission.go) 按结构化范围 allow_once，否则取消 | 没有持久 PendingRequest 和人工回答回传闭环；C06 |
| 其他交互 | G：已有 Leader 初始化/规划问题属于开发业务交互 | 不能直接当所有原生工具的请求通道；新建中立请求对象；C06 |
| 原生配置 | S：ACP session/new 传空 mcpServers，客户端 fs/terminal 能力为 false；Codex 启动有配置限制 | 不能声称完整继承本机 skills/MCP；需按模式报告实际生效配置；C09 |
| Git 绑定 | S：[Runner execution](../internal/runner/execution.go) 下载 input bundle、CloneSnapshot 后执行 | 新增非 Git 执行路径，旧开发路径保留；C02、C16 |
| 领取/调度 | S：[app execution](../internal/app/execution.go) 检查权限、依赖、容量、热点资源；审查优先不同工具 | 业务 Task 与底座任务分离；优先级/路由可解释；C11、C13 |
| 并发 | S：[Runner](../internal/runner/runner.go) 容量 1–2；当前 Claim 无 CPU/内存择优 | 不称资源自适应；按显式策略演进；C11 |
| 持久化 | S：[store](../internal/store/store.go)、[service](../internal/app/service.go) 已有状态、审计、幂等和 outbox | 现有 outbox 不等于通用逐效果确认队列，须新增送达/重试/所有者语义；C04、C12 |
| 活动记录 | S：[leader_activity](../internal/store/leader_activity.go) 有游标；总量 4096，单条 2048 字节 | 这是有界 Leader 活动投影，不是所有会话完整事件链；C04 |
| 恢复/停止 | S：[spool](../internal/runner/persistence.go)、[recovery](../internal/runner/recovery.go) 保存回执和停止事实 | 保留该基础，补原生会话/待处理请求对账；C12 |
| 验证基础 | S：[live_test](../internal/agent/live_test.go) 为 opt-in 单提示词 smoke，明确不调用工具 | 不能代表工具、多轮、审批、额度或 GUI 已验证；C07–C10、C18 |

## T3 参考版本与可重定位来源

实际检查的是本机安装的 **T3 Code Beta 0.1.6**，内嵌 `t3codeCommitHash=2359643cffa6`。
来源为安装包 `Contents/Resources/app.asar` 的 package.json 和服务端 bundle，未调用其服务或启动 Agent。
bundle 保留原模块路径注释，以下定位不依赖本机临时解包目录。
没有将 173 MB 安装包、第三方实现或个人应用数据放入 Coordica 仓库。

- 安装包 SHA-256：`1244602026b57e2eecc6f564f88a94865c783297c0a0f3eab6e5c3499a96bbb8`。
- bundle：`apps/server/dist/binCli-C12fvdZY.mjs`。
- bundle SHA-256：`472d7e2d82900ae9da669da1dfd9cd4b7075f61e6e6ac9bda9d6dd9644550d25`。
- 项目入口：[T3 Code](https://t3.codes)。本次事实来自上述本地版本，未假定线上当前版本相同。

云端按版本/提交寻找合法可获得源码或安装产物，再用模块路径和符号定位；找不到时记录 reference_unavailable。
本文已有行为摘要足以讨论方案，但不能把未取得的源码写成已复核。不得依赖 `/Applications` 或 `/tmp` 的本机文件。
以下是设计借鉴，不是源码复制授权；若将来复制实现，单独核对源版本许可和 NOTICE 要求。

| ID | 原模块 / 符号（上述 bundle 起始行） | 已观察的实现逻辑 | Coordica 采用 / 限制 |
| --- | --- | --- | --- |
| T01 | `packages/provider-core/src/server/ProviderAdapter.ts`（150366） | 统一会话、Turn、工具项、请求、子任务事件；操作有专用错误，包括停止未确认 | 借鉴分层操作和结构化事实；不强迫每个协议模拟全部能力；C03–C08 |
| T02 | `src/provider/ProviderInstanceRegistry.ts`（150056）、`ProviderAdapterRegistry.ts`（150662） | 类型与实例分开；相同 credential binding 在登录变更期间限制 openSession | 采用实例/认证域和变更互斥；不复制订阅 token 管理或任何非官方接口；C09–C10 |
| T03 | `ProviderSessionTransitionPolicy.ts`（282460）、`ProviderSwitchService.ts`（282492） | 比较 continuation identity、实例、模型、工作区，分类复用、重启恢复、交接或拒绝 | 采用显式迁移计划及上下文交接；不兼容身份绝不伪装 resume；C03、C12 |
| T04 | `EffectWorker.ts`（302482）、`ProviderRuntimeRecoveryService.ts`（303096） | outbox 领取/重试、可安全重放效果区分、重启对账；应用子任务不等于原生后台工作 | 扩展现有事务/outbox/spool，不整库改为事件溯源；保留停止证明；C04、C12 |
| T05 | `ProviderAdapter.ts`、`RuntimeRequestService.ts`（302194） | 请求响应是独立操作，有请求 ID、运行关联和专用失败 | 借鉴请求和交互操作边界；本方案的授权版本/过期围栏是 Coordica 要求，不宣称 T3 完全等同；C06 |
| T06 | `OrchestratorMcpService.ts`（343833），`delegateTask`（约 344934） | 选 provider/model，幂等委派，异步完成唤醒；wait 超时不取消孩子；可指定 worktree | 采用受控父子任务与回执；不照搬默认 shared checkout，不无限派生；C13 |
| T07 | `src/peer/RemoteDelegation.ts`（343443） | linked environment 创建远端线程，跟踪终态回传，权限拒绝与临时断网分开 | 学习远端任务关联和错误分类；Coordica 保留 Center–Runner 权威模型；C11–C13 |
| T08 | `UsageLimitRecoveryWorker.ts`（293404） | 从 usage_limit、resetAt 和用户配置决定唤醒；稳定命令 ID 抑制重复恢复 | 使用真实额度和显式恢复策略；不自动换账号、模型或 harness；C14 |
| T09 | `ContextHandoffService.ts`（149263）、`ContextHandoffDelivery.ts`（301053） | 交接为独立服务，与原生 Session 续接区分 | 采用带来源/版本的上下文材料与结果引用；材料不是权限；C03、C13、C15 |

不将 T3 的资源/调度能力推断为更好的全局负载均衡；本次没有运行性能、可靠性对比，也没有核验所有功能 UI 可达性。
T3 对不支持运行模式存在回退分支的实现，不能覆盖本基线“不静默降级”的要求。
T3 内部 mailbox、React/Effect 技术栈及业务工作台不是 Coordica 必须引入的依赖。

## DeepSeek 原型参考（本轮已确认）

Owner 已提供 [deepseek-ai/dsh-superagent-prototype](https://github.com/deepseek-ai/dsh-superagent-prototype)。
2026-11-02 固定提交 `6337fc823ce20251c8de8085948165c81aaa701e`，package 版本 0.1.0。
已读领域/执行文档及 core ports/intake/assignments/engine、app runtime/effects、executor dsh/background 的相关实现。
Task/Ticket 与执行绑定分离、输入送达/消费、持久效果和资源占用的采用方案见 [任务驱动方案](TASK_DRIVEN.md)。
原型依赖 DSH/Cordis 内部服务，定位单仓库应用；不是通用多 harness SDK，也不等同本机 DSH 安装包。
本轮未运行原型或测试，未把它作为依赖引入。原“来源版本未确认”记录已由本次固定源码审计解除。

## 待 C01 确认的决策

1. 首批两种 harness 的精确版本、官方程序接口、登录方式和云端可用平台。
2. 当前许可下订阅登录可用于哪些程序接口；记录官方来源和检查日期，未知则不作能力承诺。
3. 第二种 harness 的原生提问、审批、运行中输入是否可端到端测试；不支持时记录能力差异。
4. 会话/事件 API 草案和默认资源上限；不要先铺开所有厂商再确认最小契约。
5. 按已固定的 DeepSeek/T3 版本复核目标切片；参考版本升级时记录行为差异而非覆盖旧审计。

## C01 只读补充与 Antigravity

- 本地 Execute 仍为一次执行；目录探测保存专用会话身份、每次关闭 metadata 连接，不等于任务持续会话。
  pi 的结果 SessionID 是 sessionFile，应与其他协议原生 ID 区分。
- 远端 #27 的 WorkNote 是 `recorded_only`，未接 worker delivery；不是 C05/C06 已完成证据。
  可借鉴其重放前鉴权和带权限/epoch 的有界查询，不搬开发 project/chat 身份成为底座前置。
- 活动显示可有 gap；持久关键请求/终态必须另走可靠路径。现有 outbox 不具逐效果领取和原生 ACK 语义。
- Codex 当前官方 app-server 接口、安装包元数据和本仓库旧 exec 驱动分开记录；未核验版本对应。
- Antigravity 桌面 2.28.2 已静态确认。官方提供 ACP 与流式 CLI，但未在本轮安装或运行，能力保持待验证。
  优先复用 ACP，不解析桌面 UI 或调用私有 language_server；CLI 模式不能假设支持审批控制消息。
- C01_REVIEW 保存官方来源、检查日期、精确源码身份、首批实例建议和适配边界。
  本轮证据仍只有 S/D/G，没有新增 F/L；旧版本实验实测不自动继承。
