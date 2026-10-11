# Coordica

个人多电脑、多 Agent 开发协调底座。阶段 A 实现采用 Go 中心与 Runner、SQLite、React/TypeScript 控制台；Web 产物嵌入中心二进制，无 Redis、消息队列或额外后台运行时。

当前是 **0.1.0 开发版**。已实现固定 Git 候选、独立评审、程序检查和条件发布闭环；真实工具与各操作系统的验收情况见 [验证记录](docs/implementation-status.md)。没有把协议模拟测试或跨平台编译称为真实部署验收。

## 许可与下载

**源码可见，个人非商业使用免费。** 允许个人非商业修改并在自己的设备上使用；个人商业用途、团队或公司使用及二次开发、向他人发布修改版均须事先另获书面授权。这不是 MIT 或 OSI 开源许可。正式条款见 [LICENSE](LICENSE)，授权申请可向本仓库维护者提交 Issue。

第三方组件保留各自许可，见 [第三方声明](THIRD_PARTY_NOTICES.md)。本许可不会仅因使用 Coordica 就要求你公开独立开发的项目。

预编译开发包通过 [GitHub Releases](https://github.com/5intro/Coordica/releases) 提供。Windows amd64 包内包含 `coordica.exe` 和可直接加载的 dsh 配置模板 `coordica.cordis.yml`；安装步骤见 [MCP 初始化指南](docs/windows-onboarding.md)。发布包附 `SHA256SUMS`，当前均为待完成实际部署验收的开发预览版。

## 构建

需要 Go 1.26+（本次使用 1.27.2）、Node 24、npm、Git。Node 只用于构建 Web；部署只需要对应系统的二进制、Git 和各 Agent 官方 CLI。

```sh
make build
make test
make check
```

Windows 可在 PowerShell 分别运行：

```powershell
npm --prefix web ci
npm --prefix web run build
go build -trimpath -o bin/coordica.exe ./cmd/coordica
go test -race ./...
```

`make generate` 从 Go 协议记录生成 `api/v1.schema.json` 与 Web 类型；不手改生成文件。网络输入仍由中心做严格 JSON 解码、领域校验和授权检查。

维护者准备好 `bin/coordica`（macOS arm64）、`bin/coordica-windows-amd64.exe` 和 `bin/coordica-linux-amd64` 后，可运行 `make package`，用 Python 3 标准库生成三个分发包和 `SHA256SUMS`。脚本校验许可、包内文件及二进制字节；它不构建程序、不上传发布，普通下载用户无需安装 Python。

## 本机启动

```sh
./bin/coordica serve --data .coordica-center
```

打开 `http://127.0.0.1:8080`，将 `.coordica-center/owner.token` 中的令牌输入登录页。令牌不打印到服务日志；请不要把 owner 令牌交给 Agent。

Web 中建立项目、明确总预算和最低检查，录入需求与任务并批准。仓库地址支持不含凭据的 HTTPS URL，或中心本机 Git 仓库绝对路径。远端需要已有目标分支与至少一个提交。阶段 A 针对 Coordica 自举，中心强制保护授权、验证、集成、适配器与初始化服务模块；用户还可追加保护路径。路径模式按仓库根目录、大小写敏感的 `/` 路径匹配，支持 `*` 和完整路径段 `**`；符号链接、submodule 等当前不支持的变化会拒绝集成。

## 统一 MCP 初始化（主要接入方式）

macOS、Windows、Linux 都把对应平台的 `coordica` 可执行文件安装为 stdio MCP，参数为 `mcp --bootstrap`。在中心“运行与节点”创建一次性配对码，然后向客户端下达初始化指令，提供中心 URL、配对码和必要的公共 CA 路径。MCP 自动发现 Git 与工具路径，交给 Runner 配对，并安装/启动用户级后台服务。无需手工执行 `pair` 或 `runner`。每台电脑使用独立节点身份，容量为 1 或 2。

本地探测不读取 Agent 认证文件；已有登录继续由各官方工具使用。用户级后台服务与聊天客户端分离；macOS 使用 launchd、Linux 使用 systemd --user、Windows 使用当前用户的计划任务。服务启动失败会明确报错并保留已配对状态，不伪装成已上线。详细安装参数见 [统一初始化指南](docs/windows-onboarding.md)。

## 两台电脑通过局域网连接

在中心生成证书，`--hosts` 必须包含第二台电脑实际使用的中心 IP 或 DNS 名：

```sh
./bin/coordica tls --out .coordica-tls --hosts localhost,127.0.0.1,192.168.1.10
./bin/coordica serve --data .coordica-center --listen 0.0.0.0:8443 --tls-cert .coordica-tls/center.crt --tls-key .coordica-tls/center.key
```

把 **center.crt** 复制到 Runner 电脑，向已安装 MCP 的客户端下达指令，例如：“初始化本机 Coordica Runner，中心为 `https://192.168.1.10:8443`，CA 文件为本机 `center.crt` 的绝对路径，使用我提供的一次性配对码。”初始化工具返回节点 ID 和服务状态；中心节点页的心跳记录用于确认实际上线。

浏览器需要信任此证书或使用已受信任的服务器证书。私钥只留在中心。非回环 HTTP 连接会被拒绝；不提供跳过证书验证选项。证书默认一年有效，公网中转尚未实现。

## GitHub 发布权限

开发 Runner 通过 bundle 收发固定代码材料，不接收中心发布凭据。中心 Git 客户端禁用系统/全局 Git 配置、交互凭据、仓库 hooks 和外部过滤器；平台不会借用 Runner 的 GitHub 写凭据。

对 HTTPS 仓库，可把完整 `Authorization` 值存到中心专用私密文件，再使用 `serve --git-auth-file 文件 --git-auth-remote 精确仓库HTTPS地址`。文件内容通常为 GitHub 支持的 Basic 授权值；不要把凭据放进 URL、项目配置、CLI 参数或仓库。授权严格绑定一个 HTTPS 地址且禁止重定向。

“只有本人/本人授权中心可写”还依赖 GitHub 仓库权限和分支规则。部署到你自己的项目时，需要自行设置仓库权限和中心专用凭据；下载本项目不授予本项目仓库的写权限。私人电脑原有用户凭据不属于本平台能绝对隔离的安全边界。

## CLI 与 MCP

以下是诊断入口，正常接入使用上面的 MCP 初始化。需要前台排障时，可手动配对和启动：

```sh
./bin/coordica pair --center https://192.168.1.10:8443 --ca center.crt --data .coordica-runner --code-file pairing.code
./bin/coordica runner --data .coordica-runner
```

配对码也可通过 stdin 输入；配对后删除临时码文件。`--codex`、`--claude`、`--dsh` 可指定工具路径。`coordica doctor` 只做版本探测，不会把已安装误报为已认证或已验收。

CLI 使用同一 HTTP API，所有写操作都需要幂等键。自动生成的键打印到 stderr，丢失响应后可用 `--key` 重试：

```sh
./bin/coordica ctl
./bin/coordica ctl --path export > export.json
./bin/coordica ctl --method POST --path pairings --body-file pairing-request.json
./bin/coordica ctl --method POST --path backups
```

`--body-file -` 读标准输入。暂停/恢复使用 `project/pause`、`project/resume` 或 `tasks/任务ID/pause` 等路径，JSON 为 `{"expected_version":当前版本}`。当前版本来自状态快照；旧版本写入返回冲突。完整路由见 [接口说明](docs/api.md)。

受限 MCP 使用已配对的节点凭据，固定绑定一个 Run：

```sh
./bin/coordica mcp --runner-data .coordica-runner --run run_id --generation 1
```

提供 `get_context`、`report_block`、`request_scope`。范围申请只登记阻塞，不能放宽权限。最终代码与结构化评审由 Runner 在受管进程停止、固定材料校验后提交，MCP 不允许模型提前宣称进程已停止。stdio 遵循 [MCP 2025-11-25](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)，没有 owner 批准或发布工具。

## 暂停、恢复与备份

暂停停止新派发并要求在途进程结束。已经进入 `dispatching` 的外部 Git 操作继续核对真实远端结果，成功不会被当前暂停状态抹掉。失联 Run 不会自动当作已停止；在运行页确认实际进程已结束并填写检查证据后，再恢复任务。

中心重启会把旧验证进程标记为待核对。Runner 保留工作目录和完成消息，恢复时先交接/核对，未知旧进程不会被盲目重派。重试与修复次数、预算均保留；增加任务预算/次数需要 owner 新版本批准。

Web 或 CLI 创建一致备份，返回中心上的备份目录。备份包含数据库快照、引用材料及必要 Git 对象，清单核验完成后才发布。凭据不进入备份。恢复到不存在的新目录：

```sh
./bin/coordica restore --from 备份目录 --data 新中心目录
./bin/coordica serve --data 新中心目录
```

恢复后项目暂停，旧节点撤销，重新配对并核对 Git 外部事实后恢复。旧电脑未上传的私人修改不属于恢复保证。升级前先备份，停止旧进程再启动新二进制；代码合并不自动升级服务。

## 维护

模块依赖、状态规则、故障处理与测试分工见 [架构说明](docs/architecture.md)。设计基线位于 [v0.2](docs/lightweight-multi-agent-design-baseline-v0.2.md)。没有开启后台自动升级、公开上传、自动计划生成或阶段 B 的全局自动批准。
