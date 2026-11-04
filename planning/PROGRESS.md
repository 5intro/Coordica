# 实施记录与决策日志

工作修订：R2026-11-02.6；本次恢复核查的 main 输入版本为 R2026-11-02.5。主表：[ROADMAP](../ROADMAP.md)。
仅记录已发生的工作和明确决策；不要将实施建议写成已授权事实。

## 当前状态快照（2026-11-04 UTC）

- 规划 [#29](https://github.com/5intro/Coordica/pull/29) 与 C01 文档 [#30](https://github.com/5intro/Coordica/pull/30) 已合并，
  截至本次恢复核查，远端 main 输入为 `efa1ec1f8191b5fd4530cde6678d5b2e05fc4b58`；历史合并/审查证据见后文记录。
  旧批次中“open / denied / 待授权”均为当时事实，不再代表这两个 PR 的现状。
- C01 的实例认证/真实调用仍待验证；C02–C05、C07、C08 在本源码树有局部离线实现，C06 仍规划中。
  source、手写 fixture、局部回归、本地 review 与真实验收分开，不据此标主任务完成。
- 产品检查点固定 [ab0e2068ee42face7516acfaa6fd559fc7ab0c11](https://github.com/5intro/Coordica/commit/ab0e2068ee42face7516acfaa6fd559fc7ab0c11)，
  父会话已用标准非 force push 同步到 [codex/cloud-checkpoint-20261104](https://github.com/5intro/Coordica/tree/codex/cloud-checkpoint-20261104)，并用官方 API 核准原哈希。
  这是备份分支，不是 main 合并、产品 PR 或发布；原十三条规划/实现分支与原始 SHA 保持。
- 独立文档分支 `codex/publish-provider-plan-20261104` 基于 `1189e104b02c8fde0adaa94a2c0ce4c4a2bb1e23`，
  仅同步恢复授权/范围/证据和正式审查条件，已由父会话发布 draft [PR #31](https://github.com/5intro/Coordica/pull/31)。
  本源码分支从 ab0 保留已有产品实现，并合入该文档更新；PR #31 本身不包含产品源码。历史 local-only/no-PR 与 10% 暂停线已被 Nov4 用户授权覆盖。
- 新环境当前 HTTP unrestricted/enforced、observations_current=true，VPN 未配置、TCP grants 为空；官方 Go 1.27.2 和已有依赖缓存可复用。
  ab0 在恢复后四包普通/竞态各172项及 vet/build/quality/八项漂移通过；全仓 mcpconfig 中立临时目录前提仍阻断。
  已知 `/tmp/.git`、`/workspace/.git` 标记仍在；只读复核，不删除、不重试只读目录、不绕守卫或改变网络/凭据。
- 当前允许受影响检查与质量通过后创建小范围 draft；最新 head 三平台 CI 和代码/安全正式明确结论仍是合并前提。
  本地明确 review 不等于 GitHub 正式两审，checkpoint 不等于 PR，真实 harness/部署/派发/定时仍未授权。

以下批次按发生时的输入和结论保留；涉及当前状态，以此快照及最新追加证据为准。

## 授权记录

| 日期 | 来源 | 范围 | 不包含 |
| --- | --- | --- | --- |
| 2026-11-02 | Owner 本次聊天：审阅新需求、加入 T3 借鉴、写开发规划表并长期维护 | 静态审计、需求/方案/规划文件、维护与交接指引 | 新功能实施、真实模型调用、发布部署、节点授权或启动业务运行 |
| 2026-11-02 | Owner 指定 R2026-11-02.3，先做 C01 只读复核与 C02–C08 切片准备，并加入 Antigravity | 当前源码/dirty 核对、官方资料与安装元数据只读核查、规划交接维护 | C02–C18 实施、安装组件、真实 harness 调用、部署/重启或启动实验 |
| 2026-11-02 | Owner 追加：准备任务，创建绑定仓库的云端环境，6.1sol ultra 开发/自测/PR/自动审查/修复/合并，定时跟进至 merge | 本批 C01–C08 云端实现和离线回归、规划分支/PR、明确门禁通过后的合并、原云端会话续工与监控；C09–C12 准备 | 真实 harness 订阅消费、凭据迁移、正式中心部署/运行、配对、实验、安装包发布和本机升级；不跳过阶段出口 |
| 2026-11-02 | Owner 最新：不要定时任务，由 Dot 监控；新增 PR 意见发用户并由云端 Agent 修复；额度低于 10% 暂停 | 覆盖旧定时要求；同一云端上下文继续原 PR 修复补测，保留固定 head 门禁 | 不创建额外定时或常驻监控；额度未知不估算，真实运行范围不扩大 |
| 2026-11-02 | Owner 澄清只做底座、上层以后再做；同意先指定一家执行与统一记录，授权修正同一计划后继续施工 | R2026-11-02.4；C01–C08 离线纵向实施及逐片自测/PR/双审/门禁合并，C09–C12 仅准备 | 跨厂商接力、自动选优/额度切换、复杂父子编排、竞赛与上层应用均后置；不扩大 live/部署授权 |
| 2026-11-02 | Owner 强调上下文跟任务走，并明确希望吸收 DeepSeek Super-agent 架构；多 Agent 自驱动提问只用于澄清 | 复核官方固定源码；将任务/派工分离、前后台分离、事件推进、持久上下文映射到现有 C02–C07 | 不授权每 Agent 无限循环、自动最佳厂商路由、DSH 专属业务工具或真实订阅调用 |
| 2026-11-02 | Owner 最新：把多 Agent 协作/DeepSeek 自驱动能力后置，先做好多 harness 聚合 | R2026-11-02.5 / R101；显式原生执行、统一任务记录与必要原生控制；调研保留 | 首期不做自主 Wake 派工、总管、协作策略、自动拆解或跨 harness 接力；不停止已授权聚合开发 |
| 2026-11-02 | Owner 在明确门禁说明后回复“过了就合并” | 重新核验准确 head、全部 CI、代码与安全明确结论后，依赖顺序合并 #29/#30；已执行并核实 | 不授权未验证的产品代码发布、部署或降低门禁 |
| 2026-11-02 | Owner 要求学习 T3/Multica，并接受 Claude 绕过权限询问；父级限定本轮本地文档准备 | R102–R104：公共契约、薄协议适配、逐 provider 证据与可选可见的 Claude 原生模式设计 | 不推送/提 PR/合并，不调用 Claude，不把工具审批跳过扩成取消提问、全局权限或未来高风险操作授权；C08 不换成 Claude |
| 2026-11-02 | Owner 要求多给架构图、明确目标并推进能推进的开发；父级继续本会话施工 | 恢复 C01–C08 中独立可离线验证的聚合产品切片，逐片源码/夹具/回归/质量和本地保存；远期助理方向留作上层 | 不因全仓 blocked 停止所有独立工作，也不越过全仓门禁发布；不扩大真实 harness、部署或应用层实施范围 |
| 2026-11-04 | Owner 明确“不用在意剩余额度”，继续同模型施工，并要求经常同步 GitHub、用小 draft/正式审查推进 | 覆盖旧 10% 暂停线及临时 local-only/no-PR；C01–C08 离线小片，受影响+质量通过可 draft，准确 head 全部 CI/两审明确后按依赖合并；父会话处理线上同步 | 不换 model/effort、不充值或迁凭据；C09–C12 仅准备；无真实 harness、正式中心、部署/发包、dispatch 或定时，不降低合并门禁 |

历史 Phase A/B 只覆盖原有范围。新任务实施授权到达后，在此新增日期、任务 ID 和准确范围。
无需重复确认已明确授权的同一范围；不能把范围外动作并入旧授权。

## 批次记录

### C00 — 2026-11-02 需求与规划基线

- 状态：完成（规划文档交付，非产品功能完成）。
- 输入 HEAD：`9de987f303e77a624adeb9b3feaceb2bb12c41ce`；初始工作区干净。
- 工作：固化 R01–R90；审计会话、能力、权限、执行、持久化；编排 C01–C18 和验证矩阵。
- T3 来源：本地 0.1.6 / `2359643cffa6`，摘要和文件 hash 见 AUDIT。
- DeepSeek 原型：来源版本未确认；不将已安装 App 视为同一原型。
- 改动：仅规划和文档入口，不改产品运行代码或数据库，不执行真实 harness。
- 输出候选：未提交；后续提交时补完整 SHA/PR，不以输入 HEAD 代替输出版本。
- 验证：90 条编号连续且完整；19 个任务行（含 C00）唯一，R01–R90 全部有覆盖映射；本地文档链接有效。
- Git 检查：ROADMAP 与 planning 文件未被忽略；`git diff --check` 通过。
- 质量：`make quality` 退出 0，报告 197 条 advisory risk signals；这些提示不代表新功能验收。
- 验证边界：未运行运行时代码回归、真实 harness 或跨平台测试；本次没有相应代码变更。
- 下一步：云端取得这些跟踪文件后做 C01；功能实施范围待 Owner 指定。

## 决策日志

| ID / 日期 | 决策与理由 | 影响 |
| --- | --- | --- |
| ADR-001 / 2026-11-02 | 采用 Owner 的基础设施定位；旧开发专用门禁保留在应用层 | R01–R08、R50、R72、R77；C02、C16 |
| ADR-002 / 2026-11-02 | 原生多轮交互和事件闭环优先，完整实例路由其次，对外契约稳定与业务分离最后 | R79–R83；任务依赖图 |
| ADR-003 / 2026-11-02 | 允许实现必要双向消息/请求送达，取代旧禁止 runtime mailbox 对这些需求的阻碍 | R21–R26、R35、R43–R59；不引入通用邮件/业务邮箱 |
| ADR-004 / 2026-11-02 | Git、候选、审查与发布不能成为普通任务必要条件；采用增量新路径和旧路径兼容 | R41、R50、R72、R77、R86 |
| ADR-005 / 2026-11-02 | T3 是行为与接口参考，暂不绑定服务；Center–Runner 权威结构保持 | R60、R88；不照搬默认共享工作区 |
| ADR-006 / 2026-11-02 | 路线图放根目录，细节放 Git 可跟踪 planning/，随任务持续更新 | 云端无需私人 docs/ 或本机文件 |

首批 harness、正式 API 路由和具体字段是设计建议，不在本表伪装为 Owner 已逐项批准。

## 待决事项与阻断

| 项目 | 当前事实 | 影响 / 继续方式 |
| --- | --- | --- |
| 新方向实施授权 | 原批 C01–C08 离线范围保留；本轮专项仅本地规划核查 | 本轮不发布；C09–C12 只准备，真实调用/部署不在范围 |
| 云端 HEAD 和环境 | 文档分支基于 main efa1ec1；C07 原检查点 be707b7 保留；专用环境仍 restricted / enforced | 不将工作区同步、配置声明或规划合并当作产品验证；当前受阻下载不重试 |
| 首批 harness 官方接口/订阅适用性 | Codex CLI 0.163.0 与 Antigravity ACP 1.4.5 离线基线固定，认证/真实能力未测 | 版本/schema/源码与真实调用分级；C08 仍优先 Antigravity，Claude 仅准备 |
| DeepSeek 原型版本 | 已确认 6337fc823ce20251c8de8085948165c81aaa701e | 静态审计完成；运行兼容和故障行为仍未测试 |
| 两种 harness 的真实验证 | 全部待测 | 不能据 mock/注册表宣布支持；按 VALIDATION 记录 |
| 全仓 Go 验证 | modernc.org/sqlite@v1.60.1 的官方下载重定向 storage.googleapis.com 后 Forbidden | 网络策略未生效变更；产品全仓 build/check/test 未通过，受影响夹具通过不能替代 |

以后每完成一个切片使用 VALIDATION 的模板追加记录，并更新主表、设计差异和验证证据。
长期维护表示相关工作交接时更新同一账本；未安排后台轮询、自动开发或定时通知。

## 后续规划修订 — 任务驱动 / R2026-11-02.2

- Owner 补充“任务驱动而不是用户对话驱动”，并提供 DeepSeek 原型的确切仓库。
- 新增 R91，保留 R01–R90；Task 是调度中心，Session 是资源，对话只是入口。
- 固定审计 DeepSeek 提交 `6337fc823ce20251c8de8085948165c81aaa701e`；采用建议见 TASK_DRIVEN 的 DS01–DS07。
- 解除“原型来源未知”缺口；不整包引入 DSH/Cordis，不借用其单仓库和隐式续工授权语义。
- 更新 C02 等现有任务的细节与 V16 验收，不改变前三阶段优先级，也不标记 C01 功能实施完成。
- 本次仍是资料分析和文档维护授权；未授权安装原型、调用模型或启动功能开发。
- 输出为未提交的文档更新；91 条需求及其覆盖映射、16 组验收场景、本地链接与 `git diff --check` 均通过。
- 本轮 `make quality` 退出 0，仍报告 197 条维护提示；未运行产品功能测试或真实 harness。

| ID / 日期 | 决策与理由 | 影响 |
| --- | --- | --- |
| ADR-007 / 2026-11-02 | Owner 明确任务驱动；任务生命周期与用户聊天、原生会话解耦 | R91、C02、C03、C05、C12、C13、C15、C17、V16 |
| ADR-008 / 2026-11-02 | DeepSeek 以固定源码做设计参考；整包/侧车集成仅为未来待评估方案，不是当前依赖决定 | R89、DS01–DS07；未增加运行权限 |

## 设计讨论 — 需求池 / 审查池 / 任务池 / 阻断池

- Owner 倾向只保留任务池和阻断池，并询问是否全部放入上层；本次记录为待决定建议，不代替最终选择。
- 建议：需求及业务审查归上层；底座保留任务调度、原生请求、等待和恢复事实，池作为投影。
- 已补 POOL_BOUNDARIES，明确一任务多原因、自动等待与人工处理分开、业务 hold 的权限边界。
- 重新查看固定 DeepSeek 版本的领域文档与 stage.ts，采用事实推导视图，避免照搬开发审查分类。
- 没有新增正式需求编号，也没有删除产品代码、页面、数据或门禁。
- 本次验证：规划链接与 `git diff --check` 通过；正式需求仍为 91 条；`make quality` 退出 0，报告 197 条维护提示。


## 已确认修订 — 任务与待处理 / R2026-11-02.3

- Owner 确认上一轮边界方案，选择“待处理”名称，并授权更新任务表；此前待决建议现已确定。
- 新增 R92：需求池/业务审查池归上层；任务调度和运行事实留在底座，以任务与待处理视图呈现。
- 待处理从来源事实推导，区分自动等待与需要介入；操作重验来源版本和权限，不建立重复 Blocker 状态机。
- 已更新 C02、C06、C11–C18 的交付/验收引用；C13 的任务/依赖事实保持原职责并纳入 V17。
- 新增 V17 分层验收，同步设计、视图边界、云端阅读入口及需求版本；保留原任务 ID 和历史批次证据。
- C00 为规划完成；C01–C18 仍为规划中。本次未改运行代码、页面、数据库或现有门禁，未调用真实 harness。
- 本轮验证：92 条需求及覆盖映射、19 个任务 ID/依赖/状态、17 组验收编号和本地文件链接均通过；`git diff --check` 通过。
- `make quality` 退出 0，报告 197 条维护提示；未运行产品功能测试。文件尚未提交或推送。

| ID / 日期 | 决策与理由 | 影响 |
| --- | --- | --- |
| ADR-009 / 2026-11-02 | Owner 确认“任务＋待处理”；需求/审查归应用，待处理为运行事实投影；无需另建通用阻断状态机 | R92、C02、C06、C11–C17、V17；保留旧开发门禁与证据 |

## C01 — 2026-11-02 只读复核与首个切片准备

- 状态：只读子阶段已交接；C01 整体仍为规划中，版本兼容、首批实例及真实验证未完成。
- 范围：Owner 本轮明确只读源码复核与方案准备；Antigravity 进入接入计划，不等于已实施支持。
- 本地输入：`9de987f303e77a624adeb9b3feaceb2bb12c41ce`，分支 `codex/review-protocol-recovery`。
  Owner 已有 4 个 tracked 文档修改，ROADMAP/planning 未跟踪；均保留，无 checkout/reset/fetch。
- GitHub main：`43f208327372fda8db94babda41e4a7d38a0e8dc`；与本地 tree 等价的 #28 head 比较有 42 个差异文件。
  本地缺 #27；补读 coordination 的记录/鉴权/游标路径，不声称完整复核全部最新主线。
- 输出：[C01_REVIEW](C01_REVIEW.md) 保存精确基线、源码入口、S/D/G 证据和 C02–C08 模块/接口/依赖/验收。
  复用现有事务、身份围栏、spool 和保守恢复；新 Task 无 Git/聊天前置，旧开发门禁保持。
- 适配建议：C07 优先 Codex app-server；C08 核验一个 ACP 实例，优先核验 Antigravity 官方服务端，dsh 保留候选。
  Antigravity 桌面 2.28.2 静态确认，常用 CLI/ACP 路径未找到；版本对应、认证和能力未实测。
  未读取凭据或调用私有桌面 RPC；没有开启 harness、模型、后台运行或实验。
- 接口资料日期：2026-11-02；当前官方文档与安装版本严格分开，源码/声明不标为 F/L。
- 方案次序：C02 固定最小契约，C03/C04 可独立推进，C05/C06 集成竞态，C07 后 C08 真实验证；
  稳定公共 API、完整路由及开发应用迁移仍归 C09–C18，不在这轮提前实施。
- 输出候选：文档工作区未提交/未推送；本轮没有产品运行代码或数据迁移。
- 验证：92 条需求及覆盖、19 个任务及前向无环依赖、17 组验收编号、81 个本地文件链接和文档空白检查通过；
  `git diff --check` 通过。`make quality` 退出 0，仍报告 197 条维护提示；比较基线是本地 origin/main，
  不是最新远端 main，不能用该结果声称最新主线已验证。
- 只读独立复核修正 3 类表述：目录探测复用身份而非长连接；基本围栏仍待实现；
  WakeIntent 可继续现有 Run/Turn，不能每次唤醒都创建新 Run。没有功能或真实 harness 通过结论。
- 本地辅助证据：`.coordica-acceptance/runtime/c01-20261102/` 保存文档检查摘要和质量日志；
  本文件与 C01_REVIEW 已包含交接所需事实，云端不依赖该忽略目录。
- 下一步：保留规划，完成固定基线和协议版本核验；收到切片实施授权后再进入 C02，不以方案自动启动。

## 云端实施批次 — 2026-11-02 准备与派发

- Owner 已追加云端实施和 PR 闭环授权；准确范围见上表及 [CLOUD_EXECUTION](CLOUD_EXECUTION.md)。
- 准备：C01–C08 聚焦 PR 包，C04/C06 可再按事件/效果、请求/投影拆分；C09–C12 先细化后续工作。
  C02 后 C03/C04 在稳定接口上可并行，共享迁移和最终集成串行；不双派工、不提前放宽阶段验收。
- 模型固定 `gpt-6.1-sol` / `ultra`；工具必须确认实际目标支持，不静默回退。
- 当前环境核对：已保存的可运行配置只有 poi-platform、trade-agent，没有 Coordica；未复用无关项目环境。
  原生工具支持列出/启动配置，未提供创建/修改环境接口。Codex App UI 访问被限制，网页版/Edge 操作超时；
  已请求 Owner 在原生界面发布绑定 `5intro/Coordica` 的专用环境，待其可用后继续派发。
- 本地工作区保留原状；先准备只含公开规划/规则的分支，基于最新远端 main，不上传私人 docs、日志或数据。
- 云端 environment/thread/host：尚未取得；不把准备任务或创建请求算作已运行。
- 规划分支：`codex/task-runtime-cloud-batch-20261102`；已基于远端 main 原子提交并创建
  [PR #29](https://github.com/5intro/Coordica/pull/29)，初始 head `27ca7f49ea58436bac6bc86d1a95afd76b9b1d33`。
  仅 14 个 Markdown 文件，README 保留远端 #27 增量；未修改本地 Git 元数据或上传私人资料。
- 初始 head 审查：已发布代码评论 6573752140、安全评论 6573752607；机器人在本轮触发之后分别明确报告无重大问题/无安全问题。
  安全汇总的 null 不作为通过依据，采用绑定该 head 的明确无发现回执。
- 初始 head 的三平台 CI 在打包 README 链接回归失败：新相对 ROADMAP 链接不在发行包内。
  已将该链接改为仓库页面；本地 `python3 -m unittest discover -s scripts -p test_package_release.py` 的 11 项回归通过，
  `git diff --check` 通过。更新后 head 的 CI 和两项审查须重新确认，旧通过不继承。
- 文档检查：92 需求、19 个任务/依赖、17 组验收、91 本地文件链接/空白检查通过；
  `make quality` 退出 0，197 条既有提示，本地比较基线与最新远端不同，不代替后者验证。
- 定时监控：创建被工具拒绝，返回“requires approval, but approval policy is never”；没有创建成功、没有监控 ID。
  不绕过审批直接写 scheduler 文件或另起 cron。本批监控若后续获准创建，不因第一个规划 PR 合并就结束。
- Owner 补充创建环境也连接失败，要求重试浏览器扩展；扩展可以连接并打开 Codex 页面，但进入 Cloud 仍超时。
  已请求错误原文和发生阶段；尚无环境发布/云端派发成功证据。
- 正式中心运行配置未改；没有配对、部署、模型测试或重新启动赛车实验。
- 原 PR 已更新到 `619061a5bacd48f7eda0801783587ea1d5cbb8af`；最新代码/安全触发评论为 6573926816 / 6573927206。
  本次最后观测：该 head 的三平台 push/PR 检查均运行中，两项审查也运行中；未合并，旧 head 的通过不继承。
- 本批尚无云端会话、已发布 Coordica 环境或成功创建的定时监控；不能承诺离开本轮后仍自动跟进。
  下一步先取得 Cloud 失败原文/阶段并恢复环境创建，以及解决工具的自动化审批限制，再实际派发和登记监控。

### 同日重试：环境创建与监控权限恢复

- Owner 要求再次创建 Coordica 云端环境；未扩大真实 harness 或正式中心运行范围。
- 重新完整读取环境配置，仍只有 poi-platform、trade-agent；没有绑定本仓库的 runnable 配置，未占用其他项目环境。
- 原生浏览器扩展可列出标签页，但读取旧 Cloud 入口返回 `Debugger unattached`；新建标签页后使用 AX 读取仍超时。
  另试应用内浏览器连接也超时。未获得新建/发布页面，无法确认环境创建；未使用私有 API 或绕过浏览器安全机制。
- 当前审批权限已恢复。通过原生 automation_update 成功创建本批 heartbeat：`coordica-pr`，ACTIVE，每 20 分钟。
  工具返回创建成功，并已通过 view 显示确认；前述“没有监控”的状态已被本次事实取代。
- 监控沿用本批原授权和固定门禁；环境发布后才用 `gpt-6.1-sol` / `ultra` 派发。
  保持至本批全部应提交 PR 合并且无遗留应提交工作，不因仅 #29 合并而删除；不宣称本地工具在关机后仍可用。
- PR #29 最新 head 仍为 `619061a5bacd48f7eda0801783587ea1d5cbb8af`。两项审查有绑定此 head 的明确无发现回执，
  未解决 inline threads 为 0；最新观测已有 Linux/macOS job 成功，但 Windows 及部分重复触发 job 仍运行，未合并。
- 环境和云端会话仍未创建成功；任务准备不等于已派发。正式中心、实验、配对和部署均未改变。

### Owner 纠正：定时续工归施工 Agent

- Owner 明确要求正在施工的 Agent 自己定时跟进审查并继续施工，不接受派发者/本对话的全局监控替代。
- 已通过原生 automation_update 删除错误绑定当前本地对话的 `coordica-pr`，工具确认 `deleteStatus=deleted`。
  这是 Owner 对监控方式的纠正，覆盖此前“保留至 merge”的这一个错误监控，不是把未合并工作标成完成。
- 已更新 CLOUD_EXECUTION：环境和施工会话创建后，由施工 Agent 在自身会话建立定时续工并核验绑定，
  沿用自测、PR、两项审查、修复和固定 head 门禁全过后合并；只在自身负责 PR 全部合并且无遗留工作时删除。
- 目前环境/施工会话仍不存在，因此没有创建替代监控。目的环境不支持云端定时唤醒时报告阻断，
  不改成本地 heartbeat、全局任务或常驻 sleep 轮询。正式中心与实验状态不变。

## C01 — 云端环境接收与离线协议固定（2026-11-02 UTC）

- 本节覆盖上文“环境/会话未创建”和旧定时安排的当前状态；历史记录保留，不当作当前事实。
- 父会话提供原生创建与页面证据：10:23 UTC 以 `model=gpt-6.1-sol`、`thinking=ultra` 创建并 started；
  thread `01a19bc9-6259-7731-91a0-6742b1bf8031`。10:24:36 UTC 原生页面选择器显示 `GPT-6.1 Sol Ultra`。
  容器无实际模型字段，初次据此等待；收到父级产品页面证据后解除等待，没有改模型或启动嵌套 Agent。
- 环境：父级 10:22:55 UTC catalog 证据为 `Coordica Cloud`，仅绑定 `5intro/Coordica` / main。
  运行时确认 provider=cloud、running/connected、observations_current=true、网络策略 enforced，未配置 secrets/身份。
  配置版本 `343491389455eaeb744fe7e39841a83014c1cfceb2e6d27b4f6b764d7366b976~cecfgver_6ae863cb84d481a09bd37680ccf79957` 与父级证据相符。
  工作区 `/workspace/Coordica` 的 origin 为目标仓库；未迁移本机凭据、数据或生产配置。
- 远端实采 main `43f208327372fda8db94babda41e4a7d38a0e8dc`；规划 #29 head
  `6325566dde16e80e079c5cb03e673f39aeff5992`，open/未合并，14 个 Markdown 文件。
  从该 head 接收完整基线 R2026-11-02.3，工作分支 `codex/task-runtime-c01-20261102`，tracked 初始干净。
- 已完整读取必读规划和仓库规则；相关最新源码复核、精确工具/协议版本、归档与 schema 摘要见
  [C01 云端固定基线](C01_REVIEW.md#6-云端接收与固定离线协议基线2026-11-02-utc)。
- 工具实测：Go 1.27.2 linux/amd64（官方归档校验后安装 `.tools/go`，gofmt 可用）、Node 24.19.0、Python 3.12.14、Git 2.52.0。
  系统 `/usr/bin/go` 不是本次使用的编译器；安装文件在忽略目录，不提交工具链。
- 固定首批离线目标：Codex app-server / 实测 CLI 0.163.0，以及 Antigravity ACP / 注册表 1.4.5。
  Codex 仅本地版本/schema 生成，Antigravity 未安装；均未认证探测、调用模型或执行真实工具。
  ACP v0.11.0 stable schema 和 Antigravity 注册表固定 commit/摘要；不静默选 dsh。
- `make quality` 在最新 main 比较基线上退出 0，输出 `maintainability-v2-advisory: no new risk signals`。
  这是文档切片和离线接口证据，不是产品多轮/审批通过；本轮没有改产品代码或迁移数据库。
- #29 当前 head 的代码/安全回执均由 `chatgpt-codex-connector[bot]` 明确无发现，分别为
  [代码 6574348981](https://github.com/5intro/Coordica/pull/29#issuecomment-6574348981)、
  [安全 6574384202](https://github.com/5intro/Coordica/pull/29#issuecomment-6574384202)，关联 `6325566dde`。
  10:25 UTC CI run `41425416974` 的 Ubuntu/macOS success，Windows in_progress，review threads=0；未合并。
  没有新增待修复意见；最终合并仍须重核当前 head 和所有检查实例。
- 当前监控归 Dot；本施工会话没有创建任何定时任务或额外监控。额度当前未知；收到可验证低于 10% 信号即安全暂停。
- C01 状态为待验证（离线基线已固定，真实实例/认证未测）。下一步提交本切片、触发双审并按新 head 核验；
  C02 的独立中立 task/input/wake 用例与 scoped identity/instance 可在本批继续准备，真实阶段出口不提前通过。

## 暂停后恢复 — R2026-11-02.4 的底座范围与最小架构

- 暂停收口时 branch 为 `codex/task-runtime-c01-20261102`，本地/远端均为
  `871c14db98cddcf2fa5a6222a3459e41f810eab5`，工作区干净，没有遗留测试/构建进程。
  仅推送四个文档修改；创建 C01 draft PR 被自动审批拒绝，理由是未识别开独立 PR 的明确授权，未创建、未绕过。
- Owner 此后明确同意修正同一计划并继续施工；父级重新明确逐片自测、draft PR、双审和固定 head 门禁。
  只依据新增授权证据重试先前被拒动作一次；仍拒绝则停止该动作并报告，不换工具。
- 父级原生会话继续指定 `gpt-6.1-sol / ultra`，同一环境版本 running/connected，网络 enforced；
  11:14 UTC 父级实读共享周额度剩余 45%，未触发低于 10% 暂停线；没有充值、重置或创建定时。
- 再核远端 main `43f208327372fda8db94babda41e4a7d38a0e8dc`、#29 head
  `6325566dde16e80e079c5cb03e673f39aeff5992` 均未变化，#29 仍 open。
  此次读得该 head 两次 CI 的三平台共六个 check run 全部 completed/success；两项明确无发现回执仍绑定该 head，inline threads=0。
- 新基线保留旧 92 条，追加 R93–R100：只做多 harness 底座；Task 持有上下文；调用方指定实例；
  Codex 最小纵向先行、控制原语随需集成、第二家验证；上层应用/跨厂商交接/复杂编排/自动选优后置。
  C15 基本接口不依赖完整 C13/C14，C18 底座出口不依赖 C16 应用迁移或 C17 完整界面。
- DeepSeek 证据：完整核读固定 `6337fc823ce20251c8de8085948165c81aaa701e` 的 000/001/007 文档，
  复核 core ports/intake/assignments、app runtime/effects 全文及 engine/tickets/executor/background 相关方法。
  原型未安装或运行；机制映射与已有/缺口、适用 C 任务和 V19 见 TASK_DRIVEN，不复制 DSH 专属工具和业务平台。
- 先完成本次文档验证与 PR；第一代码切片为 C07 最小离线原生端口，随后 C02 的持久任务上下文和输入。
  C03–C06 仍保留必要生命周期、事件、补充/中断、原生请求与可靠性；不先建全通用状态平台。
- 本次文档检查：100 条连续需求、19 个 C 任务、19 组 V 场景、75 个本地文件链接均通过；
  `git diff --check`、`make quality` 退出 0，后者报告 no new risk signals。没有把文档检查当作功能验证。
- #29 合并动作被自动审批拒绝：审查器认为当前授权只覆盖 review handling/fixes、未授权 merging，
  未识别父级传入的原有门禁合并授权。已知未执行，#29 仍 open；不绕过或重复合并，交父级处理授权证据。
- 已将 R2026-11-02.4 修订推送为 `8044a2b0ade9f71e091e4396418759a67bde556a`。
  依据本次新增授权证据重试 C01 draft PR 一次，自动审批仍拒绝，认为没有覆盖 separate draft PR 的新授权；已知未执行。
  没有创建 PR、触发审查或重试 merge；不通过其他工具绕过。代码可本地准备，外部 PR/合并动作分别保持 denied。
- Owner 后续关注多 Agent 协作：已明确每次 Run/Assignment 指定目标而非整个 Task 绑定单 Agent；
  同 Task 可计划多个显式执行者，独立 Session/输出、结果按 Run/代际回写和上下文 CAS，依赖由调用方明确推进。
  这些新接口仍是待实现计划，不据旧开发调度或原型源码宣称已支持；高级自动编排没有加入本批。

## C07 — 最小 Codex app-server 离线传输切片（本地检查点）

- 输入：C01 文档 head `0f8da8055605550145fcbe6bc005e7ce383c5c46`，R2026-11-02.4。
  产品代码位于独立分支 `codex/runtime-codex-peer-20261102`；不修改待审批的 C01 分支或 PR 目的地。
- 实现：`internal/agent/appserver` 为第一原生端口，显式绝对可执行文件/工作区/环境；
  仅 initialize/initialized 握手，保留原生启动信息，不自动登录、选模型、创建 thread 或启动 turn。
  显式 RPC 请求与乱序响应关联；结构化通知和原生提问/审批回调分开，连接身份/原文指纹拒绝旧回调和重复答复。
  答复只报告本地 queued，不伪造原生消费 ACK；请求超时或断连后结果 unknown，不自动重放。
  有界帧/调用/事件/原生 ID，慢消费者及协议故障明确失败并停止监督进程；保留实际 TreeStopped。
- 这只是适配层传输，不是完整 C07 或 C02–C06：尚无新 TaskContext 持久化、身份授权、绑定/Turn 代际、
  持久事件/请求对账或公共 runtime 入口；没有接到生产 Runner，旧 exec 驱动及开发门禁未改。
  封装协议差异但保留身份/进度/产物/审批/失败可观察性；完整 UI 不在本批。
- 离线证据：监督夹具子进程完成同连接两轮、resume 不重发、steer/interrupt 原生目标、
  数字/字符串请求 ID、跨连接/重复/改内容回调拒绝、乱序响应、无效握手/JSON/UTF-8、
  超时/丢响应/超大帧/慢消费者；没有调用安装的真实 Codex、任何模型/账号或上层业务。
- 回归：关闭 live opt-in，使用仓库 `scripts/test-go.py` 执行
  `-race -count=1 -timeout=3m ./internal/agent/appserver ./internal/agent ./internal/process`，退出 0；
  输出分别为 1.285s / 16.735s / 14.174s，监督器确认 owned processes stopped、temporary root removed。
  相同三包的 `go vet` 无错误，前端 `npm run typecheck` 通过。
  `make quality` 退出 0，有一项新 advisory：测试夹具 fakeServer 131 行；它是单个受控协议模拟器，未扩张生产职责。
- 构建：`make build check quality` 尝试先构建；前端 npm ci/build 成功（既有 bundle size 提示），
  Go 主程序构建因 `modernc.org/sqlite@v1.60.1` 下载被阻断，退出 2。
  官方 proxy.golang.org 重定向到 storage.googleapis.com 后返回 Forbidden；当前 enforced 允许列表不含该目标。
  未换非官方镜像、绕过代理或扩网络。全仓 build/check/test 依赖此项，保持未通过；不重复已知被拒的下载。
- 首次直跑 Go 测试和 npm 安装分别遇到默认 home 缓存只读，已将 GOCACHE/GOMODCACHE/GOPATH/npm cache 定向到工作区；
  这是运行环境修正，不修改产品或声称原失败通过。临时日志只在受控工作区/临时目录，不提交原始依赖 URL 查询参数。
- 发布/审查：本切片仅本地保存，未 push、未建产品 PR、未触发审查或合并。
  C01 创建 draft PR 的第二次拒绝和 #29 merge 拒绝仍保持 denied，由 Dot 请求准确动作授权；本会话没有重试。
  最近只读 #29 仍 open，故待确认 C01 base 仍为 `codex/task-runtime-cloud-batch-20261102`，没有默默改成 main。
- 下一步：先由父级处理明确 PR/merge 授权及官方 Go 依赖下载的网络配置；全仓门禁未过不能声称可合并。
  在同一纵向切片上继续 C02 的任务上下文/输入及显式 Run 绑定，而非铺完整通用状态平台。

### 同日更新：精准授权后 C01 PR 已创建

- Owner 对指定仓库/head/base、仅含文档的 C01 draft PR 请求直接回复“允许”；此次新证据只用于该动作，未扩大到 #29 merge。
- 创建前重新只读核对：C01 远端 head 仍 `0f8da8055605550145fcbe6bc005e7ce383c5c46`，
  base 分支仍 `6325566dde16e80e079c5cb03e673f39aeff5992`，#29 open/未合入，没有既存同分支 PR。
- 11:54:28 UTC 成功创建 [PR #30](https://github.com/5intro/Coordica/pull/30)，draft，base 为
  `codex/task-runtime-cloud-batch-20261102`，head 为上述 C01 SHA，9 个文档文件；没有混入本地产品代码。
  C01 分支未移动，本地 C07 源码保存为 `c106015b8d30b0805a2d5d3868a07d35f5ef00e2`，未推送。
- 明确触发[代码审查 6576496662](https://github.com/5intro/Coordica/pull/30#issuecomment-6576496662)
  和[安全审查 6576498142](https://github.com/5intro/Coordica/pull/30#issuecomment-6576498142)，作者均为 5intro。
- 11:55:58 UTC `chatgpt-codex-connector[bot]` 在[代码回执 6576519278](https://github.com/5intro/Coordica/pull/30#issuecomment-6576519278)
  对 `0f8da80556` 明确表示未发现重大问题。没有新增待修复代码意见；安全审查仍 running，null 不当作通过。
  该完整 head 当前有六个 CI check run：早一次 Ubuntu/macOS success、Windows running，另一次三平台均 running。
  尚无全门禁通过结论；#29 merge 没有重试，仍单独保持 denied。
- 测试收口只读核查发现 4 个 process.test 条目为 Z 状态、PPID=1，均已退出且不再执行，待环境 PID1 回收；
  没有仍在运行的本会话构建/测试进程。未杀其他进程或修改系统回收行为。

## 最新首期优先级 — R2026-11-02.5

- Owner 再次收紧：把刚讨论的多 Agent 协作/DeepSeek 自驱动能力放到后面，先做好多 harness 聚合。
  保留 R01–R100 历史，追加 R101；同一 ROADMAP、需求/设计/验证/执行文档同步，不另建规划。
- 首期只做明确选择原生 harness/实例执行、统一 Task/Thread/native Session/机器/工作区、进度与产物记录，
  以及必要输入/中断/原生审批/恢复/安全幂等。任务上下文归 Task，仍不要求 Git 或业务审查。
  协作接口、自主 Wake 派工、总管、自动拆解、复杂父子任务及跨 harness 接力均后置。
  V19 首期仅取任务上下文/版本/来源、单次 Run 归属、重复/迟到输入与回调围栏，其他研究场景不阻塞首期。
- 精准创建许可已成功落实为 [C01 draft PR #30](https://github.com/5intro/Coordica/pull/30)，
  head `0f8da8055605550145fcbe6bc005e7ce383c5c46`，base 仍为 `codex/task-runtime-cloud-batch-20261102`。
  该旧 head 的[代码审查](https://github.com/5intro/Coordica/pull/30#issuecomment-6576519278)与
  [安全审查](https://github.com/5intro/Coordica/pull/30#issuecomment-6576545331)均由 `chatgpt-codex-connector[bot]` 明确无发现。
  本轮更新原文档 PR 后必须重新核新 head CI/双审，不能继承上述旧结果；没有新增待修复意见。
- 产品代码在另一本地分支 `codex/runtime-codex-peer-20261102`，检查点
  `5d330a2372a3a226cef5e72e5d4911be7f114ba8`（代码提交 `c106015b8d30b0805a2d5d3868a07d35f5ef00e2`）。
  新包、既有 agent/process 的 race 回归通过；无自主 Wake 或协作代码，不混入 C01 文档 PR。
- #29 仍 open；合并的自动审批拒绝未被本次 PR 创建许可覆盖，没有重试，也不默默更换 #30 的 base。
  全仓 Go 构建仍因官方模块下载跳转的 storage.googleapis.com 不在 enforced 允许列表而 blocked；没有绕过。
  不创建定时，真实订阅 harness 未调用，低于 10% 的可验证额度信号仍立即安全暂停。

### 首期范围更新与下载拒绝事实

- C01 原 PR #30 已更新为 `bf098f88db5b8128b9d29b4bdb3d5ce7b566a64b`，仍是原规划 base、9 个文档。
  明确重新触发[代码审查 6576657989](https://github.com/5intro/Coordica/pull/30#issuecomment-6576657989)与
  [安全审查 6576659241](https://github.com/5intro/Coordica/pull/30#issuecomment-6576659241)；没有在其 running 时重复触发。
  旧 `0f8da80` 的双审和 CI 不继承。父级 12:04:55–12:05:23 UTC 观察该新 head 双审及六个 CI 均运行中，无可行动意见。
- 本地代码分支合入上述文档优先级；PROGRESS 的两个追加记录保留后串行解决冲突，无代码或历史丢弃。
  下一代码工作仍只做 Codex/ACP 薄适配的共同传输与控制验证，不接自主调度、Wake 或协作策略。
- 下载原始错误（仅将签名 URL 的查询参数省略）：
  `modernc.org/sqlite@v1.60.1: Get "https://storage.googleapis.com/proxy-golang-org-prod/4637baafbecfb238-modernc.org:sqlite-v1.60.1.zip?[query redacted]": Forbidden`。
  随后 `make: *** [Makefile:9: build] Error 1`，整次 make 命令退出 2。
- 当前 `/etc/codex/network-policy.json` version=1、restricted，egress_rules 的键只有 host，缺 storage.googleapis.com；
  运行时状态确认策略 enforced。该域在当前配置下未获允许，是硬网络边界；仅凭 Go 的 Forbidden 不能区分具体 403 产生层。
  这不是命令审批器的 approval rejection，也没有已获准的临时网络例外。
- 可供用户确认的最小配置请求：仅增加 HTTPS `storage.googleapis.com`，用于官方 Go 模块 ZIP 分发，
  保持现有代理/CA、其余 allowlist 与权限不变。现有观察到的策略粒度是 host，不能声称只授权上述单个 ZIP 路径。
  本会话只有 environment_status/diagnostics/read_environment_diagnostic 读取工具，没有配置变更工具；
  须经受支持环境配置流程及用户确认，能否热应用未验证。未修改网络、试其他域/代理/缓存或重新发送被拒下载。
- 父级 12:03 UTC 实读共享周额度剩余 44%，未触发低于 10% 暂停线；没有重置或充值。

## #29/#30 合并与 main 回执（已完成动作）

- Owner 新的“过了就合并”授权到达后，重新核准精确 head、六项三平台 CI、明确代码/安全回执和未解决线程。
  [#29](https://github.com/5intro/Coordica/pull/29) 于 12:56:32 UTC 合并，merge
  `2f3e23b7d2ff88147ec23a10ec81c3e5484e530d`，候选 `6325566dde16e80e079c5cb03e673f39aeff5992`。
- #30 retarget main，head 始终为 `bf098f88db5b8128b9d29b4bdb3d5ce7b566a64b`，仍为 9 个文档，无产品代码。
  原 base 与新 main 的完整 tree 均为 `4d276456b291c44f26054cee395d454681074aeb`；
  两份 compare 的 9 文件/blob/增删/完整 patch 逐项相同。
  已独立核对[明确安全回执 6576698059](https://github.com/5intro/Coordica/pull/30#issuecomment-6576698059)
  覆盖该同一 head、同一内容；后轮 Completed/null 不用作通过。原生报告链接未能打开，不声称读到完整报告。
  最新[代码回执 6578016883](https://github.com/5intro/Coordica/pull/30#issuecomment-6578016883)明确无发现，
  作者与安全回执均为 chatgpt-codex-connector[bot]；六项 CI success，未解决线程 0。
  适用性证明已保存在[覆盖记录 6578267081](https://github.com/5intro/Coordica/pull/30#issuecomment-6578267081)，非自行批准 review。
- [#30](https://github.com/5intro/Coordica/pull/30) 于 13:24:23 UTC 合并，merge/main
  `efa1ec1f8191b5fd4530cde6678d5b2e05fc4b58`；完整树与已审候选一致，原分支保留。
  本轮再次只读核验两个 PR 均 merged，并确认该 main 的
  [run 41452700790](https://github.com/5intro/Coordica/actions/runs/41452700790) 三个平台 job 均 completed/success。
  以上只证明文档候选与 main CI，不是 C01–C08 产品或真实 harness 验收。

## 公共契约与 provider 规划准备 — R2026-11-02.6（仅本地）

- 输入：公开 main `efa1ec1f8191b5fd4530cde6678d5b2e05fc4b58`、R01–R101；新增 R102–R104，保留全部旧编号。
  分支 `codex/provider-contract-plan-20261102`，独立 worktree，保护 C07 原分支 `be707b7263bb98fa98c7a58fb660c084bdc206a5`。
- 规划：沿用同一 C 表/现有 V 场景；C02/C07 的窄公共控制契约、C08 通用 ACP 与 provider 配置分层，
  每家能力/版本/声明/fixture/真实证据独立。完整能力发现仍在 C09，不引入通用推理循环或自动调度。
  Claude 原生 bypassPermissions 记录为显式、可选、可见配置，提问回传独立；不禁用 AskUserQuestion。
- 来源：复核 T3 `47f2c8d29e981c7723467efe137ee19e91df924e` 的接入指南/公共契约及相关 ACP 实现，
  Multica `e8b6a97643ccc35efbcf2a4675c8d088a288ed6e` 的 Backend/Session 与 Codex/Claude/Kimi 相关实现；
  固定 LICENSE 与 Claude 官方文档证据见 AUDIT。没有复制第三方代码、安装依赖或运行参考产品。
- C07 历史离线证据仅作交接：此前 appserver/agent/process race 回归、scoped vet 和前端 typecheck 通过；
  最小传输尚未接生产 Runner，也没有 Task/权限/持久请求完整链。本轮未重跑产品测试，不将其列为新通过。
  全仓构建此前被 SQLite 模块下载阻断，本轮没有重试；当前实例策略仍不允许目标域。
- 父级 15:59 UTC 实读共享周额度剩余 44%，未触发低于 10% 暂停线；同一原生 Sol/Ultra 会话继续，未换模型。
  不创建定时/额外监控、不重置充值，不迁移凭据、不部署发包或启真实 harness；中心暂停、自动派发关闭。
- 文档验证：104 条连续需求、19 个唯一 C 任务、19 组 V 场景、全部需求责任映射、78 个本地链接与新增锚点通过；
  同时核对 C01 仍待验证、C02–C08 仍规划中、C08 保持 Antigravity 优先。`git diff --check` 通过。
  以 main `efa1ec1f8191b5fd4530cde6678d5b2e05fc4b58` 为固定 QUALITY_BASE，
  设置已有官方 Go 1.27.2 的 PATH、GOTOOLCHAIN=local、GOPROXY=off、GOSUMDB=off 后执行 `make quality`，退出 0，
  输出 `maintainability-v2-advisory: no new risk signals`；只执行本地文档/源码清单检查，没有下载依赖或测试产品能力。
  CLOUD_EXECUTION 的审查说明同步为实际门槛：明确结论必须覆盖当前 head/内容，null 不通过，不能声称读过不可读报告。
  未降低代码/安全双审或三平台 CI 要求；本轮没有 PR/合并动作。

| ID / 日期 | 决策与理由 | 影响 |
| --- | --- | --- |
| ADR-010 / 2026-11-02 | 公共消费方契约、薄协议适配、同协议复用与逐 provider 能力证据分层 | R102–R103；C01–C08、C09 准备、C15；V01、V03–V06、V14、V18 |
| ADR-011 / 2026-11-02 | Owner 接受 Claude 可选跳过工具审批；所选/生效模式可见，与原生提问及底座权限分开 | R104；后续 Claude 准备，C08 仍 Antigravity；不更改现有驱动或当前云端权限 |

## 恢复离线实施 — C07 原生目标与控制操作

- Owner 要求明确架构目标并推进可推进的开发。远期个人助理/项目与知识管理方向已明确，
  当前仍先做好多 harness 聚合；不因此实现助理业务、知识图谱、多 Agent 总管或全多节点体系。
  官方原生 harness/订阅路径优先，不提取凭据或做反代，也不承诺零封号风险。
- 独立产品分支从 C07 `be707b7263bb98fa98c7a58fb660c084bdc206a5` 建立，合入本地 R6 文档。
  合并命令首次误落在原工作区，只造成未提交的文档冲突；核准 HEAD/MERGE_HEAD 后撤销该次合并，原分支恢复干净。
  随后在独立工作区串行保留双方规划和历史，merge `3940f23`；没有 reset/rebase 或丢弃产品源码。
- 源码：appserver 增加 thread/turn 窄控制端口；明确 model/approval/sandbox 和原生生效配置，已知不符拒绝执行。
  Thread/Turn 目标为连接内不可变句柄；resume/read 不重放，steer 固定 expectedTurnId，interrupt 不暗建下一轮，
  RPC 成功不等于 Turn/后台停止。无 Git 目录与符号链接工作区可用；无效原生响应保留 unknown 并停止连接。
- 这不是 C02 的 Task/Run 持久化、跨进程绑定代际或完整 C03–C06。未接生产 Runner，旧 exec 未改，
  原生推理/工具仍归 harness；原生 options 支持范围只是当前 typed 子集，其他能力不伪装为已支持。
- 夹具：手写 schema-informed 受控子进程核对原生字段/目标和实际操作次数，覆盖两轮、恢复不重发、
  stale steer/interrupt、模型/工作区不符、错 ID/缺失结果、符号链接及无 Git 路径；不是原生录制或真实调用。
  关闭 live opt-in、GOPROXY=off/GOSUMDB=off，仓库监督脚本执行 appserver 包
  `-race -count=1 -timeout=3m`，退出 0，1.564s；owned processes stopped、temporary root removed。
- 下一独立切片：原生问题/工具审批的 typed 目标与答复校验，以及 resolved/Turn 结束后的回调失效。
  全仓依赖下载仍 blocked，不重试，不推送/建 PR/触发外部审查，真实 harness 全部待验证。

### C07 相邻切片 — 原生请求答复与连接内失效围栏

- 上一代码切片已保存为 `824db3e`；当前继续同一独立产品分支，原 C07 `be707b7` 和文档 `1189e10` 未改。
  父级 16:30 UTC 实读共享周额度 43%，未触发 10% 暂停线；架构图由父级完成，不在本片扩大应用层。
- 源码：Turn.Request 仅接受同一连接/thread/turn 的真实已登记回调，分问题/命令审批/文件审批；
  保留原生问题 ID、选项、secret/other/blocking 事实和原始参数的防御性副本。
  实验性问题须显式协商；答案集合精确对应原生问题；审批仅转发原生选项，缺列表时将固定 schema 基本值标为 schema_default。
  不自动批准、取消问题工具、扩权限或创建下一轮；答复仍只返回本地 queued，不能当作原生已消费。
- 失效：明确 serverRequest/resolved 在发布事件之前关闭对应回调，数字/字符串 ID 分开；
  未见原请求的 resolved 也保留有界 tombstone，阻止迟到 callback 复活。
  上层在结束/撤销执行授权时显式 InvalidateRequests；连接内 pending 和迟到回调都不再可答，目标 tombstone 上限 1024。
  原生 Turn 完成不证明后台工作已停，因此不单凭 turn/completed 关闭所有请求；修正上一段拟议的笼统失效条件。
- 声明边界：这是协议连接内保护，不是持久 PendingRequest、当前主体授权、跨重启代际或完整 C06。
  这些仍依赖 C02 持久任务/Run/实例/授权账本、C04 事务事件及效果；不以额外内存状态机替代 SQLite 事务。
  尚未接生产 Runner，没有新增公共 runtime API 或生成契约，因此本片不产生 API 生成漂移。
- 夹具：同一监督子进程核验问题/审批真实线协议字段与计数；覆盖相反/并发重复答复、错连接/目标、内容变化、
  非原生选项、schema 默认值、experimental 未开、未知方法、resolved 未消费、迟到回调、显式撤销、容量及中断无终态。
  另验证 Turn 完成不等于原生请求已解答或后台停止；仍为手写模拟帧，不是 native transcript 或真实 harness。
- 首次回归有两项失败：实际缺陷为 JSON null 被 Go string 解码为空值，已改为只接受原生字符串/对象决策，
  对象先做重复/null 校验并保留数字精度；测试故障为把 JSON 空白变化当内容篡改，已改为更改实际问题文本。
  补测 appserver race 退出 0（1.537s）；没有把原失败忽略或降低行为标准。
- 受影响回归：关闭 live opt-in、GOPROXY=off/GOSUMDB=off，通过仓库监督脚本执行
  `-race -count=1 -timeout=3m ./internal/agent/appserver ./internal/agent ./internal/process`，退出 0，
  分别 1.841s / 17.271s / 14.148s，owned processes stopped、temporary root removed。
  新补未观察到原请求的 resolved 场景后，appserver race 退出 0（1.528s）；
  增加不同问题答案及相反审批决策的并发唯一答复后再验，退出 0（1.536s）；两次监督清理均确认完成。
  同三包 scoped `go vet` 退出 0。`make quality` 对 main 固定基线退出 0；
  先报两处 fixture 分派函数和一处选项列表长行，已将列表自然分行，测试 fixture 保留单一受控协议状态机职责。
  最终 quality 仍退出 0，仅保留 controlFixture.handle 143 行、fakeServer 139 行两处测试模拟器 advisory；
  未新增生产模块混杂职责，不为数字拆成转发框架，不把 advisory 当作自动合并批准。
  文档检查：104 条连续需求、19 个任务/19 组场景、完整责任映射、78 个本地链接通过；
  gofmt、工作区及 staged diff 空白检查通过，原 C07 与 R6 文档检查点均核实干净且未改变。
- 全仓 build/check/test 仍保持此前 SQLite 下载阻断，未重试原下载、换域/代理/缓存或执行器。
  当前只本地保存；无 push、产品 PR、外部审查、合并或真实订阅消费。C01 与真实双 harness 出口不标完成。
- 下一依赖边界：C02 的持久 Task/Run/授权与最小事务入口仍需要 SQLite 模块，C03–C06 的跨重启/事件效果/输入队列/请求账本依赖它。
  C08 的适配代码按既定顺序需 C07 稳定契约与完整离线门禁；本轮不越过未满足依赖或堆未接用的通用平台。
  已推进连接层可独立回归的控制与回调两片；后续先解除官方环境策略生效及依赖验证阻断，再继续持久集成。

## 接续后 C07 — 成功响应解码失败围栏（2026-11-03 UTC，本地离线切片）

- 接收并独立验证私有源码交接：输入固定 `c8c45862e7571a77d55ac5e148b8b35bdce874f1`，
  base `efa1ec1f8191b5fd4530cde6678d5b2e05fc4b58`；原 C07、R6 规划和产品三个分支保持原 SHA。
  R6 `1189e104b02c8fde0adaa94a2c0ce4c4a2bb1e23` 已为产品祖先，没有再创建 merge 或重写旧实现。
  本片工作分支为 `codex/c07-response-result-fencing-20261103`，原始检查点独立保留。
- 先在未改生产代码的输入上增加手写 fake 子进程回归：`turn/start`、`turn/steer`、`turn/interrupt`、
  `thread/read` 分别返回合法 envelope 下的数组或标量成功结果。八个子例均真实复现缺口：
  只返回 `ErrProtocol`、Peer 仍可用、显式 Close 前后续请求确实写入 fixture journal、其他 pending 未被释放。
  red 监督退出 1，包耗时 8.346s；明确 RPC 拒绝与发前取消两个对照通过；进程停止和临时根删除均确认。
- 最小生产修复仅在 `Peer.applyResponse`：明确 `RPCError` 优先原样返回；调用方要求解码的成功结果
  无法解码时复用 `invalidResult`，关闭连接并返回 `ErrOutcomeUnknown` 与 `ErrProtocol`。
  三个响应等待分支统一调用该方法；未改 pending map/通道生命周期、原生调用目标或 `nil result` 的忽略语义。
- green 对完整 `./internal/agent/appserver` 跑现有及新增夹具，监督退出 0，包耗时 1.472s，
  52 个测试/子测试 pass 事件、无 fail/skip；所有权监督确认进程停止、临时根删除，无残留测试根。
  关键断言均先于测试显式 Close：unknown/protocol、连接失效、后续 Call 被拒且 journal 不再增加、
  其他 pending 有限退出；随后另以 Close 的 `TreeStopped` 确认实际进程树停止。
- 两轮均用官方 Go 1.27.2、`GOTOOLCHAIN=local` 和已验证模块缓存；三个 live/catalog opt-in 从子环境删除，
  `GOPROXY=off`，`GOMAXPROCS=1`，监督上限 600s、空闲门槛 8GiB、临时根上限 4GiB。
  Go 参数为 `-json -p=1 -count=1 -timeout=3m`；red 仅选上述回归/对照，green 跑完整本包，均非 race。
  本协议夹具包已只读确认不要求 Git 祖先中立；仅本包获准使用 ignored 临时根，未削弱 mcpconfig 守卫。
  本地原始证据在 `.cache/c07-response-fencing-lau0d937/{red,green}/`，各含命令环境、JSON 测试日志和结果回执。
- 当前只证明此连接层源码与手写离线夹具；不是 native transcript、真实模型/账号调用或生产 Runner 接入。
  C01 的真实实例/认证、C02 的持久 Task/Run/权限和 C03–C06 完整控制链仍未完成。
  本片仅本地保存，未 push 或创建 PR；当前候选的适用门禁、完整门禁和独立代码/安全审查未完成，不能据此发布或合并。
  正式中心继续暂停，自动派发关闭；未部署、发包、启动赛车实验或创建定时任务。

## C02 — 最小持久任务账本（2026-11-03 UTC，本地离线切片）

- 输入固定为 C07 修复 `e47c435a1fe21be30f5bfed11b1c728fccfbb29b`；独立分支
  `codex/c02-task-ledger-20261103`，不重写旧 C07，不提前实现 C03/C04 平台。
- 新 `internal/runtime` 提供 experimental in-process Ledger：Task 提交/读取、完整上下文快照更新、
  显式 Run 登记/读取、固定 Run/代际的 continuation 记录，以及本地取消和输入读取。
  Task 只固定 App；每个 Run 单独保存 instance/config、node、harness、model、effort、授权版本和不可变 ContextID。
  `registered/not_started`、`recorded` 都不是原生启动、送达或消费；取消不伪造原生停止证明。
- Manager 仅是可信宿主已持 Store 的配置能力，不暴露给旧 Owner/node/chat 或应用路由。
  管理主体显式登记 App/actions/instance-node scopes；Ledger 校验当前持久 token hash/权限及 ownership，
  不把调用方填写的 Principal 或 Scope 当认证。App/instance/node/harness 归属不能覆盖换绑。
- 复用现有 SQLite records 主键与事务，不改 schema 或旧开发 Task/Run。
  独立 `runtime.*` kinds 不进入旧调度器；当前权限和目标 scope 先于 Replay，首次写入才做 CAS/状态检查。
  source 以 app/source/event/task 确定性复合 ID 去重，异内容/目标冲突，显式多 Task 扇出保留。
  精确历史回执不重复写状态或审计，不授予将来执行权限；状态、输入、audit/outbox 通知与回执同事务。
- 真实 SQLite 主链路 Submit→RegisterRun→关闭/重开→ReadTask/ReadRun 首次通过（包 0.018s）。
  初轮 nonrace 的 runtime/store 共 23 个测试/子测试 pass；补充跨重启取消/回执及 Run 回滚检查后，
  同两包 race 再通过：runtime 2.013s、store 4.760s，23 pass、无 fail/skip。
  覆盖撤权后缓存拒绝、跨 App/目标、配置版本、来源幂等及扇出、不可变旧快照、旧 Run/代际拒绝、
  CAS 唯一胜者与最后 Remember 失败下六种 mutation 的完整回滚。所有监督回执确认停止/清理，无残留。
- 验证沿用官方 Go 1.27.2/`GOTOOLCHAIN=local` 和已验证缓存，三个 live/catalog opt-in 从子环境删除；
  `GOPROXY=off`、`GOMAXPROCS=1`、`-p=1 -count=1 -timeout=3m`，监督 600s、8GiB 空闲/4GiB 临时上限。
  本片不依赖 Git 发现；本地证据在 `.cache/c02-ledger-v9pki8jx/`，测试命令、源码快照、日志和回执分阶段保存。
- 下一最小依赖是消费此账本的显式会话/Run 控制端口：再按实际需要落实 C03 绑定/单写/重启 unknown 围栏，
  C04 事务事件/效果仍留在其原任务，不将旧 outbox 通知当可靠执行效果。
  当前没有 native Session、harness 执行、Wake/dispatch、HTTP/CLI/MCP 路由或生产 Runner 接入。
  本片仅本地保存；完整门禁、正式代码/安全审查和真实 harness 未通过，未 push/创建 PR/合并。

## C03 — 显式会话绑定与 unknown 围栏（2026-11-03 UTC，本地离线切片）

- 输入固定为 C02 `62495534b2921b873c4174c09cef8b0ed0fabd3c`；独立分支
  `codex/c03-session-binding-20261103`，原 C02/C07 及三个交接 refs 保留，不扩展 C04 事件平台。
- 新窄接口是 PrepareBinding、显式 Produce、仅账本 ReadBinding、已知闲置自有绑定 Release 和
  显式 RecoverUnknown。宿主构造 Producer 不改账本或调用 native；Create 输入不含 Task/context 正文，
  不调用 Turn/start。没有生产 C07 接线、原生 read/resume、忙碌/unknown 资源调解或自动恢复。
- `session.control` 必须明确授予，旧 run.register 不含原生权限。固定 Run/授权版本、目标/config/context、
  profile/workspace/protocol/mode 引用；当前授权和 scope 先于 replay，native I/O 在 SQLite 事务外，
  pending command/owner 先落盘，完成时再验当前权限和 owner/代际。重复/并发调用不会再次 Create。
  新 Producer 或重开 DB 不接管旧资源；RecoverUnknown 仅推进围栏，迟到结果留在原 command，不推进新 binding。
- 逻辑 Task/Run/Binding 与 provider native ID 分开保存；观察到 ID 但配置不符、撤权或确认写入失败，
  保留原 command 的观察和停止回执，控制状态为 unknown，不静默回退或重建。
  已 pending/bound/unknown 的 Run 不能走旧 C02 本地取消捷径；Release 只有匹配实际 TreeStopped 后
  才记录 released/session_stopped，保留 Task/context，与 Task 取消分开。
- 真实 SQLite 和计数 port 验证准备回滚不调用 native、确认事务失败后不重发、并发单写、重启不调用、
  旧 owner/代际迟到围栏、撤权、配置不符、明确拒绝/发前取消，以及缺停止证明不得释放。
  自有 fake 协议子进程另验证 Create/Stop 不持 DB 事务和真实进程树停止；这不是原生 transcript 或真实 harness。
- 只读预审发现明确拒绝后安全清理的 stop proof 仅在内存校验、未持久的问题：新增重开 DB 断言，
  初次测试调用签名编译错后修正，随后真实 red 复现 CleanupStopped=false/command.Stop 为空。
  最小修复在确认事务内重验并保存 proof；重开后 unbound/not_started 与持久停止证据一致。
  修复后的 runtime/store nonrace 与 race 均退出 0，各 37 个测试/子测试 pass、无 fail/skip；监督停止/清理确认。
- 沿用官方 Go 1.27.2、`GOTOOLCHAIN=local`、已验证缓存和 GOPROXY=off；三个 live/catalog opt-in 删除，
  GOMAXPROCS=1、-p=1、3m 测试/600s 监督、8GiB 空闲/4GiB 临时门槛不变。
  分阶段原始命令/源码 hash/日志/回执位于 `.cache/c03-binding-20261103/`，不把局部检查代替完整门禁。
- 本地源码和离线夹具切片；完整 mcpconfig 中立 Git 临时环境项仍阻断，未削弱 guard 或重试只读目录。
  正式 GitHub 两审、真实 harness/C01 出口未通过，未 push/创建 PR/合并/部署；中心暂停、派发关闭。
  下一最小片仍是 C03 的显式原生 read/resume 与 C07 薄接线及固定身份验证，不自动补发工作或扩大 C04。

## C07/C03 — 显式 native read/resume 薄组合（2026-11-03 UTC，本地离线切片）

- 输入固定 C03 `2e2107f73519bd85faddd3407bf3a7db22083753`；独立分支
  `codex/c07-binding-read-resume-20261103`，保留 C03/C02/C07 及原三个交接 refs。
- `runtime.SessionPort` 是消费方窄扩展，独立 `internal/runtimecodex` 组合 appserver 协议，
  agent 层不反向依赖调度实体。宿主显式选择 executable/env/directory 与固定 profile/config/workspace 引用；
  构造不启动进程或发现/迁移凭据。引用是宿主选择，不是原生认证、实际 effort 或 OS 隔离证明。
- ResumeBinding 只接受已 released 且有匹配持久 TreeStopped 的原生 ID：事务先记单 command/owner/新代际，
  DB 外 ResumeThread，再核当前权限/Run/config/owner/代际。相同绑定复用自身 native 索引，foreign 索引不改。
  错 ID、配置不符或断连观察保留在原 command，不能覆盖已验证的原 resume 目标，不能 fallback create。
  同键/并发不重复 RPC；unknown 或仍未停止的旧 owner 不允许用 resume 接管。
- ReadNative 只用当前 owner 的 ready 连接，read 不 Open/resume。当前 session.control 与完整绑定围栏
  在 I/O 前后都检查，成功和错误路径均覆盖；撤权/迟到时不返回私密 snapshot 或可被 errors.As 取得的 RPCError 数据。
  raw history 不进入 TaskContext、通用 audit、idempotency 或 command；command 仅留操作结果/观察/停止元数据。
- OpenWithLifetime 仅分离宿主进程 lifetime 与单次 initialize/RPC ctx；成功后取消操作 ctx 不杀 ready Peer。
  请求入队后取消仍 fail Peer 并 Unknown，不放松旧围栏。Stop 从 Peer.Close 取真实 TreeStopped；
  Open 失败返回 nil Peer 时不靠“看不见进程”捏造停止证明，保守 unknown。
- 真实 SQLite 与 owned Go JSON-RPC 子进程验证 Create→Read→Release→关闭重开 DB/新 host→Resume 同 ID→Read；
  journal 为 thread/start=1、resume=1、read=2、turn/start=0，Task 正文未发送。
  另覆盖配置/返回 ID 不符及原生观察、断连、明确 RPC 拒绝、发前/写后取消、并发唯一 resume、
  prepare 最后 receipt 回滚 0RPC、foreign native 索引拒绝、Read 成功/错误期间撤权和 recovery 迟到不泄私密材料。
- 首次扩大测试快照后，runner 漏同步扩大结束后的文件集合，原始测试 98 pass 但编排 assertion 失败；
  原日志保留于 matrix-nonrace，没有将其当正式通过回执。修正 runner 后 matrix-green 与 matrix-race 均退出 0，
  各 102 个测试/子测试 pass、三包 pass、无 fail/skip；源码前后 hash 相同，owned 停止/临时根删除确认。
  scoped vet 与 quality(base efa) 退出 0；维护提示按语义审查，不把数字 advisory 当自动功能/合并证明。
  分阶段命令、选定 env、源码 hash、日志与回执在 `.cache/c07-binding-20261103/`。
- 最终安全预审确认 P2：共享 Create/Resume 在撤权/旧 owner 后仍 Join 原生 RPCError，
  cleanup 无证明时还会跳过 finish 的二次鉴权。旧候选 799e 未提交；新增四个私密 sentinel 子例真实 red，
  覆盖已证停止后撤权/Recover、initialize 失败 nilPeer、私密 cleanup 错误链，5 个 fail 事件与停止清理均记录。
  最小修复在全部错误出口、cleanup/quarantine 后独立重验当前权限/target/owner/代际；
  自身 unknown 状态推进不当作外来失效，仍授权的明确拒绝保留原分类，失效仅返安全 fence/Unknown/StopUnconfirmed。
  原生观察留原 command；不把私密错误对象经 Join/As 给旧调用者。修后 private-error-green/private-error-race
  各 107 个测试/子测试 pass、三包 pass、无 fail/skip，停止清理确认；scoped vet/quality 再退出 0。
- 沿用官方 Go 1.27.2/GOTOOLCHAIN=local/已验证缓存，三个 live/catalog opt-in 删除、GOPROXY=off，
  GOMAXPROCS=1、-p=1、3m 测试/600s 监督、8GiB 空闲/4GiB 临时门槛不变。
  仅手写离线协议夹具；没有真实账号/订阅 harness、生产 Runner/public routes、Turn/输入执行或 C04 事件消费。
  unknown 资源调解与忙时策略仍未完成，不把 native read/resume 源码接线记成真实生产接入完成。
- 本地保存；全仓 mcpconfig 中立临时目录环境项仍 blocked，不重探/重试/删除共享标记/绕 guard。
  完整门禁与正式 GitHub 两审未完成，不 push/创建 PR/merge/部署；中心暂停、派发关闭。
  下一最小片是 C04 必要结构化事件接收/游标与 C03 活跃资源事实的纵向整合，仍不引入自主派工或完整事件平台。

## C04 — 受控事件事实与持久游标（2026-11-03 UTC，本地离线切片）

- 输入固定 `6ea2fa6a20b1ae9797d551bb7ee32545a0d1cee7`；独立分支
  `codex/c04-runtime-events-20261103`，保留所有原交接/C02/C03/C07 检查点。
- 窄接口 OpenEventStream/AcceptEvent/ReportSourceEnd、OpenEventCursor/ReadEvents/AckEvents 与显式 DrainEvents。
  stream 固定 App/Task/Run/Binding/nativeID/owner/gen；只存 typed 状态、计数与原生目标/请求引用，
  不存文本、问题正文、raw history 或任意 JSON；事实不授执行权限，不把 execution.ended 当 Task 业务完成。
- Store 增加兼容 schema-v1 的独立 runtime_events 表；旧 records/调度查询不改。
  app/stream/local sequence 复合主键及索引做有界 keyset，eventID/sourceSeq 分别 SQL 唯一，digest 覆盖不可变事实。
  每流 sequence 不暴露其它 App 的计数；event/stream/dataset 计数、audit/outbox 同事务，无高频全 JSON 扫描。
- DatasetEpoch 随 DB 持久，重开保留；SourceEpoch 取具体 Peer 初始 Create/Resume commandID，
  后续 Read/Release 不能改它。Codex 无原生 eventID/seq，明确标 local_observed；相同 delta 仍是两条观察，
  Resume 另建 source epoch，旧代次 cursor 显式 expired；不声称原生持久 spool 或跨重连 exactly-once。
- 历史 source receipt 与客户端 cursor 主体分开：前者同 App、当前所需 action/target scope、同 host owner/gen、
  同不可变 ID/seq/digest 可共享三字段原接受回执，不写新事实/audit/count/执行；新 first-write 仍走 liveBinding。
  cursor 固定 App/principal/query/dataset/source epoch，每次 page/ACK 再验当前权限；ACK 不越过 offered，
  offered 不证明客户端收到，ACK 仅本地消费位置，不是 native 已收到/工具审批或工作已消费。
- C07 独立小锁 drainer 不持 lease/DB/UI 锁；32 条规范化 capture，逐事实 SQLite commit 后才释放本地 buffer。
  断线/不支持帧/overflow 保留 terminal gap 和真实 TreeStopped；unknown 同 owner/gen 的已存事实仍可补读。
  每流 256、dataset 10,000 条硬限，容量耗尽先事务保存 gap/unknown 再报 backpressure；不删旧事件或去重键，
  不做 retention/删除/可靠效果平台，也不把未提交内存 capture 当重启无损。
- 真实 SQLite 验证收取→重复/冲突→page/ACK→关闭重开补取、跨 App/主体/撤权/代次拒绝、乱序 sticky gap、
  最后 outbox 写失败下 row/sequence/stream/dataset/audit 全回滚及硬容量后原回执仍可重放。
  同一 generic 契约另用注册 acp 的计数夹具通过；这不是 C08 原生接入验证。
- JSON-RPC owned 子进程验证 thread/started、两个相同私密 delta、问题回调仅成 4 条受控事实，正文不入页面；
  Drain 不发 Turn/start，不重开线程。Release 保留 source epoch，显式 Resume 使用新 source，DatasetEpoch 不变。
  96 帧慢消费洪泛产生 bounded 32 条事实、明确 gap/observed high-water、TreeStopped 与 unknown，非静默丢弃或原生 ACK。
- 预审 P2 高水位回退真实 red：观察 10 无 Stop 后较旧 8 首携合法 Stop，使高水位倒退，旧 closed 还把 failed 降级，
  terminal-watermark-red 共 3 fail；相关 late-gap 早退吞诊断另有 1 fail。最小同事务修正为 high-water 取 max、
  Stop 独立合并、Gap/失败不降级；晚诊断不重写已 released/releasing 的资源事实。red 证据分阶段保留。
  初期 other-principal 2 fail 是把授权共享 source 原事实误按 cursor 私有性拒绝的测试预期不当，
  已按父级/安全窄核修正契约与正反例，不记为安全漏洞或安全 red。
- 首次冻结前四包 runtime/runtimecodex/store/appserver nonrace 与 race 均退出 0，各 131 Test/subtest pass、无 fail/skip，
  源码 hash 前后一致，owned 进程停止和临时根清理确认；受影响 vet 与 quality(base efa) 退出 0。
  本地证据 `.cache/c04-events-20261103/` 含命令、选定 env、源码 hash、原始日志和回执；
  初 smoke runner 扩集合漏同步 postcheck 的编排 assertion 原日志保留，没有当正式通过回执。
- 生命周期复审另确认 active-terminal P2：另一同 App scope 主体/同主体更新授权版本可经新 SourceEnd
  写活跃 ready→unknown、Run/stream/audit；已停止诊断还推进资源版本。source-terminal-authority-red 真实 4 fail。
  最小修复把“历史重复/持久已知停止诊断”与“可能活跃 source 的新终态”分开，后者写任何 stream/resource 前
  核原 live Run Principal/AuthVersion/current target；已停止诊断共享不改 Binding/Run。
  同一调用链再实际复现 forbidden Drain 把新写授权失败当 source 故障清理，另一主体/更新版本各触发 Stop=1；
  source-drain-authority-red 与含 capture 释放计数的 drain-capture-authority-red 均真实 3 fail，capture release=0。
  最小复用 disclosureFence 于 cleanup 前，失效返回错误并保留原 capture，不发 Stop、不写终态。
  真正内部 capture overflow 防护与客户端 forbidden Drain 分开；旧 6e4b 候选归档未提交，不据旧审查结论保存。
  补正后 review-final-green/review-final-race 四包各 138 Test/subtest pass、无 fail/skip，源码 hash 前后一致，
  owned 停止/临时根清理确认；受影响 vet/quality 再退出 0。red 旧源码/测试的归档提取或明确标注重构
  均以当时 preflight SHA 校验，不把重构当额外测试运行；最终回归与待审源码逐字节匹配。
- 沿用官方 Go 1.27.2/GOTOOLCHAIN=local/已验证缓存、GOPROXY=off，三个 live/catalog opt-in 删除，
  GOMAXPROCS=1/-p=1、3m 测试/600s 监督、8GiB 空闲/4GiB 临时门槛不变。
  源码/手写离线夹具/真实验证分开；未接生产 Runner/public routes、原生持久 spool、C05/C06 或自主调度。
- 本地保存；完整 mcpconfig 中立临时目录环境项仍 blocked，未重探/重试/绕 guard。
  正式 GitHub 两审和真实 harness/C01 出口未通过，未 push/PR/merge/部署，中心暂停、派发关闭。
  下一最小片是消费这些事实的 C05 显式输入/当前 Turn 围栏及必要 C04 补足，不在本片展开。

## C05 — 显式单 text 工作 Turn（2026-11-03 UTC，本地离线切片）

- 输入固定 `f00eab8eb82e891e7698ff19acc6813da3aff591`；独立分支
  `codex/c05-explicit-work-turn-20261103`，保留原交接及 C02/C03/C04/C07 检查点。
  本片优先补空会话到显式工作的纵向缺口，再接第二种原生协议；不按编号补齐整个平台。
- 窄接口 PrepareWork/ProduceWork/ReadWork 与消费方 WorkPort；新增独立 `turn.start` action，
  既有 `session.control` 或更新授权版本不能替原 Run 铸造工作权限。
  仅接受显式有界 text，私有正文进入独立 work-input，不入 receipt/audit/idempotency；
  不自动投递 Task Context、Continuation、聊天或旧 recorded Input。
- 固定 Run/context/target、Binding/owner/generation/lifecycle command、source epoch/boundary 与 logical Work/command，
  SQLite 同事务准备私有输入和单写 slot；持久单 claim 后才在 DB 事务外调用原生 StartText。
  返回时重验当前执行 action、原 Principal/AuthVersion、配置、代次及 slot。
  accepted、terminal 与 delivery_unknown 分开；未知不清槽、不自动重发，关闭重开也不调用 native。
- 通用 logical Work ID 不等同原生 Turn ID；native ID 可缺，但必须有明确 RPC terminal 证据。
  终结式无原生 ID 的 generic 夹具验证公共边界，不冒称 C08/ACP 原生实现已接入。
  C07 薄组合只向已拥有 Thread 转发显式正文和固定 effort，不增加自动工具循环。
- C04 同事务保存精确 source/owner/gen/native Turn 的小型终态观察；早于 Start 响应的终态先保存，
  响应再按原生 ID 与调用前 source boundary 关联，不猜 Work 身份。
  已 unknown、source failed、已停止或 quarantine 的生命周期保持围栏，迟到事实不能复活 ready/session_bound。
  正常原生 terminal 可允许同健康 Peer 的下一次显式 Work，但不是 Task 业务完成或 TreeStopped。
  busy/unknown slot 阻止 Release/Cancel 偷清资源；真正 Release 仍需独立实际停止证据。
- 真实 SQLite 与 owned JSON-RPC 子进程验证同 Peer 两次明确 Turn、重复/并发仅一次 Start、异内容冲突、
  早终态先入库再返回 ACK、独立执行授权、私有正文隔离、事务末段失败全回滚、ACK 后落盘失败 unknown，
  以及关闭重开无原生重发。正常序列只发 1 次 thread/start 与 2 次 turn/start，不发送 Task 正文。
- 预审两项 P2 真 red：Start 期间执行权限/配置/owner 失效后仍 Stop；
  source end/quarantine 后迟到 ACK/terminal 把 Binding/Run 从 unknown 复活。
  `work-fence-red` 退出 1、8 个 fail 事件；当时生产源码原样归档于 `red-source/`，SHA 与 red preflight 一致。
  最小修复分别在 cleanup I/O 前核当前执行权限及 Work/command/slot 围栏，失效不 Stop/不清槽且剥私密错误链；
  confirmation/终态应用核原生命周期及 source active/busy，迟到结果仅补原 Work 观察。
  原两次显式 Start/合法早终态正例保持通过，没有顺带 queue/steer/interrupt。
- 首次固定四包 runtime/runtimecodex/store/appserver nonrace 与 race 均退出 0，各 155 Test/subtest pass、无 fail/skip，
  当前 Go 源码/测试与 preflight hash 一致，owned 进程停止、临时根清理确认；受影响 vet 与 quality(base efa) 退出 0。
  复用 Go 1.27.2/GOTOOLCHAIN=local/官方已验证缓存、GOPROXY=off，三个 live/catalog opt-in 删除，
  GOMAXPROCS=1/-p=1，600s supervisor、3m Go test timeout、8GiB 空闲与 4GiB 临时上限不降。
  命令、env、源码 SHA、red/green/race 原始日志及回执保存于 `.cache/c05-work-20261103/`。
- 最终安全复审补出本片 busy Read 旁路：读取期间撤执行权限、更新原授权版本或目标配置，
  post-read 已拒绝仍 cleanup Stop 活跃 Work。`busy-read-authority-red` 退出 1、4 fail 事件，三个场景均 Stop=1；
  原生产字节保存于 `busy-read-red-source/` 并与 red preflight SHA 匹配。
  最小修复使 post-fence 失效立即剥私密链返回，不 Stop/quarantine；合法未知读取 cleanup 前再核当前执行权限、
  初始物理 Binding 与 busy Work/command/gen/slot/source，不能清理读期间出现的新 Work。
  补正后 busy-read-fix-green/race 均退出 0，四包各 159 Test/subtest pass、无 fail/skip，
  源码 hash 一致、owned 停止/临时根清理确认，受影响 vet/quality 再退出 0；固定 head 检查另存同目录。
- 本片为源码/手写离线夹具阶段，未接真实订阅 harness、生产 Runner/public routes、C06、自动派发或重启调度。
  完整全仓 mcpconfig 中立 Git 临时目录环境项仍 blocked，未重探/绕过守卫；未 push、创建 PR 或合并。
  下一最小聚合片可按固定 ACP 差异接 C08 Antigravity 薄端口；C05 queue/steer/interrupt 后续另片验证。

## C05 — 显式单轮中断（2026-11-03 UTC，本地离线切片）

- 输入固定 `b895d499db8fa12a0af36a80618fefa5e9ed8ce7`；独立分支
  `codex/c05-work-interrupt-20261103`。本片选择既有活跃单轮的安全中断，不展开恢复、queue、steer 或 C08。
- 新增独立 `turn.interrupt` action、InterruptWork 与 scoped ReadWorkInterrupt；不把 `session.control`/`turn.start`
  升级为中断权限，新授权版本也不能升级旧 Run。当前主体/原 AuthVersion、scope/config、owner/gen、
  原 Work/start command/native Turn、busy slot 与 source epoch 在持久 claim 前核验。
- 每 Work 只允许一个独立中断命令，claim/audit/outbox/receipt 同事务后才在 DB 外 I/O。
  重试只读原命令当前回执，换 key 也不能重发；缺原生 Turn ID/typed 端口明确 unsupported。
  原生 ACK、rejected、not_sent、delivery_unknown 与 Work 状态分开；任何 ACK 都不清槽、不结束 Task、不 Stop。
  真实终态仍通过 C04 精确关联；旧 ACK 晚于 terminal 或下一 Work 准备，仅补旧命令事实。
- runtimecodex 在同一 lease 内有界缓存 StartText 返回的原 typed Turn，固定 logical Work ID/start command/gen；
  中断只调用其 Interrupt，不从用户字符串铸句柄、不创建会话或下一轮，不把 ACK 当 TreeStopped。
  unknown 围住仍匹配的活跃资源，已终态/换代资源不被重写，撤权错误不泄私密原生链。
  claimed/unknown 在重开后保留但不自动重发；即使 rejected/not_sent 也采用每 Work 一次的保守策略。
- 真实 SQLite 首个闭环 1 pass；JSON-RPC 首批 6 pass 后补必要矩阵。
  四包 matrix-green/matrix-race 均 178 Test/subtest pass、退出 0，无 fail/skip，
  当前源码与两轮 preflight SHA 一致、owned 停止与临时根清理确认；受影响 vet/quality(base efa) 退出 0。
  覆盖同 key/并发/异内容/换 key、独立权限/旧授权版本、无 native ID/缺端口、claim 末段失败全回滚、
  ACK 落盘失败 unknown 与关闭重开、RPC 内撤权/配置/owner 变化不 Stop/清槽/泄错，
  以及 JSON-RPC 明确拒绝、发后取消/断连/畸形成功、terminal 先于 ACK/新 Work 准备后的迟到 ACK。
  同健康 Peer 只有第二次显式 Start 才发下一轮；ACK/拒绝后仍能显式读取。
- 复用 Go 1.27.2/GOTOOLCHAIN=local/官方缓存，三个 live/catalog opt-in 删除，GOPROXY=off、
  GOMAXPROCS=1/-p=1，600s supervisor、3m Go timeout、8GiB 空闲与 4GiB 临时上限不降。
  证据 `.cache/c05-interrupt-20261103/` 保存命令、选定 env、源码 SHA、原始日志与回执。
  仅源码/手写离线夹具验证，真实 harness、生产路由与中断后台任务的实际停止范围仍未验证。
  全仓 mcpconfig 中立 Git 临时环境项保持 blocked，不重探/绕守卫；未 push/PR/merge，不扩后续模块。

## C08 — ACP v1 持久初始化骨架（2026-11-03 UTC，本地离线切片）

- 输入固定 `b7d5a35069e4605706dc7a0556b1c3b84945c9e1`；独立分支 `codex/c08-acp-initialize-20261103`。
  选择第二协议的初始化底座，避免继续只补 Codex 控制；本片不接完整 Session/Prompt/runtime 端口。
- 独立 `internal/agent/acpclient` 仅复用 process/jsoninput。旧 executeACP 会自动 prompt 后关闭，不搬其一次执行生命周期；
  不抽 Codex 共用传输平台，不改变旧执行路径/注册表。Antigravity 仍为计划优先目标，没有安装或运行发行包。
- 按 C01 已固定 `451c3cbc7df2f9bc164c25e25d36fa526003f085` 的稳定 v1 schema 只读复核 initialize，
  可选字段省略取默认；出现的 caps/authMethods 不接受 null，agentInfo 可 null；strict typed/null/version 围栏。
  唯一发出的请求是 JSON-RPC 2.0 initialize v1，客户端 fs/terminal 声明 false，不发送 initialized/authenticate/new/load/prompt。
- 宿主显式绝对 executable/canonical cwd/argv/env，防御复制且不继承环境；host 与单次初始化 ctx 分开。
  256KiB 帧、10s 或更短 caller deadline、精确 pending ID/单一 result-error；stdout 噪声、重复/错 ID、
  callback 或后握手 unsolicited 消息 fail closed。stderr 始终有界排空，不进 Info 或公开错误文本。
- Info 只保存有界 typed agent 声明与 auth 方法 ID/name，返回深复制；声明不等于认证/已实现 Session 或有效权限。
  明确 RPC 拒绝/不兼容版本不误报 unknown；发送后断连/取消/畸形响应保留 unknown，无自动重发。
  初始化失败的 InitializationError 单列实际 Process/TreeStopped，nil Peer 不制造停止证据；Close 也取实际监督回执。
- 首个真实 Go 子进程初始化 smoke 1 pass，普通与竞态四包 acpclient/appserver/process/jsoninput 各 115 Test/subtest pass，
  均退出 0、无 fail/skip，当前源码与预检 SHA 匹配、owned 停止/临时根清理确认；受影响 vet/quality(base efa) 退出 0。
  夹具核仅一条 initialize、成功后取消 op ctx 不杀 host Peer、声明无别名、显式 Env 不继承假父 marker，
  optional 默认、null/type/版本/错重复 ID/双结果错误、回调/超帧/噪声、明确拒绝/断连/阻塞无响应/host 取消及真实停止。
- 证据 `.cache/c08-initialize-20261103/` 含命令、env、源码 SHA、原始日志和回执；沿用官方 Go 1.27.2、
  GOTOOLCHAIN=local、官方缓存/GOPROXY=off、三 live/catalog opt-in 删除、-p=1/GOMAXPROCS=1 与原 8GiB/4GiB/600s guard。
  仅源码/手写协议夹具，本片没有 ACP Session、工作、native callback 回答或运行时接线，不标 C08 完成或真实多 harness 验收。
  全仓 mcpconfig 中立 Git 临时环境项仍 blocked、不重探或绕守卫；未 push/PR/merge。
  下一自然 C08 小片是显式 Session 建立及配置/能力拒绝，再单独接终结式 prompt，不在本片展开。

## C08 — 显式 ACP Session 创建（2026-11-03 UTC，本地离线切片）

- 输入固定 `133936bde6f514233c29183e099b1278e508e1b7`；独立分支 `codex/c08-acp-session-new-20261103`。
  本片只 session/new，不接 prompt、认证、runtime adapter 或 load 历史重放，不因同协议冒称 Antigravity 实测。
- Peer.NewSession 固定宿主 canonical cwd 与 `mcpServers: []`，没有 Task 可填 cwd/MCP 参数；
  Session 句柄私有绑定原 Peer/native ID/请求目录，没有字符串铸句柄或跨 Peer 接管入口。
  native ID 必须非空、非 null、有界 UTF-8/无空白控制；可选配置只查容器形状、不留 raw、不解释或声称 model/effort/权限生效。
- 每 Peer 一次 allocation attempt，已知拒绝也消耗；这是客户端保守策略，不是 ACP 协议限制。
  同 Peer 并发/重复不第二次 new，发前取消不消耗、不杀 Peer；成功后单操作 ctx 结束不停止宿主生命周期。
  原 initialize 关联不放宽，只增一个 typed ID2 pending；Close 回收新 writer 并取实际监督进程回执。
- 发送后取消/超时/断连、错重复 ID/畸形结果/未知 callback 使 Peer unknown 并关闭；
  SessionCreationError 单列实际 Process.TreeStopped 与已观测有效 native Session ID，不把本地停止当远端 Session 删除。
  已知 RPC/auth-required 拒绝原样区分，不自动 authenticate、fallback、重发或新建下一轮。
  固定基线无标准 session/close，不制造该 RPC；LoadSession 即使声明 loadSession=true 仍明确 unsupported、零 RPC。
- 首个 session smoke 1 pass；普通/竞态四包 acpclient/appserver/process/jsoninput 各 131 Test/subtest pass、退出 0，
  当前源码匹配预检、owned 停止/临时根清理确认，受影响 vet/quality(base efa) 退出 0。
  离线 Go JSON-RPC fixture 覆盖精确 cwd/MCPempty、仅 initialize/new journal、句柄身份/host lifetime、
  known 拒绝与未知结果、坏 ID/配置容器/callback/EOF、并发唯一、发前取消/发后取消/timeout 与真实关闭证明。
  矩阵后仅 types.go 两行 package 注释准确化为包含 Session 创建；final-green 再 131 pass，
  旧 race 字节差异仅此注释，comment-only-proof.json 反向替换精确匹配旧 SHA；固定提交后的 race 另行验证。
- 证据 `.cache/c08-session-new-20261103/` 含原始日志、命令/env/源码 SHA 与回执；原 Go 1.27.2、
  GOTOOLCHAINlocal/官方缓存、三 live opt-in 删除、GOPROXYoff、单并发及 8GiB/4GiB/600s guard 保持。
  仅源码/手写离线夹具；实际认证、Session 配置、prompt/工具与运行时接线仍未验证，C08 不标完成。
  全仓 mcpconfig 中立 Git 临时环境项保持 blocked，不重探/绕守卫、未 push/PR/merge。
  下一必要小片再考虑显式配置/终结式 prompt；不在本片展开或自动运行。

## C08 — 有界 ACP update metadata（2026-11-03 UTC，本地离线切片）

- 输入固定 `c56099e9fdd66569bdbd10dca480dd0f2cc7428a`；独立分支 `codex/c08-acp-updates-20261103`。
  选择 Load/terminal Prompt 共同依赖的接收前置，不一次加入长 RPC、配置、历史重放或工作派发。
- 仅接受已确认 Session 的 JSON-RPC session/update，私有句柄固定原 Peer/SessionID/本连接 epoch；
  new 回复前通知明确 unsupported，不猜第一个 ID。三个 text chunk 与 tool_call/tool_call_update 五类 metadata 白名单，
  只存 kind/UTF-8 字节数/有界 tool 引用/显式合法 status，不存正文、thought、标题、tool input/output 或完整历史。
  tool update status absent/null 保留“未提供变更”，tool completed 不是 Turn/Task 完成或权限批准。
- 固定 schema 另外四种更新及非 text 内容本片 unsupported，显式 gap/fail closed，不误称 schema-invalid 或偷偷丢弃。
  native permission/fs/terminal 等请求仍不答复。wrong Session、畸形/未知通知与超容量均停止 owned Peer，
  不自动 authenticate/load/prompt、重启、派发或回复工具权限。
- 独立小锁 FIFO 最多 32 条/16KiB metadata，协议 read loop 不等 consumer/DB。
  每连接 fresh local epoch/local_observed sequence，同内容两条均保留；不是原生 eventID、durable spool 或跨重连去重。
  Updates 防御复制并提供独立 sticky gap/high-water/终态，ReleaseUpdates 只推进已 offered 本地前缀，零 native ACK。
  清空 buffer 不修复 gap；结束后原 Session 仍能读诊断，Process.TreeStopped 只取真实监督 Wait。
  任何关闭的尾部窗口都未证明完整，包括显式 Close；不据 FIFO 空或本地停止声称远端 Session 删除/历史完整。
- 首个 Go 子进程 smoke 1 pass。WIP 契约 red：显式空 tool status 被误当 absence，两个子例加父共 3 fail；
  该新 parser 原字节在 red-source/ 归档并匹配当时 preflight，最小 presence 修复后纳入回归；不是旧 c560 产品缺陷。
  普通/竞态四包 acpclient/appserver/process/jsoninput 各 153 Test/subtest pass、退出 0、源码一致/owned 停止/临时根清理，
  受影响 vet/quality(base efa) 退出 0。必要夹具覆盖同 delta 两条/隐私无别名、nullable/absent 状态、
  wrong/早到 Session、unsupported plan/image/callback、count/byte overflow、sticky gap、本地消费越界、epoch 隔离与 Close 并发。
- 证据 `.cache/c08-updates-20261103/` 保存命令/env/源码 SHA、red/green 原日志与回执。
  Go 1.27.2/GOTOOLCHAINlocal/官方缓存/GOPROXYoff、三 live opt-in 删除、单并发及原 8GiB/4GiB/600s guard 保持。
  源码/手写离线 fixture 不代表 Antigravity 实测或完整 Load/Prompt 输出；C08/认证/runtime 接线仍未完成。
  全仓 mcpconfig 中立 Git 临时环境项保持 blocked，不重探/绕守卫，未 push/PR/merge；本片不展开后续模块。

## C08 — 显式 terminal text Prompt（2026-11-03 UTC，本地离线切片）

- 输入固定 `eb67ce2d3fb95b89a73e03eefe1f46f2e63b132d`；独立分支 `codex/c08-acp-prompt-text-20261103`。
  本片实际离线发送显式文本，不再只加调用前置；仅协议层，不接配置、Load、认证或 runtime adapter。
- Session.PromptText 固定原私有 Peer/Session/epoch，只一条非空 UTF-8 text（≤64KiB且实际编码帧≤256KiB）。
  单 Session 单 in-flight 覆盖 response 解码及 writer 回收，local request ID 单调递增，不是 native Turn ID。
  Busy/非法/发前取消零 RPC、不伤 Peer，不排队/自动注入 Context/自动重试或新建 Session。
- 等完整 ACP terminal response，只保留五种原生 stopReason 与 rpc_terminal 证据；
  end_turn/max_tokens/max_turn_requests/refusal/cancelled 不替换成 Task 业务成功、即时 ACK、输出全文或 TreeStopped。
  采用当前客户端五分钟有限操作 cap/更短 caller deadline，不是 ACP/Task 上限；不复用 initialize/new 的10秒。
  Session metadata 无 Prompt ID，terminal 附近/下一轮 late update 不猜归属，不清旧 FIFO。
- 已知 RPC 拒绝保留私密结构化对象/安全 Error 文本，正常保 Peer；它不证明此前没有输出/工具效果，下一次发送仍需显式调用。
  发送后 ctx/EOF/无效回包/unsupported callback/overflow unknown 并 Close，PromptError 携实际 Process 回执；
  不自发 session/cancel、认证、工具答复或恢复。已经验证的终态即使 ctx/EOF/writer 竞态也保留，
  连接失败与实际停止另报，不让终态回执谎称 Peer 可复用或把已知 stopReason 丢成 unknown。
- 首个 Go JSON-RPC 同 Session 两次显式 Prompt smoke 1 pass。首次扩大矩阵因测试未使用变量编译失败，
  该日志单列，不作为产品行为 red 或通过；移除测试变量后四包普通/竞态各172 Test/subtest pass、退出0，
  源码前后一致、owned 停止/临时根清理，受影响 vet/quality(base efa) 退出0。
  必要夹具覆盖五StopReason、Busy/预取消/编码膨胀、明确拒绝后显式发送、错/旧/重复ID/坏shape/EOF/callback/overflow、
  sent cancel/timeout、真实>10.5秒整轮响应，以及未读stdin writer阻塞时保留已收terminal并回实际连接停止证据。
  矩阵后仅 types.go package 注释准确化；comment-only-proof 反向替换精确匹配两轮旧 SHA，
  不冒称当前 Go 注释字节已跑该轮 race；提交后准确 HEAD 相关检查另行覆盖，不为注释重跑 precommit 矩阵。
- 证据 `.cache/c08-prompt-text-20261103/` 含原始命令/env/源码 SHA、日志与回执；Go1.27.2、官方缓存/GOTOOLCHAINlocal、
  GOPROXYoff/三live opt-in删除/单并发/原8GiB/4GiB/600s guard 保持。
  metadata 仍不是输出正文或完整历史，model/effort/权限未选择或生效验证；真实 provider/工具/认证未测，C08不标完成。
  generic ProduceWork 的10秒 start-ACK 路径仍是后续ACP runtime接线依赖，本片未修改。
  全仓mcpconfig环境项保持blocked、不重探/绕守卫，未push/PR/merge；只保存本小片，不展开后续模块。

## 2026-11-04 — 恢复验证与独立规划同步

- 固定产品 HEAD `ab0e2068ee42face7516acfaa6fd559fc7ab0c11`，parent `eb67ce2d3fb95b89a73e03eefe1f46f2e63b132d`，
  tree `3e9ae2362bcbfe19b215b9f0d329165d2757e3a6`；恢复核实工作树 clean、原十三个保护 ref 不变。
  独立规划 PR 的文档输入为 `1189e104b02c8fde0adaa94a2c0ce4c4a2bb1e23`，产品实现位于上述独立检查点，未混入该规划 PR。
- 原检查点已通过相关四包 acpclient/appserver/process/jsoninput 普通与 race 各172 Test/subtest pass，
  全部 vet/build、quality(base efa, CI=true) 与八项隔离生成文件逐字节比对均退出0；旧回执不是新 head CI 的替代。
  旧精确 HEAD 回执：`.cache/c08-prompt-text-20261103/committed-head-checks/summary.json`，
  SHA-256 `4e7a7b5fd24ba8d49a6f5c1eb8aa215decf1392396df03d7e6464eda4747a7ef`。
- 2026-11-04 04:42 UTC 起按正常有界 supervisor 在同一 ab0 重新执行上述四包普通/race，各172 pass、四包均0，
  owned 进程停止/临时根清理确认；全 vet/build、quality 与八项 drift 再通过，源码/refs 未变。
  新回执：`.cache/resume-validation-20261104-kzvd5kzc/summary.json`，
  SHA-256 `68de023c9e872a316d8dd552fce9bef4c0bb3039e3687dfe7fd0961f7753984b`。
  这些路径均在 `/workspace/Coordica-continuation-20261103/` 下，是未上传的本地证据，不宣称公开 CI/日志已有相同结果。
  首次恢复 runner 因 JSON bool 嵌入 Python 的格式错误在测试前退出，单列 startup failure，不计产品 red 或测试通过。
- 本地完整门禁保持 blocked：mcpconfig 对临时目录逐祖先 Lstat `.git`，当前已知获准临时根存在平台标记。
  本次仅做一次只读 metadata 复核；未删除/换路径绕过/跳测试，也未重试先前 read-only `/var/tmp` 动作。
  正常 ignored TMP 仅用于不依赖该中立条件的上述协议/进程夹具；不把其172项结果当全仓通过。

| 证据层 | 本批事实与边界 |
| --- | --- |
| source / checkpoint | ab0 保留 C02–C05/C07 与 C08 初始化、Session、metadata、terminal text Prompt 局部代码；未进入 main，C06 仍规划中，C08 配置/Load/runtime 接线未完成 |
| fixture / local regression | 手写离线 Go JSON-RPC、SQLite 与进程夹具；ab0 新旧精确回执通过，不冒称原生录制或 provider 兼容 |
| local code / safety review | ab0 的审定 diff SHA `a13a5b0625157a645a32df19c00833cc80aa2130111fc5e550d2299154a1e20f` 有两项明确本地结论；容量失败与已完成 MESSAGE 分开核对，恢复后内容未变 |
| GitHub / formal review | 父会话标准推送 checkpoint 并核准原 hash；恢复核查时 open PR 为0。当前文档候选准备后由父会话创建 draft/触发正式两审，实际 PR/head/CI/回应另行记录 |
| live / stage exit | C01 真实认证与 C07/C08 原生持续执行、工具/审批、有效 model/effort 仍未验；无生产中心/部署/发包/自动派发/定时，不据代码或 fixture 关闭阶段出口 |

- 父会话已确认 checkpoint 的精确 ab0 触发 [CI run 41774760591](https://github.com/5intro/Coordica/actions/runs/41774760591)。
  本恢复记录时仍 in_progress，仅记录 pending 和链接，不把部分 job、旧 main CI 或本地172项写成三平台完整通过。

- Nov4 增量相对1189更新 CLOUD_EXECUTION/ROADMAP/PROGRESS，并限定 DESIGN/VALIDATION/REQUIREMENTS 三页的历史基线表述，共六份文档；
  拟 PR 相对核查的 main 输入 efa 包含既有 R.6 十文档及这六文件追加，总计仍十文档。验证限定质量与必要链接检查，不重复无关产品单测或继承 ab0 为本片全部 CI。
  受影响检查与质量通过可创建 draft；main 合并仍按准确最新 head 三平台 CI、代码/安全两项明确结论及最终版本核验。
  用户已覆盖旧 quota 停线和临时 local-only/no-PR；同 `gpt-6.1-sol` / `ultra`、C09–C12 仅准备及所有 live/凭据/部署禁令保持。

## 2026-11-04 — 产品分支整合恢复文档

- 从受保护 ab0 建立独立分支 `codex/c08-acp-config-options-20261104`，整合已审规划 `84f01e2b46b1422a9eda8a3c7b8b497fd1325611`。
  保留所有产品实施历史与原始 refs，仅解决文档冲突；Go 源码逐文件哈希保持 ab0，不把文档合并称新功能。
- 父会话已发布 [draft PR #31](https://github.com/5intro/Coordica/pull/31)，准确 head 为84f01e2b；
  正式 [代码触发](https://github.com/5intro/Coordica/pull/31#issuecomment-6623054874) 与 [安全触发](https://github.com/5intro/Coordica/pull/31#issuecomment-6623056172) 已成功，结论/CI仍 pending。
  本地整合不替代该 PR 的正式双审与准确 head 三平台 CI；线上动作由父会话处理。

## C08 — 原生声明配置与显式选择（2026-11-04 UTC，离线切片）

- 产品输入为受保护 `ab0e2068ee42face7516acfaa6fd559fc7ab0c11`；独立分支 `codex/c08-acp-config-options-20261104`。
  先以文档合并 `7c6e7f10ca58a5e20c3ce90125725aa8f9160799` 整合已审恢复指导，所有 Go 文件哈希仍等于 ab0、原 refs 不变。
  当前只补第二协议发送前需要的配置事实与显式选择，不展开 C09 目录、自动选优、Load、认证或 runtime adapter。
- `Session.ConfigOptions` 返回原私有 Peer/Session/epoch 的有界防御快照；new 的 absent/null 与已知空数组分开。
  固定 ACP schema 的 select flat/grouped、nullable UX 描述/未知 category 可见；数组≤64KiB、64选项/256选择及单字段有界。
  重复 ID/值歧义拒绝；category/name 不推断模型、effort、权限或 Task 授权，也不宣称 OS/账号隔离。
- `SetConfigOption(ctx,id,value)` 仅显式选取当前广告的精确 ID/value；缺失/未知为本片客户端子集的零 RPC 拒绝，
  不代表原生不支持，也不冒称完整 R6 explicit_unverified 路径。无默认、fallback、自动认证或重试；十秒客户端 cap/更短 caller deadline。
  与 Prompt 共享单 inflight、递增本地 RPC ID、解析与 writer 回收；不伪造 native Turn ID 或即时工作 ACK。
- set 回应必须提供非 null 完整 configOptions；精确 ID/currentValue 匹配才报告 native_reported Confirmed。
  错值/缺目标保实际快照并返回 mismatch，不伪回滚；已知 RPC 拒绝也不证明原配置没有改变。
  new/set-response/config_option_update 只由 read loop 按接收顺序全量替换并递增本地观察版本；它不是服务端 CAS。
  回执保自身历史版，晚返回不会覆盖新通知；通知只将 ConfigVersion metadata 入原有 FIFO，不公开选项正文/值或猜 Prompt 归属。
- 发前取消/Busy/未知选择不写也不杀 Peer；发送后未知关闭并带真实 Process 停止回执，不自动重发。
  已验证配置回执遇 ctx/EOF/writer 失败仍保留历史观察，连接失败/停止另列；不据本地停止声称远端回滚或 Session 删除。
  现有 gap/overflow、unsupported callback 与 fs/terminal=false 保持，不自动答复工具或扩大宿主能力。
- 首个真实 Go JSON-RPC 子进程闭环1项通过；配置专属矩阵46项通过。四包 acpclient/appserver/process/jsoninput
  普通与竞态各218 Test/subtest pass、退出0，无 fail/skip；源码哈希一致、owned 停止/临时根清理确认。
  夹具覆盖精确广告/flat-group/null/empty/歧义、深拷贝、双向 Busy/共享 ID、回应前后更新顺序、错值/漏目标、
  已知拒绝后显式 Prompt、发后取消/畸形/断连与真实 TreeStopped、已收回执保留、容量与 sticky gap。
  证据 `.cache/c08-config-options-20261104/` 保存命令/env、源码 SHA、原始日志/回执及文档整合证明。
- Go1.27.2/GOTOOLCHAINlocal/官方已验缓存、GOPROXYoff、三 live/catalog opt-in unset、单并发与8GiB/4GiB/600s guard 保持。
  仅源码/手写离线 fixture；真实 provider、认证、实际 model/effort/权限仍未验，C08 不标完成。
  全仓 mcpconfig 中立临时目录环境项仍 blocked，不重探/删 marker/绕守卫；本源码片未 push/建 PR。
  后续按依赖处理 ACP 整轮 runtime 接线或其它真实必要缺口，本片不自动展开。
