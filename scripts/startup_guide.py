"""Public first-use guide text and its limited package-identity rendering."""
from string import Template


_BASE_GUIDE = Template(r"""Coordica v$version — $platform

版本说明与下载：https://github.com/5intro/Coordica/releases/tag/v$version
源码与发布包可能对应不同提交，请以所用版本的说明为准。
检出或更新源码不会自动安装、发布或部署。
中心、MCP 与 Runner 使用同一个可执行文件，无需单独安装 Runner 或构建源码。

许可：仅个人非商业使用及私人本地修改免费。商业用途、团队或公司使用、
以及发布修改版须另获书面授权。完整条款以 LICENSE 为准。
这是附有限制的源码可见许可，不是 OSI 批准的开源许可。
第三方组件保留各自许可，详见 THIRD_PARTY_NOTICES.md 和 third_party/licenses/。

执行电脑：
1. 将 $binary 放入固定目录，不要从临时目录或 go run 启动。
   安装路径可自定义；macOS 首次安装建议固定于 ~/Library/Application Support/Coordica/bin，数据与项目选用 Desktop/Documents/Downloads 之外的目录。
2. 本机需要 Git；要使用的 Agent CLI 应已安装并登录，沿用其自身配置。
3. 运行 $binary setup 预览，再运行 $binary setup --apply，统一为发现的客户端
   注册 MCP。Windows 使用 .\coordica.exe，Unix 使用 ./coordica 或固定绝对路径。
   可用 --client 选择一个客户端；dsh 会生成独立 overlay 并给出加载方式。
4. 先调用 mcp__coordica__inspect_environment 检查本机环境，不需要中心服务。
5. 使用中心“中心配置与接入”为这台电脑生成的一次性接入指令，
   让 Agent 保存公共 CA、注册 MCP，再调用 initialize_runner 配对并启动用户级 Runner。
   也可自行提供真实中心 HTTPS 地址、配对码和必要的公共 CA 绝对路径。
6. 在中心“运行与节点”核对后续心跳。关闭 MCP 会话不影响独立 Runner。

项目初始化：所有旧数据库与无 Goal 项目也需要 Leader 只读分析和 Owner 确认精确基线。
中心主机先用 host-config 保存可信固定角色/路径，再 setup --host-role center --host-config 固定配置 --runner-data 独立固定Runner目录；initialize_center/start_center/get_host_status 均无路径参数。
首次保存 host-config 前选择仅同机或跨机：跨机保留 console-listen 127.0.0.1:8080，
设置 runner-listen 为实际 IP:8443 和 runner-url 为匹配的 HTTPS 地址；初始化在原 data 生成 TLS 证书。
需要远端 Git/GitHub 权限时，同次保存 git-auth-file/git-auth-remote 或 github-repository/github-token-file。
具体安全文件格式和参数见 README.md 的“可信中心、项目初始化与 MCP 安装”（#setup-reference）。
host-config 不覆盖已有配置；不要改 data 或另启 serve 冒充既有中心接续。
同一个中心 MCP 保留 initialize_runner，无第二个入口；中心 limited profile 保持原身份，不发送到提供的其它配对端点。
checks-file 可省略，先建立暂停的空检查草案并访谈；Owner 保存真实非空检查政策后重新分析确认，空检查不能执行。
初始化捕获 Owner 的 PlanningTimeoutSeconds 与 PlanningMaxRetries；默认300秒、两次重试，无需开启自动化。
原始回答、覆盖和源 SHA 保留；new 访谈可以在首次提交前开始。分析不执行检查、不生成开发任务、不启用自动化。
Owner token 不经 MCP 返回；受限 MCP 不隔离聊天的其它工具。

开始使用与控制台：README.md 的“开始使用”（#getting-started）。
可信中心与 MCP 初始化：README.md 的“可信中心、项目初始化与 MCP 安装”（#setup-reference）。
跨机接入及局域网证书：README.md 的“Runner 本机页面与局域网接入”（#network-reference）。
中心统一管理入口只在中心电脑打开，默认 http://127.0.0.1:8080；
需求池、任务池、运行与节点及政策均在这里。登录后打开 /#connection，
可在建项目前查看实际部署与公共 CA，为每个远端节点分别生成一次性接入配置。
重试复制使用“复制已有接入配置”，保留原配对码。指令不含 owner/node 令牌或中心私有数据路径。
执行电脑使用中心另行提供的局域网 HTTPS 节点地址和公共 CA。
通配监听地址不能分享；中心使用 --runner-url 指定 HTTPS 实际地址，且须由 TLS 证书 SAN 覆盖。
支持本机页面的 Runner 启动后，可在该电脑打开 http://127.0.0.1:8081，
查看自己的运行并向中心提交需求提案；提案不会自行创建或批准任务。
仅当中心也在本机且连接信息匹配时显示中心各池与配置入口，中心仍须 owner 登录。
统一接入：默认 setup 创建唯一的 coordica（mcp --interaction）；每台物理主机共享一个有限 interaction profile。
中心本机：$binary chat-init --data /绝对路径/中心数据 --url http://127.0.0.1:8080 --profile /绝对路径/interaction.json --name "Shared center host"
节点 initialize_runner 自动复用/签发有限节点对话凭据；已保存注册的重试不会再次配对。
也可显式签发：$binary chat-init --runner-data /绝对路径/已有Runner数据 --profile /绝对路径/interaction.json --name "Shared Runner host"
然后预览 $binary setup --chat-profile /绝对路径/interaction.json，核对后加 --apply。
只迁移可确认归属且路径匹配的旧 coordica/coordica-chat；冲突保留，客户端选项与禁用状态保留。
旧显式 --mode bootstrap/chat 仅为兼容入口；默认统一接入，不安装多个角色。
中心停机或缺失 profile 时本机工具可用；在线工具每次校验凭据和监听范围。
能力变化在客户端活动时通知；不支持动态刷新时手动重新发现或重连。
dsh 只迁移安装器生成的文件；已加载旧 overlay 须自行从启动命令移除。
固定 Run MCP 仍仅有 get_context/report_block/request_scope，无主机管理或对话权限。
当前源码已支持按工具实际报告选择 Agent 模型与思考强度，并配置节点容量；
具体功能与权限边界见 README.md 的“目标授权、需求生命周期与工作台”（#workflow-reference）；
获取对应版本请参阅“版本与下载”（#validation-reference）。
可执行文件、数据和私有 profile 使用稳定绝对路径；setup 后重新加载或重启对应客户端。
中心凭据仅在中心回环地址允许 Leader 咨询；节点凭据仅允许安全公共事实、需求及本节点 Agent/Run。
节点不能查询全局任务、阻断、Leader 对话或其他节点执行上下文。凭据范围由中心持久化记录决定。
profile 不含 Owner/node token，不可放入源码或公开包；旧无范围凭据须显式 --renew。
Leader 纯咨询/建议不创建任务或修改授权；页面切换保留未确认请求，重试使用原内容与同一键。
项目 total_budget_seconds 与 ChatOverview budget_seconds 均为忽略的弃用字段，项目支出仅作统计。
开发/评审/规划/咨询单 Run 最多七天；任务/目标内规划累计最多 365 天且不少于单 Run，重试仍有限。
旧 budget 阻断不自动迁移；Owner 核对停止结算、剩余额度和当前授权后以当前任务版本恢复。
恢复不清零支出，不绕过重试上限，不改变已有发布意图；具体兼容步骤见 README.md 的“暂停、恢复、备份与维护”（#operations-reference）。
当前源码已支持需求内容修订与撤回，按固定修订冻结、停止核对和重排相关工作；
暂停或自动化关闭时只排队，重大决定仍由 Owner 处理。
工具注册示例：examples/tools/custom-acp.json
独立验收小项目：examples/acceptance
Windows batch 路径限制：internal/process/README.md

包内不含个人凭据、中心地址或配对信息。解压不会自动安装或启动系统服务；
initialize_center 请求启动所选中心用户服务；initialize_runner 配对并请求启动 Runner 用户服务。
开始协作：在中心本机 Owner 页面建立项目与需求，保存完整目标、非目标、验收、
关联需求、架构、目录大纲、字面允许根目录及有限累计预算，再由 Owner 一次授权。
“目标规划与 Leader”的评估通过持久化原生 Codex 只读执行完成；Leader 是配置的
CLI 执行，不是桌面聊天会话。默认模型为 gpt-6.1-sol；Owner 可选择工具实际报告的模型
与思考强度，选择固定到新 Run，不静默回退。
目标准入后，中心本机“系统自动化”开关才允许边界内普通任务与范围自动授权；默认关闭。
新目标、架构、检查、受保护变化与外部权限仍需 Owner 批准，测试不替代授权。
原生严格执行意图协议 v1（native execution intent v1）最终输出仅为严格 JSON：
version=1、status、非 null 字符串数组 requested_paths、字符串 reason、非空 summary；
拒绝额外、重复或缺失字段及 JSON 外文字。completed 是意图，不是完成证据；
Runner 仍核对实际变化、范围、固定基线及进程树静止，独立评审、中心检查与固定候选
条件发布仍须通过。意图与请求回执均不授予权限。
范围不足时须在任何编辑前停止并保持固定工作区无变化，以 scope_requested 提出准确
新增路径和原因；Runner 确认实际进程树停止并复核无变化后持久化、提交原 Run 请求。
中心核对停止与结算，再交给只读 Leader 评估；只有自动化启用、目标批准与准入仍有效、
请求属于目标字面允许根目录且政策及剩余累计预算允许，普通范围才可自动批准。
同一任务以新授权和新执行代次恢复并保留累计支出，不创建替代任务或清零预算。
重大工作或其他阻断返回 blocked，保持工作区无变化；重大工作不会自动批准，须 Owner 决策。
scope_requested 与 blocked 是停止交接，不是成功完成。
项目累计支出只作统计，无生命周期额度；目标/任务/Run 与规划重试仍有限。
请求投递、规划重试及目标/任务累计预算有限；旧版本、无变化核对失败或未知停止状态不会放行，
须保留阻断并核对进程树停止证据。
节点能力按实际报告发现，由 Owner 管理实现/评审角色、排空、禁用及容量。
详细池、来源导航、决策、规划与任务范围历史，以及真实 API/CLI 路径见 README.md。
""")

_DSH_WINDOWS = r"""
Windows dsh Web 快速接入：
解压后确认 C:\Coordica\coordica.exe 和 C:\Coordica\coordica.cordis.yml 存在。
PowerShell：dsh web --patch C:\Coordica\coordica.cordis.yml
安装在其他目录时，先修改 YAML 的 command 绝对路径。
这份配置是可选 overlay，不会覆盖已有 dsh 配置。
"""

_DSH_UNIX = r"""
macOS/Linux 的 dsh 接入：
运行 ./coordica setup --client dsh --apply，使用结果给出的本机 overlay 路径：
dsh web --patch /结果中的路径/coordica.cordis.yml
examples/dsh/coordica.cordis.yml 保留作 Windows 路径示例。
"""


def start_here(version, platform, binary):
    instructions = _BASE_GUIDE.substitute(version=version, platform=platform, binary=binary)
    instructions += _DSH_WINDOWS if platform == "windows-amd64" else _DSH_UNIX
    return instructions.encode("utf-8")
