# Coordica

多电脑、多 Agent 开发协调底座。采用 Go 中心与 Runner、SQLite、React/TypeScript 控制台；Web 产物嵌入中心二进制，无 Redis、消息队列或额外后台运行时。

当前是 **0.1.3 开发版**。已实现固定 Git 候选、独立评审、程序检查和条件发布闭环；构建与测试结果见 [GitHub Actions](https://github.com/5intro/Coordica/actions)。协议模拟测试、跨平台编译和托管 CI 不代表用户电脑上的真实部署验收。

## 许可与下载

**源码可见，个人非商业使用免费。** 允许个人非商业修改并在自己的设备上使用；个人商业用途、团队或公司使用及二次开发、向他人发布修改版均须事先另获书面授权。这不是 MIT 或 OSI 开源许可。正式条款见 [LICENSE](LICENSE)，授权申请可向本仓库维护者提交 Issue。

第三方组件保留各自许可，见 [第三方声明](THIRD_PARTY_NOTICES.md)。本许可不会仅因使用 Coordica 就要求你公开独立开发的项目。

预编译开发包通过 [GitHub Releases](https://github.com/5intro/Coordica/releases) 提供。通过 `coordica setup --apply` 统一配置本机 Agent 的 MCP 接入，不必逐家手写配置；见下方初始化步骤。Windows amd64 包内还附 `coordica.exe` 和 dsh 配置模板 `coordica.cordis.yml`。发布包附 `SHA256SUMS`，当前均为待完成实际部署验收的开发预览版。

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

## 运行目录与 macOS 权限

首次安装时自行选择固定可执行路径，macOS 建议使用 `~/Library/Application Support/Coordica/bin/coordica`，再从该位置执行 `setup`。`setup --runner-data 绝对路径` 可自定义 Runner 数据目录；中心和 Runner 均支持 `--data 绝对路径`。MCP 用户配置默认保存在用户配置目录，项目仓库建议放在 `~/Projects` 等 Desktop、Documents、Downloads 之外的目录。macOS 对 Desktop、Documents 和 Downloads 另有文件访问授权，仅改变启动工作目录不会授予权限。[Apple 文件访问说明](https://support.apple.com/en-ca/guide/security/secddd1d86a6/web)

当前开发版没有稳定的 Developer ID 分发签名。构建产物变化会改变 ad hoc 签名的 `cdhash`，系统可能重新请求权限，不能保证一次授权跨版本有效；请按实际需要授权，不要为减少弹窗而授予全磁盘访问。[Apple 分发签名说明](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac/)、[DR 身份说明](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements)

已有节点升级保持原数据目录和节点身份，不直接搬动已有数据；材料和仓库记录可能使用绝对路径。

## 本机启动

```sh
./bin/coordica serve --data .coordica-center
```

在运行中心服务的电脑上打开 `http://127.0.0.1:8080`，将 `.coordica-center/owner.token` 中的令牌输入登录页。控制台及 owner API 只监听本机回环地址；令牌不打印到服务日志，请不要把 owner 令牌交给 Agent。

8080 是统一管理入口，包含需求池、任务池、运行与节点、政策与恢复，以及“中心配置与接入”。登录后可直接访问 `http://127.0.0.1:8080/#connection`，即使尚未建立项目也能查看真实监听地址、版本、数据目录、公共 CA 和证书 DER 的 SHA-256 指纹。

为每个远端节点分别生成一次性接入指令，再发给那台电脑的 Agent；指令包含公共 CA 和配对码，不包含 owner/node 令牌、私钥或中心私有数据路径。复制失败时使用“复制已有接入配置”或手动复制文本，保留原配对码；到期后再重新生成。

Web 中建立项目、明确总预算和最低检查，录入需求与任务并批准。仓库地址支持不含凭据的 HTTPS URL，或中心本机 Git 仓库绝对路径。远端需要已有目标分支与至少一个提交。阶段 A 针对 Coordica 自举，中心强制保护授权、验证、集成、适配器与初始化服务模块；用户还可追加保护路径。路径模式按仓库根目录、大小写敏感的 `/` 路径匹配，支持 `*` 和完整路径段 `**`；符号链接、submodule 等当前不支持的变化会拒绝集成。

## 统一 MCP 初始化（主要接入方式）

安装到固定目录后，先运行 `coordica setup` 预览，再用 `coordica setup --apply` 为发现的客户端注册同一个 MCP。支持 Claude Code、Codex、Cursor、Kimi Code、pi 的已核验配置格式，并为 dsh 生成本机 overlay；有版本或配置冲突会明确报告。需要指定客户端时使用 `--client`。Runner 按本机工具注册表发现能力，内置 Codex、Claude Code、dsh、Cursor CLI、Kimi Code、pi 的执行定义；新工具可以复用已有协议驱动注册，本机注册示例见 [custom-acp.json](examples/tools/custom-acp.json)。版本探测不代表已登录或已经完成真实任务验收。

macOS、Windows、Linux 都把对应平台的 `coordica` 可执行文件安装为 stdio MCP，参数为 `mcp --bootstrap`。在中心“中心配置与接入”生成一次性接入指令，交给执行电脑的 Agent；也可在“运行与节点”创建配对码，另行提供中心 URL 和必要的公共 CA 路径。MCP 自动发现 Git 与工具路径，交给 Runner 配对，并安装/启动用户级后台服务。无需手工执行 `pair` 或 `runner`。每台电脑使用独立节点身份，容量为 1 或 2。

本地探测不读取 Agent 认证文件；已有登录继续由各官方工具使用。用户级后台服务与聊天客户端分离；macOS 使用 launchd、Linux 使用 systemd --user、Windows 使用当前用户的计划任务。服务启动失败会明确报错并保留已配对状态，不伪装成已上线。

## 执行电脑的本机页面

Runner 启动后，在该电脑打开 `http://127.0.0.1:8081`，查看本机节点、工具能力、已领取任务、Run 状态和自己提交的需求。页面可向中心提交需求提案；阶段 A 只登记需求，由中心 owner 创建并批准任务，不会因为提交文字自动启动开发。

本机页面使用 Runner 已配对的节点身份，无需输入 owner 令牌；节点令牌保留在 Runner 中，不发给浏览器。节点身份只能查看自己的运行与需求，不能批准、扩围、改预算或发布。页面仅监听回环 HTTP，前台启动可通过 `runner --console-listen 127.0.0.1:端口` 自定义端口。关闭页面或聊天客户端不影响后台执行。

若中心也在这台电脑，Runner 会核对已配置的中心地址是否属于本机及中心返回的连接信息，再显示跳转到中心需求池、任务池、运行与配置页的入口。中心页面仍须使用 owner 凭据登录，Runner 不会代为读取或授予 owner 身份；其他电脑的 Runner 页面不显示这些入口。

已有安装需将中心与执行电脑上的 Coordica 二进制升级到 `0.1.3-dev`，再重启服务，才能使用同机中心导航和新的接入配置页；保留原数据目录和节点身份。

## 两台电脑通过局域网连接

在中心生成证书，`--hosts` 必须包含第二台电脑实际使用的中心 IP 或 DNS 名：

```sh
./bin/coordica tls --out .coordica-tls --hosts localhost,127.0.0.1,192.168.1.10
./bin/coordica serve --data .coordica-center --listen 192.168.1.10:8443 --console-listen 127.0.0.1:8080 --tls-cert .coordica-tls/center.crt --tls-key .coordica-tls/center.key
```

把 **center.crt** 复制到 Runner 电脑，向已安装 MCP 的客户端下达指令，例如：“初始化本机 Coordica Runner，中心为 `https://192.168.1.10:8443`，CA 文件为本机 `center.crt` 的绝对路径，使用我提供的一次性配对码。”初始化工具返回节点 ID 和服务状态；中心节点页的心跳记录用于确认实际上线。

也可在中心配置页下载公共 CA 并生成完整接入指令，让远端 Agent 将指令中的公共 CA 写入自己的固定路径。指纹核对使用证书 DER 的 SHA-256，不能直接计算 PEM 文件文本的哈希。

将示例 IP 替换为中心实际的局域网 IPv4。`https://192.168.1.10:8443` 只提供 Runner 配对与执行接口，不提供控制台或 owner API。控制台仍在中心电脑的 `http://127.0.0.1:8080` 打开，不需要浏览器信任局域网的自签证书。

`--console-listen` 默认 `127.0.0.1:8080`，仅接受回环地址，端口可自定义。默认启动时两个监听地址相同，在本机共享 HTTP 服务；指定局域网监听后分为本机控制台和 HTTPS 节点接口。节点接口也可使用 `--listen 0.0.0.0:8443`，此时需加上 `--runner-url https://192.168.1.10:8443`，向配置页明确可分享的实际地址；该地址必须使用 HTTPS，且主机名或 IP 须被 TLS 证书 SAN 覆盖。未配置实际地址时不会把 `0.0.0.0` 或 `::` 复制给远端 Agent。私钥只留在中心，非回环 HTTP 连接会被拒绝；不提供跳过证书验证选项。证书默认一年有效，公网中转尚未实现。

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

配对码也可通过 stdin 输入；配对后删除临时码文件。`--tool-executable 工具ID=路径` 可重复指定注册工具的本机路径；旧三工具参数保留兼容。`--tools-file` 加载本机可信 JSON 注册表。`coordica doctor` 只做版本探测，不会把已安装误报为已认证或已验收。

owner CLI 在中心电脑使用本机控制台的 HTTP API，默认地址为 `http://127.0.0.1:8080`，默认令牌文件为 `.coordica-center/owner.token`。所有写操作都需要幂等键。自动生成的键打印到 stderr，丢失响应后可用 `--key` 重试：

```sh
./bin/coordica ctl
./bin/coordica ctl --path export > export.json
./bin/coordica ctl --method POST --path pairings --body-file pairing-request.json
./bin/coordica ctl --method POST --path backups
```

自定义控制台端口或中心数据目录时，分别设置 `ctl --center http://127.0.0.1:端口 --token-file 数据目录/owner.token`。此前指向局域网 HTTPS 地址的 owner CLI 需改用本机控制台地址；仅分离中心监听不要求升级已有 Runner，它们保持原 HTTPS 地址、CA 和节点身份，无需重新配对。执行电脑的本机页面需使用支持该页面的 Runner 版本。

`--body-file -` 读标准输入。暂停/恢复使用 `project/pause`、`project/resume` 或 `tasks/任务ID/pause` 等路径，JSON 为 `{"expected_version":当前版本}`。当前版本来自状态快照；旧版本写入返回冲突。协议记录见 [API Schema](api/v1.schema.json)。

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

恢复后项目暂停，旧节点撤销，重新配对并核对 Git 外部事实后恢复。旧电脑未上传的私人修改不属于恢复保证。升级前先备份，停止旧进程再启动新二进制。中心与 Runner 升级到 `0.1.3-dev` 后可使用新的接入配置页和同机中心导航；`0.1.2-dev` Runner 保留本机活动页，`0.1.1-dev` Runner 仍可连接新中心但没有本机页面。`0.1.0` 中心的严格解码不接受新能力字段，不能混用新 Runner；新中心会保守阻止缺少取消/评审声明的旧 Runner 领取新活动。在途旧 Run 按原租约核对。代码合并不自动升级服务。

## 维护

真实 Agent 验收可使用安装包中的 [独立小项目](examples/acceptance/README.md)，在隔离仓库执行，不影响本项目或用户实际项目。

没有开启后台自动升级、公开上传、自动计划生成或阶段 B 的全局自动批准。
