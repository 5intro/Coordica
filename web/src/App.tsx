import { useMemo, useState } from 'react';
import { ApiClient } from './api/client';
import { Empty, Feedback, time } from './components/ui';
import { useSnapshot } from './hooks/useSnapshot';
import { Login, TOKEN_KEY } from './features/Login';
import { ProjectSetup } from './features/ProjectSetup';
import { Overview } from './features/Overview';
import { Requirements } from './features/Requirements';
import { Tasks } from './features/Tasks';
import { Runs } from './features/Runs';
import { Candidates } from './features/Candidates';
import { Audit } from './features/Audit';
import { Settings } from './features/Settings';

export type Page = 'overview' | 'requirements' | 'tasks' | 'runs' | 'candidates' | 'audit' | 'settings';
const pages: { id: Page; label: string; number: string }[] = [
  { id: 'overview', label: '项目概览', number: '01' }, { id: 'requirements', label: '需求池', number: '02' },
  { id: 'tasks', label: '任务池', number: '03' }, { id: 'runs', label: '运行与节点', number: '04' },
  { id: 'candidates', label: '集成候选', number: '05' }, { id: 'audit', label: '审计记录', number: '06' },
  { id: 'settings', label: '政策与恢复', number: '07' },
];
function storedToken() { try { return sessionStorage.getItem(TOKEN_KEY) ?? ''; } catch { return ''; } }

export function App() {
  const [token, setToken] = useState(storedToken);
  function connect(value: string, remember: boolean) {
    try { if (remember) sessionStorage.setItem(TOKEN_KEY, value); else sessionStorage.removeItem(TOKEN_KEY); } catch { /* Private mode can deny sessionStorage. */ }
    setToken(value);
  }
  function logout() { try { sessionStorage.removeItem(TOKEN_KEY); } catch { /* No persistent credential is required. */ } setToken(''); }
  return token ? <Workspace key={token} token={token} logout={logout} /> : <Login onConnect={connect} />;
}

function Workspace({ token, logout }: { token: string; logout: () => void }) {
  const client = useMemo(() => new ApiClient(token), [token]);
  const { snapshot, error, updatedAt, loading, refresh } = useSnapshot(client);
  const [page, setPage] = useState<Page>('overview');
  const [focusId, setFocusId] = useState<string>();
  const [viewKey, setViewKey] = useState(0);
  function navigate(next: Page, id?: string) { setPage(next); setFocusId(id); setViewKey((v) => v + 1); window.scrollTo({ top: 0, behavior: 'auto' }); }
  const waitingApproval = (snapshot?.tasks ?? []).filter((t) => t.state === 'draft').length;
  const project = snapshot?.project;
  return <div className="workspace"><a href="#main-content" className="skip-link">跳转到主要内容</a><aside className="sidebar"><button className="wordmark brand-button" onClick={() => navigate('overview')} aria-label="Coordica 项目概览">Coordica<span className="wordmark-dot" /></button><div className="project-label"><span className="project-monogram">{project?.name.slice(0, 1) || 'C'}</span><div><strong>{project?.name || '新项目'}</strong><small>阶段 A · 受控协作</small></div></div><nav aria-label="工作台页面">{pages.map((item) => <button key={item.id} className={page === item.id ? 'nav-item active' : 'nav-item'} aria-current={page === item.id ? 'page' : undefined} onClick={() => navigate(item.id)}><span className="nav-number">{item.number}</span><span>{item.label}</span>{item.id === 'tasks' && waitingApproval > 0 && <span className="nav-count">{waitingApproval}</span>}</button>)}</nav><div className="sidebar-bottom"><div className={`connection ${error ? 'offline' : loading ? 'loading' : 'online'}`}><span />{error ? '中心连接异常' : loading ? '正在连接中心' : '中心已连接'}</div><small>单中心 · 局域网</small><button className="text-button" onClick={logout}>断开工作台</button></div></aside><div className="main-shell"><div className="topbar"><span>{pages.find((item) => item.id === page)?.label}</span><div><span className="snapshot-time">{updatedAt ? `状态更新 ${time(updatedAt)}` : '等待中心快照'}</span><button className="text-button" onClick={refresh} aria-label="刷新中心状态">刷新 ↻</button><button className="text-button mobile-logout" onClick={logout}>断开</button></div></div><main id="main-content" tabIndex={-1}><Feedback error={error ? `${error}${snapshot ? ' 当前显示上次成功取得的状态。' : ''}` : ''} />{!snapshot ? <div className="panel connection-panel"><Empty title={loading ? '正在读取中心状态' : '尚未取得中心状态'}>{loading ? '仅显示服务端实际记录。' : '确认 owner token 与中心连接后重试。'}</Empty>{!loading && <div className="actions"><button className="primary" onClick={refresh}>重新连接</button><button className="secondary" onClick={logout}>更换凭据</button></div>}</div> : !project ? <ProjectSetup client={client} refresh={refresh} /> : <div className="page-enter" key={`${page}-${viewKey}`}>{page === 'overview' && <Overview snapshot={snapshot} navigate={navigate} />}{page === 'requirements' && <Requirements requirements={snapshot.requirements ?? []} client={client} refresh={refresh} />}{page === 'tasks' && <Tasks snapshot={snapshot} client={client} refresh={refresh} initialId={focusId} />}{page === 'runs' && <Runs snapshot={snapshot} client={client} refresh={refresh} />}{page === 'candidates' && <Candidates snapshot={snapshot} client={client} refresh={refresh} />}{page === 'audit' && <Audit events={snapshot.events ?? []} />}{page === 'settings' && <Settings project={project} client={client} refresh={refresh} />}</div>}<footer className="workbench-footer"><span>COORDICA / CONTROLLED COLLABORATION</span><span>批准 · 运行 · 评审 · 验证 · 集成</span></footer></main></div></div>;
}
