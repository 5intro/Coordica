import type { IconName } from './icons';

export type Pool = 'ideas' | 'tasks' | 'reviews' | 'blocks';
export type Tone = 'orange' | 'blue' | 'purple' | 'rose' | 'green' | 'gray';
export type RecordItem = {
  id: string; pool: Pool; title: string; summary: string; status: string; tone: Tone;
  owner: string; role: string; host: string; time: string; checks: string[];
  related: string[]; events: { text: string; time: string }[];
};

export const pools: { id: Pool; title: string; icon: IconName; tone: Tone; caption: string }[] = [
  { id: 'ideas', title: '需求池', icon: 'idea', tone: 'orange', caption: '想法，从这里开始' },
  { id: 'tasks', title: '任务池', icon: 'task', tone: 'blue', caption: '正在把想法变成现实' },
  { id: 'reviews', title: '审查池', icon: 'review', tone: 'purple', caption: '每次交付，都值得认真看' },
  { id: 'blocks', title: '阻断池', icon: 'block', tone: 'rose', caption: '一起解开这些结' },
];

export const initialRecords: RecordItem[] = [
  { id: 'idea-workbench', pool: 'ideas', title: '让项目进展一目了然', summary: '首页直接展示四个工作池和各主机的 Agent 动态。信息保持简洁，点击即可查看上下文、验收标准和关联工作。', status: '规划完成', tone: 'green', owner: 'Leader', role: '规划', host: '这台 Mac', time: '12 分钟前', checks: ['首页包含需求、任务、审查、阻断的具体列表', '各主机的 Agent、当前工作与状态清晰可见', '点击条目，在详情面板中查看完整上下文'], related: ['task-pools', 'task-keyboard', 'review-permission'], events: [{ text: '你提交了需求', time: '14:20' }, { text: 'Leader 完成拆解，关联了 3 项工作', time: '14:24' }] },
  { id: 'idea-glass', pool: 'ideas', title: '会呼吸的玻璃 Dock', summary: '让常用入口触手可及。玻璃随背景透色，边缘产生折射，图标随指针连续放大，选中时给出清楚的反馈。', status: '推进中', tone: 'blue', owner: 'Leader', role: '规划', host: '这台 Mac', time: '8 分钟前', checks: ['导航入口不遮挡正文', '支持鼠标、键盘和触摸操作', '减少动态效果时，内容与操作保持可用'], related: ['task-dock'], events: [{ text: '需求已确认', time: '14:26' }, { text: 'Codex 开始实现 Dock', time: '14:28' }] },
  { id: 'idea-test', pool: 'ideas', title: '把测试结果带回工作台', summary: '在同一处查看冒烟、黄金旅程和端到端测试，测试证据绑定具体提交。首页只显示需要关注的结果。', status: '待规划', tone: 'gray', owner: 'Leader', role: '待规划', host: '这台 Mac', time: '刚刚', checks: ['测试结果绑定固定版本', '失败结果能关联到修复任务', '通过状态不能由文字描述推断'], related: [], events: [{ text: '你提交了需求，等待 Leader 规划', time: '14:32' }] },
  { id: 'task-pools', pool: 'tasks', title: '实现四池列表与详情交互', summary: '首页展示四个工作池的当前事项。点击任意条目展开详情；关闭后回到原来的位置，不丢失浏览上下文。', status: '开发中', tone: 'blue', owner: 'Codex', role: '开发', host: '这台 Mac', time: '进行中 · 18 分钟', checks: ['完成首页四池布局', '接入详情面板与关联条目跳转', '补充空状态与窄屏适配'], related: ['idea-workbench', 'task-keyboard'], events: [{ text: 'Leader 分配任务给 Codex', time: '14:14' }, { text: '独立工作区已就绪', time: '14:14' }, { text: '完成列表，正在实现详情交互', time: '14:29' }] },
  { id: 'task-dock', pool: 'tasks', title: '打磨 Dock 的玻璃与弹性', summary: '实现可体验的折射材质与连续放大。高光、透色和阴影保持统一，动效尊重系统的减少动态效果设置。', status: '开发中', tone: 'blue', owner: 'Codex', role: '开发', host: '这台 Mac', time: '进行中 · 9 分钟', checks: ['完成玻璃边缘折射', '完成连续放大与选中背景过渡', '验证键盘访问和减少动态效果'], related: ['idea-glass'], events: [{ text: '任务已分配', time: '14:23' }, { text: '正在校准折射与高光', time: '14:30' }] },
  { id: 'task-keyboard', pool: 'tasks', title: '补全键盘访问与焦点返回', summary: '详情面板支持 Escape 关闭，焦点回到打开它的条目。搜索框、导航与所有操作按钮均可通过键盘使用。', status: '待开始', tone: 'gray', owner: '待分配', role: '开发', host: '尚未分配', time: '等待前置任务', checks: ['Escape 关闭详情面板', '焦点返回原条目', '所有交互具有可读的名称'], related: ['task-pools'], events: [{ text: 'Leader 创建任务', time: '14:24' }, { text: '等待详情面板的基础实现', time: '14:24' }] },
  { id: 'review-permission', pool: 'reviews', title: '交互层权限边界', summary: '预合并检查已通过，等待独立审查。重点核对本机交互层只能查询本机 Agent，远程节点不能访问 Leader 的决策接口。', status: '审查中', tone: 'purple', owner: 'Claude Code', role: '独立审查', host: 'Windows 工作站', time: '进行中 · 6 分钟', checks: ['开发自测已通过', '预合并检查已通过', '独立审查权限与状态流转'], related: ['idea-workbench'], events: [{ text: '开发 Agent 提交固定候选', time: '14:18' }, { text: '预合并检查与相关测试通过', time: '14:22' }, { text: 'Claude Code 开始独立审查', time: '14:26' }] },
  { id: 'review-status', pool: 'reviews', title: '断线恢复后的状态一致性', summary: '预合并检查通过，代码审查完成。旧租约、重复回执与迟到结果的相关检查均已通过，等待按集成规则继续。', status: '审查通过', tone: 'green', owner: 'Claude Code', role: '独立审查', host: '这台 Mac', time: '4 分钟前', checks: ['旧租约拒绝检查通过', '重复消息不会重复写入', '迟到结果不影响新的运行实例'], related: [], events: [{ text: '预合并检查通过', time: '14:20' }, { text: '独立代码审查通过', time: '14:28' }] },
  { id: 'block-release', pool: 'blocks', title: '确认 Windows 发布策略', summary: 'macOS 安装包验证通过，Windows 签名流程尚未确定。Leader 需要你选择本轮发布方式，关联工作会保持等待。', status: '需要你决定', tone: 'rose', owner: '你', role: '决策', host: '中心', time: '等待决定 · 5 分钟', checks: ['明确本轮发布范围', '记录 Windows 安装与签名的验收条件', '决定后由 Leader 继续编排'], related: ['idea-workbench'], events: [{ text: 'Leader 发现发布策略需要确认', time: '14:27' }, { text: '已暂停受影响的发布工作', time: '14:27' }] },
  { id: 'block-browser', pool: 'blocks', title: 'Safari 折射效果需要兼容方案', summary: '部分浏览器不支持通过 backdrop-filter 应用 SVG 折射。保持统一的玻璃层次，并为不支持的环境提供柔和磨砂材质。', status: '等待方案', tone: 'orange', owner: 'Leader', role: '方案确认', host: '这台 Mac', time: '2 分钟前', checks: ['可读性和交互不依赖折射', '不支持时使用磨砂玻璃', '保留减少动态效果的入口'], related: ['task-dock'], events: [{ text: '开发 Agent 标记兼容性差异', time: '14:30' }, { text: 'Leader 正在核对兼容方案', time: '14:31' }] },
];

export const dockItems: { id: string; name: string; icon: IconName; tone: Tone | 'brand'; pool?: Pool }[] = [
  { id: 'home', name: '总览', icon: 'home', tone: 'brand' },
  { id: 'ideas', name: '需求池', icon: 'idea', tone: 'orange', pool: 'ideas' },
  { id: 'tasks', name: '任务池', icon: 'task', tone: 'blue', pool: 'tasks' },
  { id: 'reviews', name: '审查池', icon: 'review', tone: 'purple', pool: 'reviews' },
  { id: 'blocks', name: '阻断池', icon: 'block', tone: 'rose', pool: 'blocks' },
  { id: 'agents', name: 'Agent', icon: 'agents', tone: 'green' },
  { id: 'testing', name: '测试', icon: 'test', tone: 'blue' },
  { id: 'appearance', name: '样稿外观', icon: 'settings', tone: 'gray' },
];
