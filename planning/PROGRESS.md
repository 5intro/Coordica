# 实施记录与决策日志

工作修订：R2026-11-02.6；本次恢复核查的 main 输入版本为 R2026-11-02.5。主表：[ROADMAP](../ROADMAP.md)。
仅记录已发生的工作和明确决策；不要将实施建议写成已授权事实。

## 当前状态快照（2026-11-04 UTC）

- 规划 [#29](https://github.com/5intro/Coordica/pull/29) 与 C01 文档 [#30](https://github.com/5intro/Coordica/pull/30) 已合并，
  截至本次恢复核查，远端 main 输入为 `efa1ec1f8191b5fd4530cde6678d5b2e05fc4b58`；历史合并/审查证据见后文记录。
  旧批次中“open / denied / 待授权”均为当时事实，不再代表这两个 PR 的现状。
- C01 的实例认证/真实调用仍待验证；C02–C05、C07、C08 有另分支局部离线实现，C06 仍规划中。
  source、手写 fixture、局部回归、本地 review 与真实验收分开，不据此标主任务完成。
- 产品检查点固定 [ab0e2068ee42face7516acfaa6fd559fc7ab0c11](https://github.com/5intro/Coordica/commit/ab0e2068ee42face7516acfaa6fd559fc7ab0c11)，
  父会话已用标准非 force push 同步到 [codex/cloud-checkpoint-20261104](https://github.com/5intro/Coordica/tree/codex/cloud-checkpoint-20261104)，并用官方 API 核准原哈希。
  这是备份分支，不是 main 合并、产品 PR 或发布；原十三条规划/实现分支与原始 SHA 保持。
- 独立规划文档分支 `codex/publish-provider-plan-20261104` 基于 `1189e104b02c8fde0adaa94a2c0ce4c4a2bb1e23`，
  只更新恢复授权/范围/证据和正式审查条件，已由父会话发布 draft [PR #31](https://github.com/5intro/Coordica/pull/31)。
  C07 发布分支仅整合e47与规划；C02 发布分支从6249整合该依赖；本 C03 发布分支再从2e整合C02文档，只新增绑定及必要披露修复，不导入C04/C05/C08或native read/resume薄组合。
  下述全局进展不表示本树具备所有实现。历史 local-only/no-PR 与 10% 暂停线已被 Nov4 用户授权覆盖。
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

## C07 core — 既有源码独立发布准备（2026-11-04 UTC）

- 源码固定 `e47c435a1fe21be30f5bfed11b1c728fccfbb29b`；独立工作树/分支 `codex/publish-c07-core-20261104`。
  只整合已审规划 `84f01e2b46b1422a9eda8a3c7b8b497fd1325611` 的文档，保留全部 C07 历史与原始 refs；Go 逐文件保持 e47。
  本树仅新增 C07 app-server 传输、typed thread/turn 控制、原生问题/审批答复和异常成功响应围栏；旧驱动不改。
  不导入 C02–C05/C08 或持久 runtime 接线，不新增产品行为，不把 offline 夹具称真实 harness。
- 完整发布差异以规划84f为比较输入，正式 base 若变化由父会话再核；本地复审覆盖完整源码与文档差异，不能只审本次合并文档。
  新增协议包直接依赖 process/jsoninput，并纳入未改的 legacy agent 包回归旧驱动兼容；acpclient 不存在，不运行它。
  appserver/agent/process/jsoninput 普通与 race 均279 Test/subtest pass、4包退出0；各有3项真实 harness/catalog opt-in 测试按未开启正常 skip，未将它们写成通过。
  两轮 Go 哈希不变，owned 进程停止/临时根删除确认；全 vet/build、quality（固定84f为发布比较输入、CI=true）退出0。
  隔离 contractgen 的8个输出与精确 e47 HEAD blobs 逐字节一致；只整合文档，不借旧 ab0/5e39 的测试批准本候选。
  `.cache/publish-c07-20261104/` 保存完整命令/env、源码 SHA、测试日志、门禁与保护 refs 证据，均为未上传本地文件。
  全仓 mcpconfig 中立 TMP 环境项仍 blocked，不重探/删 marker/绕守卫；受影响通过不是全仓通过，真实 harness/认证仍未验证。
  旧低10%/临时localNoPR限制已被Nov4授权覆盖；同Sol/Ultra、不live/凭据迁移/部署/dispatch/timer及准确head正式两审/三平台CI要求保持。
- 父会话已推送配置片 `5e39f779df85b819857d2b672f6f98daab8214d5` 并创建另一个 stacked [draft PR #32](https://github.com/5intro/Coordica/pull/32)，base为冻结ab0；
  正式code/security触发已成功且本记录时 running/pending，不借该候选的回执批准本C07 core。配置分支/规划84f与全部原始refs仍受保护，本writer未作线上动作。

## C02 — 既有账本独立发布准备（2026-11-04 UTC）

- 源码固定 `62495534b2921b873c4174c09cef8b0ed0fabd3c`；独立工作树/分支 `codex/publish-c02-ledger-20261104`。
  整合已发布 C07 候选 `e4a64a829aa0cd34acbde72c99e8afe5fd892ac0` 的文档，全部 Go 文件保持原6249，保留所有旧历史、日志所在工作树与refs。
  本公开增量仅11个 runtime Go文件及README：Task/context/input、显式Run登记/读取、本地未启动记录取消、host-only Manager及当前持久授权/幂等/CAS/事务回滚。
  C07 core作为既有依赖保留；不导入C03绑定/C04事件/C05工作/C08 ACP，构造Ledger不启动harness/worker/dispatch或服务，旧transport路由不改。
- 固定公开比较输入为e4a；PR33依赖尚在正式安全审查/CI时可准备stacked draft，合入与否及实际main由父会话精确复核。
  本地复审覆盖该输入至候选的完整源码/README/发布docs差异，不只审此次文档合并；本树registered/not_started/recorded不冒称原生执行或TreeStopped。
  runtime/store/domain/jsoninput 普通与race均82 Test/subtest pass、4包退出0，无fail/skip；真实本地SQLite覆盖重开、撤权、scope、CAS、幂等和末段回滚。
  两轮Go哈希不变、owned停止/临时根删除确认；全vet/build、quality（固定发布输入e4a、CI=true）退出0，隔离生成8个contract与精确6249 HEAD blobs逐字节一致。
  `.cache/publish-c02-20261104/` 保留命令/env、源码哈希、原始日志、门禁与refs回执，均是未上传本地文件，不借原检查点测试或其它PR结果。
  全仓mcp中立TMP环境项仍blocked，不重探/删marker/绕守卫，无真实harness/凭据迁移/部署/派发/timer；这些受影响检查不是全仓通过。
- 父会话本次提供的线上快照：规划PR31已按准确84f合并到main28e31b2，规则/内容树等于原84f；
  [PR33](https://github.com/5intro/Coordica/pull/33) 的e4a代码审查明确clear、安全/CI pending；[PR32](https://github.com/5intro/Coordica/pull/32) 的cfg5e39双formalclear，CI仍待核且冻结baseab0不可merge。
  不借这些回执批准本C02，writer不作线上动作；正式准确新head双审/三平台CI、依赖合入及最终版本核验保持。

## C03 — 既有绑定与已知披露修复发布准备（2026-11-04 UTC）

- 源码输入 `2e2107f73519bd85faddd3407bf3a7db22083753`；独立分支 `codex/publish-c03-bindings-20261104`，公开比较输入为C02候选 `95827cdf54d35f347afcd0f287f88fc3f495765e`。
  先只整合该候选文档，保存全Go逐字节等2e的本地基点；保留所有旧源码、refs和日志所在工作树，不盲reset/rewrite。
- 两项只读复核确认原2e Produce失败最终出口会保留过期调用方可As取得的私密native/cleanup错误链，cleanup无proof时还能跳过finish复验。
  选择仅回移6ea的post-quarantine disclosureFence出口及helper；不提取perform，不导入SessionPort/read/resume/runtimecodex/C04/C05/ACP。
  原生产binding_produce字节/hash先归档；新Create-only回归必须在原2e生产上真实red，再最小green，历史6ea red不作为本候选实跑。
- 首次准备基点时，PR33精确protected-default merge被自动审批拒绝，父会话向用户请求具体授权、当时结果pending；不得换工具/autoMerge/pushmain绕过或重试。
  PR34/32依赖hold，writer无线上动作；C03只读准备/本地修复不授外部main合并权，真实harness/凭据迁移/部署/dispatch/timer禁令及C09–12仅准备保持。

## PR34 — 通用 Run 可选 Model/Effort（2026-11-04 UTC）

- 输入为已发布旧候选 `95827cdf54d35f347afcd0f287f88fc3f495765e`；保留本地备份 `codex/c02-publish-checkpoint-95827-20261104`。
  原生产 run.go 字节/hash先归档；旧82普通/race、CI/两审结论不覆盖本次修复的新head。
- 父会话与两只读预审确认 generic validTarget 强制 Model/Effort 非空会拒绝合法 native-harness 选择。
  最小修复仅让二者各自 empty-or-valid-identifier（非空仍≤128字节、UTF-8、无空白/控制）；
  空表示未指定，指定值原样进入 Run 快照与幂等 digest，不补默认、不选优、不fallback、不改变实例/node/harness/version/workspace必填或当前授权/CAS。
  registered/not_started 不证明可执行；具体原生适配器仍可拒绝缺少其必需配置，C07 model校验与PR32 native-reported范围均未放宽。
- 新真实SQLite回归先在原95827生产上red：5 fail事件/15 pass、退出1，三种省略组合先被ErrInvalid拒绝（含父4fail），
  另一个scope对照同样在授权前被语法校验挡住（1fail，不是权限绕过）；非法非空/必填字段反例及完整显式对照通过。
  最小两行生产条件修复后，同一测试字节的20项专属回归通过；停止/临时根清理确认，无真实harness。
  新四包 runtime/store/domain/jsoninput 普通/race各102 Test/subtest pass、4包退出0、无fail/skip，源码哈希一致与停止/清理确认。
  全vet/build、quality（固定e4a、CI=true）退出0，8个隔离contract与95827对应HEAD blobs逐字节一致；不借旧head通过，也不把局部结果当全仓mcp门禁通过。
  证据 `.cache/c02-optional-options-20261104/` 含原run.go、red测试原字节、命令/env/源码SHA及raw回执；均为未上传本地文件。
- C03本地docs-only基点 `a255110488df0c6ff07595645192b393f18c6e84` 已暂停，581Go保持2e，尚无新Create/Release红测或生产修复；
  已知Create与Release私密错误披露均必须修复后才可发布，恢复时也须携本C02修复，不能重引强制选择。
  PR33 protected-default精确merge首次被自动审批拒绝；父会话取得补充可信用户原文授权后，只对同工具/同动作/expected e4a 重试一次成功。
  PR33现已closed/merged，实际main为 `ed6e96f80c649468751ad62dce19c62700e27cb2`（parents28+e4a），tree `bc0df16f33c93ac1fc1733d581e627add55f16be` 与e4a完全相同；没有自approve或绕过。
  本C02修复改用该实际main比较，父会话已将原PR34 retarget main，操作时head仍为95827/draft true；新修复head须重做CI/正式两审，旧95827含已知finding不能合并。
  PR32依赖hold不因此解除，writer无线上动作。
- Owner已撤回过程中短暂的high继任偏好，最终指示为当前及后续开发轮次均继续 `gpt-6.1-sol` / `ultra`；
  本轮未作模型切换或重启，不将实施选择转成产品Run默认选项；不care剩余额度与其余无live/凭据迁移/部署/dispatch/timer、C09–12仅准备的边界保持。

## C03 — 继承C02可选配置并修两类披露出口（2026-11-04 UTC）

- 从已保存的docs-only基点 `a255110488df0c6ff07595645192b393f18c6e84` 恢复，先整合C02修复 `462de7eaf59c1893612745ad2d82a0bc3a6cb5ab`。
  继承的Go差异只有run.go可选校验与optional_target_test.go，其余Go仍为原2e；保存继承基点后才加新Create/Release回归，不覆盖可选修复。
- Create只携6ea已知post-quarantine披露围栏；Release另按bindingFor(false)的合法清理权限复验，允许目标配置退役后当前同主清理。
  撤权/移scope/RecoverUnknown旧owner的私密错误链不得从As取回，原command观察/Stop事实保留、unknown/新代次不被迟到回执推进，不重发。
  两个生产原文件先归档，各自新真实red后最小green；实际owned Go夹具Wait/TreeStopped与逻辑状态分开，不能用历史6ea red冒称新候选实跑。
- 不引perform抽取、native read/resume薄组合、C04/C05/ACP或C09–12实现。当前及后续仍Sol/ultra；PR33已merged ed6，PR34已推462并触发新head审查/CI、结果仍running。
  writer只做本地安全开发，不push/approve/merge；fullmcp中立TMP阻断保持、不重探/绕守卫，无live/凭据迁移/部署/发包/赛车/dispatch/timer。
