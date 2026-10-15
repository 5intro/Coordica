---
name: Coordica
description: macOS 中性组件工作台（Operate）
colors:
  canvas: "#f5f5f5"
  surface: "#ffffff"
  subtle: "#eeeeef"
  ink: "#242426"
  muted: "#606065"
  border: "#dedee1"
  accent: "#454549"
  focus: "#707077"
  selection: "#e7e7ea"
  success: "#23633e"
  warning: "#80520b"
  failure: "#a52c37"
rounded:
  control: "6px"
  group: "10px"
  dock: "16px"
---

# 已确认组件方向

本次用户指定的 macOS 方向替代此前视觉条款。应用是浏览器里的工作台，无系统桌面模拟、窗口交通灯、Apple 资源、外部字体或新依赖。字体依次为 -apple-system、BlinkMacSystemFont、SF Pro Text、PingFang SC、Segoe UI、Microsoft YaHei、sans-serif。标题 22px/600、区域 16px/600、正文 14px、辅助 12–13px。间距 4/8/12/16/24px。

## 工具与列表

页内标题和主要动作紧凑排列。分段控件用浅灰轨道、白色选中项与轻微向下阴影；高级筛选按需展开。列表有列头、行分隔、中性选中态，标题优先。桌面控件 32–36px，手机至少 40px；手机列表改为单列条目。状态文字始终保留，成功、错误和警示只用于小面积语义，不铺满画布。未知状态保留原值。

## 设置与测试

项目设置是组标题、白色圆角组与左名称右实际动作的成组行。自动化使用受控 checkbox，保留保存、未保存、失败及版本冲突反馈。测试套件、记录、覆盖就绪分别操作；执行表单在选择套件后展开。报告摘要包含套件名称、结果、时间和平台，检查结果与有界输出渐进展开。

## Dock 与响应式

八入口浅色 Dock，16px 圆角、细边、向下柔影；当前项浅灰与深色文字、小活动点。手机保留五入口及池内分段。底部留出 safe-area 与正文空间，320px 可用。无放大跳动。焦点使用中性 focus，键盘及 aria-current 保留；reduced-motion 关闭过渡。

## 信息与权限

不在普通标题、按钮、选项或可访问名称中使用内部 UID。技术详情默认关闭，仍保留完整证据及关联值供排障。完整 SHA 在真正输入与技术详情显示，业务摘要使用短 SHA。警告随相应动作出现，权限、版本校验、取消、恢复和固定候选证据不因设计变化削弱。
