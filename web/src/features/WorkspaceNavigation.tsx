import type { Page } from '../App';
import { GlassDock, type DockItem } from '../components/material/GlassDock';

type Props = { page: Page; navigate: (page: Page) => void };
const managementPages: { id: Page; label: string }[] = [
  { id: 'settings', label: '项目' }, { id: 'automation', label: '系统' }, { id: 'connection', label: '接入' },
  { id: 'initialization', label: '项目准备' }, { id: 'runs', label: '运行与恢复' }, { id: 'audit', label: '审计记录' },
];
const dockPages: DockItem[] = [
  { id: 'overview', name: '总览', icon: 'home' }, { id: 'requirements', name: '需求池', icon: 'idea' },
  { id: 'tasks', name: '任务池', icon: 'task' }, { id: 'candidates', name: '审查池', icon: 'review' },
  { id: 'blocks', name: '阻断池', icon: 'block' }, { id: 'agents', name: 'Agent 管理', icon: 'agents' },
  { id: 'planning', name: 'Leader 活动', icon: 'activity' },
  { id: 'testing', name: '测试中心', icon: 'test' }, { id: 'settings', name: '设置', icon: 'settings' },
];
const dockAppearance = { motion: true, refraction: true };

export function SettingsNavigation({ page, navigate }: Props) {
  if (!managementPages.some((item) => item.id === page)) return null;
  return <>
    <nav className="settings-switch" aria-label="管理与设置">
      {managementPages.slice(0, 3).map((item) => <button key={item.id}
        aria-current={page === item.id ? 'page' : undefined} onClick={() => navigate(item.id)}>{item.label}</button>)}
    </nav>
    {page === 'settings' && <nav className="settings-group settings-links" aria-label="更多项目设置">
      {managementPages.slice(3).map((item) => <button className="settings-row text-button" key={item.id}
        onClick={() => navigate(item.id)}>{item.label}<span aria-hidden="true">›</span></button>)}
    </nav>}
    {managementPages.slice(3).some((item) => item.id === page)
      && <button className="text-button" onClick={() => navigate('settings')}>返回设置</button>}
  </>;
}

export function WorkspaceDock({ page, navigate }: Props) {
  return <GlassDock label="工作台页面" items={dockPages}
    active={managementPages.some((entry) => entry.id === page) ? 'settings' : page}
    onSelect={(id) => navigate(id as Page)} appearance={dockAppearance} />;
}
