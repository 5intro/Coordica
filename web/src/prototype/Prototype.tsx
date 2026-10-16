import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { PoolRow } from '../components/material/PoolRow';
import { Glass } from './Glass';
import { GlassDock } from '../components/material/GlassDock';
import { dockItems, initialRecords, pools, type Pool, type RecordItem, type Tone } from './data';
import { Icon, type IconName } from './icons';
import './prototype.css';

type Panel = { kind: 'record'; id: string } | { kind: 'pool'; pool: Pool } | { kind: 'new' | 'search' | 'appearance' | 'agents' | 'testing'; id?: string };
type Wallpaper = 'paper' | 'mist' | 'porcelain';
type Appearance = { wallpaper: Wallpaper; motion: boolean; refraction: boolean };
const wallpapers: { id: Wallpaper; name: string }[] = [{ id: 'paper', name: '米白' }, { id: 'mist', name: '亚麻' }, { id: 'porcelain', name: '暖瓷' }];
const statuses = ['开发中', '自测通过', '预合并通过', '待独立审查'];

function getAppearance(): Appearance {
  const defaults: Appearance = { wallpaper: 'paper', motion: !window.matchMedia('(prefers-reduced-motion: reduce)').matches, refraction: true };
  try {
    const saved = JSON.parse(localStorage.getItem('coordica.visual-prototype.appearance') || 'null') as Partial<Appearance> | null;
    if (saved) return { wallpaper: wallpapers.some((item) => item.id === saved.wallpaper) ? saved.wallpaper! : defaults.wallpaper, motion: typeof saved.motion === 'boolean' ? saved.motion && defaults.motion : defaults.motion, refraction: typeof saved.refraction === 'boolean' ? saved.refraction : true };
  } catch { /* A stale appearance preference never prevents opening the sample. */ }
  return defaults;
}

function Glyph({ icon, tone, size = 'small' }: { icon: IconName; tone: Tone | 'brand'; size?: 'small' | 'large' }) {
  return <span className={`glyph tone-${tone} glyph-${size}`}><Icon name={icon} size={size === 'large' ? 28 : 19}/></span>;
}

function Badge({ tone, children }: { tone: Tone; children: React.ReactNode }) {
  return <span className={`status-badge tone-${tone}`}>{children}</span>;
}


function RecordRow({ item, onOpen, refract, attention = false }: { item: RecordItem; onOpen: (id: string) => void; refract: boolean; attention?: boolean }) {
  return <Glass className="record-glass" radius={14} blur={.35} refract={refract}><button className={`record-row ${attention ? 'record-attention' : ''}`} onClick={() => onOpen(item.id)}>
    <span className="record-copy"><strong>{item.title}</strong><span><Badge tone={item.tone}>{item.status}</Badge><span className="record-owner">{item.pool === 'ideas' ? item.time : item.owner}</span></span></span>
  </button></Glass>;
}

function AgentAvatar({ tone, leader = false }: { tone: Tone; leader?: boolean }) {
  return <span className={`agent-avatar tone-${tone}`} aria-hidden="true">{leader ? 'L' : 'C'}<i/></span>;
}

function AgentSidebar({ openRecord, openAgents }: { openRecord: (id: string) => void; openAgents: (id?: string) => void }) {
  return <aside className="collaboration" aria-label="主机与 Agent 动态">
    <div className="section-heading"><h2>此刻的协作</h2><button className="icon-button" aria-label="查看全部 Agent" onClick={() => openAgents()}><Icon name="arrow" size={17}/></button></div>
    <button className="leader-line" onClick={() => openAgents('leader')}><AgentAvatar tone="purple" leader/><span><strong>Leader</strong><small>正在梳理下一步计划</small></span><span className="working-label">规划</span></button>
    <div className="host-heading"><span><Icon name="mac" size={17}/>这台 Mac</span><small><i/>中心主机</small></div>
    <button className="agent-work" onClick={() => openRecord('task-pools')}><span className="agent-work-top"><AgentAvatar tone="blue"/><span><strong>Codex</strong><small>开发 Agent</small></span><span className="activity-bars"><i/><i/><i/><i/></span></span><span className="agent-current">实现四池列表与详情交互</span><span className="agent-step"><i/>正在连接详情面板</span></button>
    <button className="agent-work" onClick={() => openRecord('task-dock')}><span className="agent-work-top"><AgentAvatar tone="orange"/><span><strong>Codex</strong><small>开发 Agent</small></span><span className="activity-bars"><i/><i/><i/><i/></span></span><span className="agent-current">打磨 Dock 的玻璃与弹性</span><span className="agent-step"><i/>正在校准边缘高光</span></button>
    <div className="host-heading second-host"><span><Icon name="windows" size={15}/>Windows 工作站</span><small><i/>已连接</small></div>
    <button className="agent-work compact-agent" onClick={() => openRecord('review-permission')}><span className="agent-work-top"><AgentAvatar tone="purple"/><span><strong>Claude Code</strong><small>独立审查</small></span><Icon name="review" size={16}/></span><span className="agent-current">审查交互层权限边界</span></button>
    <div className="collaboration-footer"><span className="stacked-dots"><i/><i/><i/></span><span>4 个 Agent · 2 台主机</span></div>
  </aside>;
}

export function Prototype() {
  const [records, setRecords] = useState(initialRecords);
  const [appearance, setAppearance] = useState(getAppearance);
  const [panel, setPanel] = useState<Panel | null>(null);
  const [history, setHistory] = useState<Panel[]>([]);
  const [closing, setClosing] = useState(false);
  const [tab, setTab] = useState('overview');
  const [query, setQuery] = useState('');
  const [toast, setToast] = useState('');
  const [checked, setChecked] = useState<Record<string, boolean>>({});
  const [flow, setFlow] = useState(-1);
  const [playing, setPlaying] = useState(false);
  const [activeDock, setActiveDock] = useState('home');
  const [resolved, setResolved] = useState(false);
  const [newTitle, setNewTitle] = useState('');
  const [newDescription, setNewDescription] = useState('');
  const drawer = useRef<HTMLElement>(null);
  const origin = useRef<HTMLElement | null>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => { try { localStorage.setItem('coordica.visual-prototype.appearance', JSON.stringify(appearance)); } catch { /* Nonessential local preferences. */ } }, [appearance]);
  useEffect(() => { if (!toast) return; const timer = setTimeout(() => setToast(''), 4200); return () => clearTimeout(timer); }, [toast]);
  useEffect(() => () => clearTimeout(closeTimer.current), []);

  const open = useCallback((next: Panel, back = false) => {
    clearTimeout(closeTimer.current);
    setClosing(false);
    if (!panel) origin.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    if (back && panel) setHistory((previous) => [...previous, panel]);
    else setHistory([]);
    setPanel(next);
    setTab('overview');
    setQuery('');
  }, [panel]);

  const close = useCallback(() => {
    if (!panel) return;
    setClosing(true);
    closeTimer.current = setTimeout(() => {
      setPanel(null);
      setClosing(false);
      setActiveDock('home');
      setHistory([]);
      origin.current?.focus({ preventScroll: true });
    }, appearance.motion ? 240 : 0);
  }, [appearance.motion, panel]);

  useEffect(() => {
    function keydown(event: KeyboardEvent) {
      if (event.key === 'Escape') close();
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); open({ kind: 'search' }); }
    }
    window.addEventListener('keydown', keydown);
    return () => window.removeEventListener('keydown', keydown);
  }, [close, open]);

  useEffect(() => {
    if (panel && !closing) drawer.current?.querySelector<HTMLButtonElement>('.panel-close')?.focus({ preventScroll: true });
  }, [panel, closing]);

  useEffect(() => {
    if (!playing || flow < 0) return;
    if (flow >= 3) { setPlaying(false); return; }
    const timer = setTimeout(() => {
      const next = flow + 1;
      setFlow(next);
      setRecords((previous) => previous.map((item) => item.id === 'task-pools' ? { ...item, pool: next === 3 ? 'reviews' : 'tasks', tone: next === 3 ? 'purple' : 'blue', status: statuses[next], events: [...item.events, { text: next === 1 ? '开发自测通过' : next === 2 ? '预合并检查与相关测试通过' : '进入审查池，等待独立审查', time: '刚刚' }] } : item));
      if (next === 3) setToast('演示：任务已通过预合并检查，进入审查池');
    }, 2100);
    return () => clearTimeout(timer);
  }, [playing, flow]);

  function openRecord(id: string, back = false) { open({ kind: 'record', id }, back); }
  function selectDock(id: string) {
    setActiveDock(id);
    if (id === 'home') { close(); return; }
    const pool = pools.find((item) => item.id === id);
    if (pool) open({ kind: 'pool', pool: pool.id });
    else if (id === 'agents' || id === 'appearance' || id === 'testing') open({ kind: id });
  }
  function playFlow() {
    if (playing) { setPlaying(false); return; }
    if (flow < 0 || flow === 3) {
      const original = initialRecords.find((item) => item.id === 'task-pools')!;
      setRecords((previous) => previous.map((item) => item.id === original.id ? original : item));
      setFlow(0);
    }
    setPlaying(true);
    openRecord('task-pools');
    setTab('activity');
  }
  function submitRequirement(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const title = newTitle.trim();
    if (title.length < 2) return;
    setRecords((previous) => [{ id: `sample-${crypto.randomUUID()}`, pool: 'ideas', title, summary: newDescription.trim() || '你刚加入的想法，等待进一步讨论。', status: '待规划', tone: 'orange', owner: 'Leader', role: '待规划', host: '这台 Mac', time: '刚刚', checks: ['与 Leader 确认需求与验收标准'], related: [], events: [{ text: '你在样稿中加入了需求', time: '刚刚' }] }, ...previous]);
    setNewTitle('');
    setNewDescription('');
    close();
    setToast('样稿中已加入需求池，仅保存在本次页面中');
  }
  function decide(value: string) {
    setResolved(true);
    setRecords((previous) => previous.filter((item) => item.id !== 'block-release'));
    close();
    setToast(`演示决定：${value}。真实项目未发生变更`);
  }
  const current = panel?.kind === 'record' ? records.find((item) => item.id === panel.id) : undefined;
  const currentPool = pools.find((item) => item.id === (panel?.kind === 'pool' ? panel.pool : current?.pool));
  const panelName = current?.title || (panel?.kind === 'pool' ? currentPool?.title : panel?.kind === 'new' ? '新需求' : panel?.kind === 'search' ? '搜索工作台' : panel?.kind === 'appearance' ? '样稿外观' : panel?.kind === 'agents' ? '协作中的 Agent' : '测试概况');
  const filtered = records.filter((item) => (panel?.kind !== 'pool' || item.pool === panel.pool) && `${item.title} ${item.summary} ${item.owner}`.toLowerCase().includes(query.toLowerCase().trim()));
  const flowChecks = flow < 0 ? 1 : Math.min(3, flow + 1);

  return <div className={`prototype wallpaper-${appearance.wallpaper} ${appearance.motion ? '' : 'reduce-motion'} ${panel ? 'has-panel' : ''}`}>
    <div className="ambient-scene" aria-hidden="true"><div className="ambient-top"/><div className="ambient-bottom"/></div>
    <div className="workbench-shell">
      <header className="app-header"><button className="brand" onClick={() => { close(); setActiveDock('home'); }} aria-label="Coordica 项目概览"><span className="wordmark">Coordica<span className="wordmark-dot"/></span></button>
        <div className="app-header-right"><span className="sample-indicator">交互样稿<span/>演示数据</span><button className="search-trigger" aria-label="搜索工作台" onClick={() => open({ kind: 'search' })}><Icon name="search" size={17}/><kbd>⌘ K</kbd></button><button className="owner-avatar" aria-label="查看样稿外观" onClick={() => { setActiveDock('appearance'); open({ kind: 'appearance' }); }}>我</button></div>
      </header>
      <main className="home-content">
        <div className="page-heading"><div><h1>项目概览</h1><p>前端工作台重构</p></div><div className="page-actions"><button className={`flow-button ${playing ? 'playing' : ''}`} onClick={playFlow}><Icon name={playing ? 'pause' : 'play'} size={15}/>{playing ? '暂停流转' : flow === 3 ? '重播流转' : '演示流转'}</button><button className="primary-button" onClick={() => open({ kind: 'new' })}><Icon name="plus" size={17}/>新需求</button></div></div>
        <div className="overview-layout"><div className="main-column">
          {!resolved ? <button className="attention-banner" onClick={() => openRecord('block-release')}><span className="attention-icon"><Icon name="block" size={21}/></span><span className="attention-copy"><small>需要你处理<span>1</span></small><strong>确认 Windows 发布策略</strong></span><span className="attention-action">查看决策<Icon name="arrow" size={17}/></span></button> : <div className="attention-banner resolved-banner"><span className="attention-icon"><Icon name="check" size={21}/></span><span className="attention-copy"><small>决定已记录</small><strong>发布工作可以继续了</strong></span><span className="resolved-label">样稿演示</span></div>}
          <div className="pool-grid">{[pools.slice(0,2),pools.slice(2)].map((pair,rowIndex) => <PoolRow row={rowIndex} count={Math.max(...pair.map((pool) => records.filter((item) => item.pool === pool.id).length))} storageKey="coordica.visual-prototype.pool-height" key={rowIndex}>{pair.map((pool) => {
            const items = records.filter((item) => item.pool === pool.id);
            return <section className="pool-group" key={pool.id} aria-label={pool.title}>
              <header className="pool-header"><h2><button onClick={() => { setActiveDock(pool.id); open({ kind: 'pool', pool: pool.id }); }}>{pool.title}</button></h2><span className="pool-count">{items.length}</span></header><div className={`pool-surface pool-well pool-${pool.tone} ${activeDock === pool.id ? 'pool-focused' : ''}`}>
              <div className="pool-rows">{items.map((item) => <RecordRow key={item.id} item={item} onOpen={openRecord} refract={appearance.refraction} attention={item.id === 'task-pools' && playing}/>)}</div></div>
            </section>;
          })}</PoolRow>)}</div>
          <div className="workspace-footer"><span className="completion-note"><span className="completed-icon"><Icon name="check" size={12}/></span>今天已有 <strong>6 项工作</strong> 顺利交付</span><button onClick={() => { setActiveDock('testing'); open({ kind: 'testing' }); }}>查看测试概况<Icon name="arrow" size={14}/></button></div>
        </div><AgentSidebar openRecord={openRecord} openAgents={(id) => { setActiveDock('agents'); open({ kind: 'agents', id }); }}/></div>
      </main>
    </div>
    <GlassDock items={dockItems} active={activeDock} onSelect={selectDock} appearance={appearance}/>
    {panel && <>
      <div className={`panel-scrim ${closing ? 'closing' : ''}`} onClick={close} aria-hidden="true"/>
      <aside ref={drawer} className={`detail-panel ${closing ? 'closing' : ''}`} role="dialog" aria-modal="false" aria-label={panelName}>
        <Glass className="panel-glass" radius={30} blur={9} refract={appearance.refraction}>
          <div className="panel-toolbar"><div>{history.length > 0 && <button className="icon-button" aria-label="返回上一项" onClick={() => { const next = history[history.length - 1]; setHistory((previous) => previous.slice(0, -1)); setPanel(next); setTab('overview'); }}><Icon name="back" size={18}/></button>}<span>{currentPool ? currentPool.title : panel?.kind === 'new' ? '把想法留下来' : panel?.kind === 'appearance' ? '为你而设' : 'Coordica'}</span></div><button className="icon-button panel-close" aria-label="关闭详情面板" onClick={close}><Icon name="close" size={19}/></button></div>
          <div className="panel-scroll" key={current?.id || panel.kind}>
            {current && <>
              <div className="record-detail-heading"><Glyph icon={currentPool?.icon || 'task'} tone={currentPool?.tone || 'blue'} size="large"/><Badge tone={current.tone}>{current.status}</Badge><h2>{current.title}</h2><div className="record-meta"><span><AgentAvatar tone={current.tone} leader={current.owner === 'Leader'}/>{current.owner}</span><span>{current.host}</span><span>{current.time}</span></div></div>
              <div className="panel-tabs" role="tablist" aria-label="详情内容">{[{ id: 'overview', title: '概览' }, { id: 'activity', title: '动态' }, { id: 'related', title: `关联 ${current.related.length}` }].map((item) => <button key={item.id} role="tab" id={`tab-${item.id}`} aria-selected={tab === item.id} aria-controls="record-tab-panel" onClick={() => setTab(item.id)}>{item.title}</button>)}</div>
              <div className="tab-content" id="record-tab-panel" role="tabpanel" aria-labelledby={`tab-${tab}`} key={tab}>
                {tab === 'overview' && <><p className="detail-summary">{current.summary}</p><section className="detail-section"><h3>验收要点</h3><div className="checklist">{current.checks.map((text, index) => {
                  const key = `${current.id}:${index}`;
                  const done = checked[key] ?? (current.pool === 'reviews' ? index < 2 || current.status === '审查通过' : current.id === 'task-pools' ? index < flowChecks : current.pool === 'ideas' && current.status === '规划完成');
                  return <label key={key} className={done ? 'is-checked' : ''}><input type="checkbox" checked={done} onChange={(event) => setChecked((previous) => ({ ...previous, [key]: event.target.checked }))}/><span className="checkbox-shape"><Icon name="check" size={12}/></span><span>{text}</span></label>;
                })}</div></section>{current.id === 'block-release' && <section className="detail-section decision-section"><h3>本轮怎么发布？</h3><button className="decision-option" onClick={() => decide('先发布 macOS，Windows 后续补齐')}><span><strong>先发布 macOS</strong><small>Windows 完成签名验证后跟进</small></span><Icon name="arrow" size={18}/></button><button className="decision-option" onClick={() => decide('等待三平台一起发布')}><span><strong>三平台一起发布</strong><small>保持发布工作等待，先推进其他任务</small></span><Icon name="arrow" size={18}/></button></section>}{current.pool === 'reviews' && <div className="gate-note"><Icon name="check" size={16}/><span>开发自测与预合并检查已通过</span></div>}{current.id === 'task-pools' && <button className="wide-action" onClick={playFlow}><Icon name={playing ? 'pause' : 'play'} size={15}/>{playing ? '暂停这次演示' : '体验开发 → 自测 → 预检查 → 审查'}</button>}</>}
                {tab === 'activity' && <><div className="activity-title"><h3>工作留下的足迹</h3>{playing && current.id === 'task-pools' && <span className="live-chip"><i/>演示流转中</span>}</div><ol className="event-timeline">{current.events.map((event, index) => <li key={`${index}:${event.text}`}><span className={`event-point ${index === current.events.length - 1 ? 'latest' : ''}`}/><div><strong>{event.text}</strong><time>{event.time}</time></div></li>)}</ol>{current.id === 'task-pools' && <div className="flow-steps" aria-label="演示流程">{['开发', '自测', '预检查', '审查'].map((step, index) => <span key={step} className={flow >= index ? 'step-done' : ''}><i>{flow > index ? <Icon name="check" size={11}/> : index + 1}</i>{step}</span>)}</div>}</>}
                {tab === 'related' && <section className="detail-section related-section"><h3>保持上下文相连</h3>{current.related.length ? current.related.map((id) => { const item = records.find((record) => record.id === id); return item ? <RecordRow key={id} item={item} refract={appearance.refraction} onOpen={(next) => openRecord(next, true)}/> : null; }) : <p className="empty-message">暂无关联工作</p>}</section>}
              </div>
            </>}
            {(panel.kind === 'pool' || panel.kind === 'search') && <><div className="collection-heading">{currentPool && <Glyph icon={currentPool.icon} tone={currentPool.tone} size="large"/>}<h2>{panel.kind === 'search' ? '搜索工作台' : currentPool?.title}</h2><p>{panel.kind === 'search' ? '需求、任务、审查与阻断，一起查找。' : currentPool?.caption}</p></div><label className="panel-search"><Icon name="search" size={18}/><input aria-label="搜索条目" autoFocus value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索标题、内容或 Agent"/><kbd>⌘ K</kbd></label><div className="collection-list">{filtered.length ? filtered.map((item) => <RecordRow key={item.id} item={item} refract={appearance.refraction} onOpen={(id) => openRecord(id, true)}/>) : <div className="empty-search"><Icon name="search" size={29}/><strong>没有找到相关工作</strong><p>换一个关键词试试。</p><button className="subtle-button" onClick={() => setQuery('')}>清空搜索</button></div>}</div>{panel.kind === 'pool' && panel.pool === 'ideas' && <button className="wide-action" onClick={() => open({ kind: 'new' }, true)}><Icon name="plus" size={17}/>加入新需求</button>}</>}
            {panel.kind === 'new' && <><div className="collection-heading"><Glyph icon="idea" tone="orange" size="large"/><h2>新需求</h2><p>先记下来，再一起想清楚。</p></div><form className="requirement-form" onSubmit={submitRequirement}><label>需求名称<input autoFocus value={newTitle} minLength={2} maxLength={120} required onChange={(event) => setNewTitle(event.target.value)} placeholder="比如，让 Agent 的进展更容易看懂"/></label><label>补充细节<span className="optional-label">可选</span><textarea value={newDescription} maxLength={3000} rows={6} onChange={(event) => setNewDescription(event.target.value)} placeholder="你希望解决什么问题？什么样的结果会让你满意？"/></label><p className="form-note"><Icon name="idea" size={15}/>这是演示需求，不会发送给真实 Leader。</p><button className="primary-button" disabled={newTitle.trim().length < 2}><Icon name="plus" size={17}/>加入需求池</button></form></>}
            {panel.kind === 'appearance' && <><div className="collection-heading"><Glyph icon="sun" tone="orange" size="large"/><h2>样稿外观</h2><p>试试透过玻璃的不同光线。</p></div><section className="detail-section appearance-section"><h3>空间色彩</h3><div className="wallpaper-options">{wallpapers.map((item) => <button key={item.id} className={`wallpaper-option wallpaper-${item.id} ${appearance.wallpaper === item.id ? 'selected' : ''}`} aria-pressed={appearance.wallpaper === item.id} onClick={() => setAppearance((previous) => ({ ...previous, wallpaper: item.id }))}><span className="wallpaper-preview"/><span>{item.name}</span>{appearance.wallpaper === item.id && <i><Icon name="check" size={12}/></i>}</button>)}</div></section><section className="appearance-controls"><label><span><strong>流体折射</strong><small>玻璃边缘弯折背后的色彩</small></span><input type="checkbox" role="switch" checked={appearance.refraction} onChange={(event) => setAppearance((previous) => ({ ...previous, refraction: event.target.checked }))}/></label><label><span><strong>动态效果</strong><small>Dock 放大、弹性与面板过渡</small></span><input type="checkbox" role="switch" checked={appearance.motion} onChange={(event) => setAppearance((previous) => ({ ...previous, motion: event.target.checked }))}/></label></section><div className="material-study"><Glass refract={appearance.refraction} className="study-lens" radius={24}><Icon name="spark" size={27}/><span>移动的光，安静的内容。</span></Glass><span className="material-study-caption">玻璃材质预览</span></div><p className="appearance-note">折射增强适用于 Chromium；其他浏览器保留磨砂玻璃。这里只改变样稿外观。</p></>}
            {panel.kind === 'agents' && <><div className="collection-heading"><Glyph icon="agents" tone="green" size="large"/><h2>协作 Agent</h2><p>2 台主机 · 4 个 Agent · 演示数据</p></div>{[{ name: 'Leader', role: '规划与协调', tone: 'purple' as Tone, host: '这台 Mac', task: '梳理下一步计划', record: 'idea-workbench', leader: true }, { name: 'Codex', role: '开发', tone: 'blue' as Tone, host: '这台 Mac', task: '实现四池列表与详情交互', record: 'task-pools' }, { name: 'Codex', role: '开发', tone: 'orange' as Tone, host: '这台 Mac', task: '打磨 Dock 的玻璃与弹性', record: 'task-dock' }, { name: 'Claude Code', role: '独立审查', tone: 'purple' as Tone, host: 'Windows 工作站', task: '交互层权限边界', record: 'review-permission' }].map((agent) => <button className="agent-detail-row" key={agent.record} onClick={() => openRecord(agent.record, true)}><AgentAvatar tone={agent.tone} leader={agent.leader}/><span><strong>{agent.name}<small>{agent.role} · {agent.host}</small></strong><span>{agent.task}</span></span><Icon name="chevron" size={16}/></button>)}</>}
            {panel.kind === 'testing' && <><div className="collection-heading"><Glyph icon="test" tone="green" size="large"/><h2>测试概况</h2><p>本轮测试概况 · 演示数据</p></div><div className="test-summary"><Icon name="check" size={24}/><div><strong>相关门禁全部通过</strong><span>仅示例结果，不代表当前项目验收</span></div></div>{[{ name: '核心链路冒烟', detail: '初始化 → 领取 → 交付', count: '8 项检查' }, { name: '黄金旅程', detail: '需求 → 开发 → 审查 → 集成', count: '12 项检查' }, { name: '权限与断线恢复', detail: '旧租约、重复回执与迟到结果', count: '16 项检查' }].map((test) => <div className="test-row" key={test.name}><span className="test-check"><Icon name="check" size={14}/></span><span><strong>{test.name}</strong><small>{test.detail}</small></span><span>{test.count}</span></div>)}<div className="gate-note"><Icon name="link" size={16}/><span>正式版中的测试证据将绑定固定提交</span></div></>}
          </div>
          <div className="panel-bottom"><span><i/>交互样稿</span><span>Esc 关闭</span></div>
        </Glass>
      </aside>
    </>}
    <div className={`toast ${toast ? 'toast-visible' : ''}`} role="status" aria-live="polite">{toast && <><span className="toast-check"><Icon name="check" size={15}/></span><span>{toast}</span><button className="icon-button" aria-label="关闭提示" onClick={() => setToast('')}><Icon name="close" size={15}/></button></>}</div>
  </div>;
}
