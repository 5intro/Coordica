# Windows dsh 与 Mac 中心双机验收

先用独立小项目，避免故障测试影响 Coordica 自身；它通过后，再在本项目做一项小而可回滚的实际开发任务。所有结果记录两端 OS、工具版本、中心/Runner 版本、固定 Git 对象、预算、代次、进程和检查证据；协议替身或 CI 不能代替这项验收。

## 准备隔离项目

两端安装本次包，工具已由本人登录。中心所在 Mac 安装 Node（仅此验收项目的检查需要，无 npm 包或其他后台服务）。把安装包的 `examples/acceptance` 复制到独立短路径，例如 Mac `/private/tmp/coordica-acceptance-work`。它包含已有函数、通过的基线测试、owner 的独立新需求 verifier，以及 dsh 任务 JSON。

在该目录建立 Git 提交，再创建中心本地裸仓库作为测试远端：

```sh
cd /private/tmp/coordica-acceptance-work
git init -b main
git add .
git -c user.name=Acceptance -c user.email=acceptance@localhost commit -m 'Acceptance baseline'
node --test test/greeting.test.mjs
git clone --bare . /private/tmp/coordica-acceptance-remote.git
```

使用新的中心数据目录、独立 TLS 证书和端口，不复用实际开发项目。LAN 连接要求 HTTPS、正确主机名/IP 与公共 CA；不复制中心私钥或 owner 令牌。Windows 用短数据路径（如 `C:\CoordicaData`）规避 Git 路径长度限制。两台电脑配对为不同节点，分别调用 MCP 的环境探测和初始化；关闭聊天客户端后仍应看到 Runner 心跳。

Web 项目仓库为上面的裸仓库绝对路径，目标分支 main。项目预算建议 3600 秒，保护 `tools/**` 及 `task.json`。配置最低检查：

| 检查 | command | args | 超时 |
|---|---|---|---|
| 任务回归 | 中心 Node 的绝对路径 | `["--test", "test/greeting.test.mjs"]` | 30 秒 |
| owner 独立验收 | 同一 Node 绝对路径 | `["tools/verify-acceptance.mjs"]` | 30 秒 |

独立 verifier 在基线上应失败，因为新增需求尚未完成；不是一个假通过脚本。`task.json` 可通过 Web 对照创建或 `ctl POST tasks --body-file` 保存草稿，再由 owner 明确批准。实施工具 dsh，允许 `src/**` 与 `test/**`，单 Run 600 秒，任务累计 1200 秒。Mac 节点应报告至少一个可用独立评审工具（优先 Claude；Codex 先排除现有连接问题）。容量建议两端各 1，避免并行干扰。

## 实测顺序与断言

1. **接入**：Windows dsh 调用 MCP，Git/dsh 版本与节点身份真实上报；后台 Runner 上线，重复初始化不重复注册，聊天关闭后仍有心跳。
2. **成功闭环**：Windows dsh 领取上述改动，在私有工作区完成函数和测试。上传固定 B/H；Mac 新 Run、新会话评审固定 C；中心两项检查通过；精确 C 进入测试 main。实际 diff 只在允许路径，verifier 和检查政策不变。
3. **范围拒绝**：在新的隔离任务/基线中故意产生 `tools/**` 变更，确认中心拒绝集成且 main 未变化，私人修改可留作证据。不要在成功主线上破坏 verifier。
4. **取消与预算**：单独启动一个持续运行的小任务，在真实子进程活动时取消或等预算到期；核对 Windows Job 及后代结束、quiescent、额度结算。未确认静止时占槽不能释放；不以 cancel ACK 判断结束。
5. **断线与重启**：仅切断 Runner 到测试中心的连接，确认约 30 秒成功心跳间隔触发本地停止；恢复后完成消息重放不重复集成/扣费。测试中心重启、旧代次/租约请求拒绝、重复消息及服务重入；这些协议异常使用受控 harness，不让模型伪造凭据。
6. **主线变化与评审失败**：在独立任务处理中由 owner 推进测试 main，确认旧 C 的评审/检查失效并重新构建。分别制造失败检查和 changes_requested，确认返修受相同累计预算与次数限制，不自动越过门禁。
7. **权限与凭据**：验证未配对/撤销节点不能领取或上传，Windows Runner 没有中心发布凭据。已登录 dsh 的真实权限消息逐条核对；若某版本只提供不可验证的请求，应明确记录阻塞及缺口。

故障项每次使用新的任务或独立测试仓库/中心数据，避免互相污染。先跑无需模型的现有 Go harness 做异常断言，再选择必要项在真实 dsh 上验证进程、断线和重放；不会为了覆盖每个错误反复消耗模型预算。

## 通过后继续开发本项目

选择一个不涉及授权、租约、预算、验证或发布边界的小任务，例如增加节点能力的展示/过滤。Windows dsh 实现，Mac 另一工具独立评审，中心运行项目真实检查；范围明确，涉及受保护模块时仍需要 owner 对精确 C 特别批准。

两阶段都保留真实版本及结果后，才将相应 OS × 工具标为已验收。Cursor、Kimi Code、pi 的兼容性另用同一小项目分别验证，不用 Windows dsh 的通过结果代替。
