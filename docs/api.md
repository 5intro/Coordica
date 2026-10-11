# API v1

默认前缀 `/api/v1`。除一次性配对注册外，需要 `Authorization: Bearer ...`。写用例需要 `Idempotency-Key`，相同主体、相同键但不同请求内容返回冲突；心跳与内容寻址材料为自然幂等操作。错误统一 `{code,message}`。

| 方法/路径 | 权限 | 输入/作用 |
|---|---|---|
| GET state / export | owner | Snapshot；不导出凭据 |
| POST project | owner | 名称、无凭据仓库URL、分支、总预算、保护路径、最低检查 |
| POST requirements | owner | title/body |
| POST tasks | owner | 标题/目标/验收、依赖、关联需求、路径、资源、工具、预算/次数 |
| POST tasks/:id/approve / pause / resume / cancel | owner | VersionRequest |
| POST tasks/:id/scope | owner | ScopeRequest；停止后批准新范围版本 |
| POST tasks/:id/budget | owner | BudgetRequest；停止后批准有限额度，保留已用 |
| POST project/pause / resume | owner | VersionRequest |
| POST project/budget | owner | BudgetRequest；暂停后批准总额度，max_repairs/max_retries 为0 |
| POST pairings | owner | name；返回有效十分钟的一次性码 |
| POST runner/register | 配对码 | RegisterRequest；返回节点令牌 |
| POST nodes/:id/revoke | owner | 撤销节点，拒绝后续许可 |
| POST runner/claim | node | ClaimRequest；原子领取，返回 Assignment 或 null |
| GET runs/:id / context | 所属 node | 执行状态或有限任务上下文 |
| GET runs/:id/input.bundle | 有效所属 node | 固定执行基线 bundle |
| POST runs/:id/heartbeat | 所属 node | HeartbeatRequest；续约或取消 |
| POST runs/:id/artifacts | 有效所属 node | 最多 64 MiB 不可变 bundle |
| POST runs/:id/complete | 所属 node | CompleteRequest；Runner 停止后持久重放 |
| POST runs/:id/block | 有效所属 node | generation/reason；请求人工处理并停执行 |
| POST runs/:id/confirm-stopped | owner | StopConfirmation；只确认 lost Run，证据必填 |
| POST candidates/:id/approve | owner | VersionRequest；对精确 C 特别批准 |
| POST backups | owner | 返回经闭包校验的本机备份目录 |

完整字段见 `api/v1.schema.json`。这是公开记录契约，创建请求允许省略由服务端分配的字段；应用会丢弃客户端提交的权威状态/计数字段并自行产生。ID 不接受作为本地文件路径使用。

MCP 从节点凭据派生，限制于启动参数绑定的 Run；不能传入另一 Run ID、owner 令牌或任意 API 路径。`request_scope` 是阻塞报告，授权调整仍由 owner 的 scope 用例处理。代码提交由 Runner 的停止与材料校验流程统一完成。
