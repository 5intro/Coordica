# 工作台视觉体系

已确认方向为建筑事务所项目档案的阅读纪律：纸白工作面、深墨文字、蓝色操作，按标题、状态、责任排列真实工作。适用首页、四池、Agent、测试、设置，以及初始化、咨询、运行、恢复和登录。工作台为 Operate；无图片、网络字体、新依赖或装饰性图案。

## Token 与排版

`styles.css` 持有共享语义 token。canvas #f4f6fa 为页面底，surface #ffffff 为工作面，subtle #edf1f7 为辅助层，ink #182236 为正文，muted #53627a 为说明，border #d7dfea 为分隔。accent #2456d9 / accent-hover #1c43b0 用于主操作、焦点、caret；selection #e9efff 用于选择和选中行。旧 paper/bone/green/red/blue 类名与变量保留兼容语义别名。

成功使用 #23633e / #eaf5ee；待处理与暂停使用 #80520b / #fff4da；失败使用 #a52c37 / #ffedf0。通知用蓝色信息态。导航、普通进度及能力记录不借用成功绿；已取消使用中性状态。状态始终保留可读文字。

系统字体按 SF Pro Text、PingFang SC、Segoe UI、Microsoft YaHei、sans-serif 顺序回退。正文与表格14px，区域标题16px，页内标题28px，说明12–13px；标题 tracking -.025em。代码12px等宽，计数和时间使用 tabular 数字。间距 token 为4、8、12、16、24px，标准控件半径5px。无入场动画，reduced-motion 取消过渡。

## 布局与交互

桌面正文限宽1480px，常规左右32px。首页目标默认单行，可展开完整内容；展开控件用 aria-expanded 和 aria-controls 关联原文。四池为平面2×2工作区域，右侧约四分之一宽展示真实主机运行动态。池与主机共用标题基线、细分隔与14px条目标题。短记录最小52px，长标题自然换行，空态随内容收缩。人工待办仅在有真实记录时出现，使用琥珀标题与明确处理入口。

1000px 以下主机移至池下；640px 以下池单列，页边16px，Dock沿用五项与可见池切换。宽表在内部滚动，长ID断行，表单和flex/grid子项可收缩。底部预留112px及安全区。

Dock 使用 #182236 底、#26344c hover、#dfe8f8 文字；当前项蓝底白字。仅Dock有带实际偏移的柔和阴影。保留原生1.6px SVG图标、中文标签、aria-current和键盘操作。控件共享蓝色焦点、选中、hover、active及disabled语言；危险主按钮红底白字。高级证据、输出和能力报告维持现有渐进展开。

## Finish review 与实际验证

代码检查已覆盖四份CSS的颜色用途、紧凑首页、响应式规则、所有共享控件与状态；Impeccable机械检测返回空数组。无shipping raster，因此无图片来源记录。

结论：实现与代码级检查完成；真实浏览器视觉验收待Root在固定候选上执行。当前工具未提供浏览器能力，不能证明1280×800、320/390px实际渲染、像素密度或键盘浏览器旅程。本地Vitest为jsdom组件测试，不是浏览器E2E；macOS执行不代表Windows/Linux实测。功能边界沿用E与D，不改API、授权或业务状态机。

本轮实际执行：从指定tool-cache复制npm缓存后，npm --prefix web ci --offline --cache "$PWD/.cache/coordica-npm" --no-audit --no-fund 成功（104 packages）；最终 npm --prefix web test -- --maxWorkers=2 为22文件、223测试通过；npm --prefix web run build 成功（TypeScript与Vite），保留对应dist。基础文本token对比计算覆盖正文、辅助文本、蓝色操作、三类状态与Dock，最小5.46:1；这是色值计算，不代替浏览器逐元素检查。已清理本轮node_modules、tsbuildinfo和本地npm缓存。


## G：最终验收修正

沿用 Root 已在固定候选完成的桌面 1280×800、手机 390×844/320px 验收方向，不开启新视觉方案。HTML 源 theme-color 与 canvas #f4f6fa 一致，并重新构建 dist。SectionTitle 不再含装饰 eyebrow 参数与 DOM，遗留装饰节点及对应 CSS 已删除；必要凭据、角色与备份位置事实保留。changes_requested 维持红色，移除 warning 集合死项。

任务列表以标题与状态成组，只在实际阻断时显示原因；原始任务、候选、运行标识和阻断值放任务技术详情。当前运行与候选、前置依赖仍可访问，任务已用/预留/上限保持独立有限预算语义。套件列表优先名称、阶段、版本、启用状态、中文分类、平台、失败阻断配置与需求标题深链接；完整匹配规则、输出目录、检查命令与原始分类、ID 按需展开。分类显示映射不改 Owner 值，未修改分类时保存保留原数组顺序。已知阻断与决策种类中文显示，未知值原样；不改变待办集合及后台状态。

实际验证：按指定离线缓存执行 npm ci 成功；最终 `npm --prefix web test -- --maxWorkers=2` 为 22 文件、226 测试通过，包含新增任务信息分组、原始阻断值保留、需求标题链接和 Owner 套件完整载荷回归；`npm --prefix web run build` 成功并保留匹配 dist；Impeccable detect 返回 []。原有授权、恢复、取消、准备停止及咨询丢 ACK 断言保留，行为查询随中文术语同步。本轮未执行真实浏览器 E2E 或 Windows/Linux 实测；最终浏览器确认由 Root 完成。清理本次 node_modules、tsbuildinfo 与 npm 缓存。
