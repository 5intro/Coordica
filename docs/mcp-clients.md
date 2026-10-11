# 统一接入 Agent 客户端

每台电脑只安装一份 Coordica。Claude Code、Codex、Cursor、Kimi Code、pi 和 dsh 最终都启动同一个 `coordica mcp --bootstrap`；客户端之间的文件格式差异由 `setup` 处理，不要求用户逐个编写配置。

## 一个安装入口

先将二进制放入固定目录。在 Windows PowerShell 中：

```powershell
& C:\Coordica\coordica.exe setup
& C:\Coordica\coordica.exe setup --apply
```

macOS/Linux：

```sh
/固定路径/coordica setup
/固定路径/coordica setup --apply
```

第一条预览本机发现的客户端和目标文件，第二条应用配置。自动发现依赖本机 CLI 和已存在的客户端配置目录；未安装或无法确认的客户端不会伪造成功。只安装指定客户端时使用 `--client cursor` 等参数；未知客户端可以用 `--client generic --apply` 导出标准 MCP JSON，再由该客户端导入。

无需逐家填写二进制路径：默认使用当前运行的 Coordica 的绝对路径。不要从临时构建目录执行此命令。可用 `--binary` 明确指定固定绝对路径，用 `--runner-data` 固定 Runner 数据目录。`--tools-file` 指定固定绝对路径的可信工具注册文件，让 bootstrap 与 Runner 继承它；`--json` 输出结构化结果；`--output` 指定 dsh overlay / 通用 JSON 的导出目录。

## 客户端差异由配置模块处理

| 客户端 | 注册方式 | 使用后操作 |
| --- | --- | --- |
| Claude Code | 用户级 JSON 中合并 `mcpServers.coordica` | 重新打开会话，检查 MCP 工具列表 |
| Codex | 官方 CLI 读取结构化列表并注册；保留 TOML 其他设置 | 重新打开会话，检查 MCP 工具列表 |
| Cursor | 用户级 `~/.cursor/mcp.json` 合并 stdio 服务 | 刷新 MCP 或重新打开客户端 |
| Kimi Code | 根据明确的用户配置布局选择新旧 MCP JSON | 新建会话 |
| pi | 支持原生 MCP 的版本使用用户级 MCP JSON | `/reload` 或重新打开会话 |
| dsh | 生成包含本机路径的独立 Cordis overlay | 按返回路径执行 `dsh web --patch 配置路径` |
| 其他 MCP 客户端 | 导出通用 `mcpServers` JSON | 使用该客户端的导入入口 |

dsh 的 overlay 会生成到系统用户配置目录下 `Coordica/mcp`，或 `--output` 指定目录；它不会替换整个 dsh profile。客户端尚不具备统一的“安装 MCP”协议，因此 Coordica 内部仍需维护少量配置适配，但用户使用同一个入口。

pi 需要 0.99.0 或更新版本的原生 MCP 支持；无法确认版本时报告不支持。Kimi 的新旧目录分别为 `~/.kimi-code` 与 `~/.kimi`；两种目录同时存在或没有明确依据时，不猜测当前 CLI 使用哪个配置，可明确设置官方 `KIMI_CODE_HOME` 指向新版目录或使用通用导出。Claude Code 的 `CLAUDE_CONFIG_DIR`、pi 的 `PI_CODING_AGENT_DIR` 与 Codex 的 `CODEX_HOME` 自定义配置位置也应遵循客户端自己的设置。

已有同名服务且启动命令/参数一致时保留原配置，包括用户设置的额外选项；同名但启动方式不同则报告冲突，不能静默覆盖。JSON 文件的其他 MCP 和未知字段会保留；修改前备份原文件，格式错误时拒绝修改。配置写入期间应避免在客户端同时编辑同一文件；检测到并发变化时修复后重试。Codex 使用官方 CLI 管理配置。

生成内容只包含服务名、绝对可执行文件路径、固定启动参数以及客户端需要的传输/超时字段。不会写入中心地址、配对码、节点令牌、模型密钥或模型账号。`setup` 不会注册节点或启动 Runner。

## 接入与受管执行分别验收

| 能力 | 本版范围 |
| --- | --- |
| 客户端调用 Coordica MCP | 提供上述客户端的配置入口；实际连接仍需在对应已安装版本上确认 |
| Runner 启动 Agent 并处理结构化事件、取消、预算和恢复 | 内置六工具定义，由四种协议驱动执行；见 [注册与协议说明](tool-registry.md) |
| 新的同协议工具 | 从本机可信文件注册；节点上报能力后即可参与调度，不修改中心名单 |

配置完成后先在客户端调用 `inspect_environment`，检查 Git 与本机注册工具的版本及声明能力。使用 Cursor 的是官方 CLI `agent` / `cursor-agent`，不能用 IDE 的 `cursor` 命令代替；pi Runner 驱动要求 1.1.0 或更新版本（高于仅配置 MCP 所需的版本）。版本成功不证明账号认证或一次真实执行成功。之后提供真实中心 HTTPS 地址、一次性配对码和公共 CA，调用 `initialize_runner`。同一电脑的各客户端共用固定 Runner 数据目录，不必分别配对。

完整的配对、后台服务与故障处理见 [初始化指南](windows-onboarding.md)。客户端对 MCP 服务的信任/启用确认仍由客户端自己的界面管理，Coordica 不修改模型登录或绕过这些确认。

## 官方格式依据

- [Claude Code MCP](https://code.claude.com/docs/en/mcp)：用户 scope 与 stdio 配置。
- [Codex MCP CLI 实现](https://github.com/openai/codex/blob/main/codex-rs/cli/src/mcp_cmd.rs)：结构化读取、用户配置注册与同名替换行为。
- [Cursor MCP](https://cursor.com/docs/mcp)：用户级配置和 stdio 字段。
- [Kimi Code MCP](https://www.kimi.com/code/docs/en/kimi-code-cli/customization/mcp.html)、[迁移说明](https://www.kimi.com/code/docs/en/kimi-code-cli/guides/migration.html)：新旧配置布局。
- [pi MCP](https://pi.dev/docs/latest/mcp)：当前原生 MCP、用户级文件和重载方式；旧版需先升级。
- [dsh MCP 客户端](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/mcp/mcp-client/README.md)：Cordis stdio overlay。

这些是注册格式依据，不能代替所有系统与客户端组合的实测结果。
