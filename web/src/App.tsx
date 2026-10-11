import { useEffect, useMemo, useState } from 'react';
import { ApiClient, messageOf } from './api/client';
import { LocalClient } from './api/localClient';
import type { LocalInfo } from './api/localClient';
import { localConsoleURL } from './api/connection';
import { Empty, Feedback, time } from './components/ui';
import { useSnapshot } from './hooks/useSnapshot';
import { useConnectionInfo } from './hooks/useConnectionInfo';
import { Login, TOKEN_KEY } from './features/Login';
import { ProjectSetup } from './features/ProjectSetup';
import { Planning } from './features/Planning';
import { Automation } from './features/Automation';
import { Overview } from './features/Overview';
import { Requirements } from './features/Requirements';
import { Tasks } from './features/Tasks';
import { Agents } from './features/Agents';
import { Blocks } from './features/Blocks';
import { agentRows, blockRecords } from './features/management';
import { Runs } from './features/Runs';
import { Candidates } from './features/Candidates';
import { Audit } from './features/Audit';
import { Settings } from './features/Settings';
import { ConnectionSettings } from './features/ConnectionSettings';
import { RunnerWorkspace } from './features/RunnerWorkspace';

export type Page = 'planning' | 'automation' | 'blocks' | 'agents' | 'overview' | 'requirements' | 'tasks' | 'runs' | 'candidates' | 'audit' | 'settings' | 'connection';
const pages: { id: Page; label: string; number: string }[] = [
  { id: 'planning', label: '目标规划与 Leader', number: 'B1' }, { id: 'automation', label: '系统自动化', number: 'B2' },
  { id: 'overview', label: '项目概览', number: '01' }, { id: 'requirements', label: '需求池', number: '02' },
  { id: 'tasks', label: '任务池', number: '03' }, { id: 'runs', label: '运行与节点', number: '04' },
  { id: 'blocks', label: '阻断池', number: '09' }, { id: 'agents', label: 'Agent 管理', number: '10' },
  { id: 'candidates', label: '集成候选', number: '05' }, { id: 'audit', label: '审计记录', number: '06' },
  { id: 'settings', label: '政策与恢复', number: '07' }, { id: 'connection', label: '中心配置与接入', number: '08' },
];
function hashFocus() { return new URLSearchParams(window.location.hash.split('?')[1] ?? '').get('id') ?? undefined; }
function storedToken() { try { return sessionStorage.getItem(TOKEN_KEY) ?? ''; } catch { return ''; } }
function hashPage(): Page | undefined {
  const hash = window.location.hash.replace(/^#\/?/, '').split('?')[0];
  return pages.find((item) => item.id === hash)?.id;
}

export function App() {
  const [info, setInfo] = useState<LocalInfo>();
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    const client = new LocalClient();
    setError('');
    void client.info(controller.signal).then((next) => { if (!controller.signal.aborted) setInfo(next); })
      .catch((failure) => { if (!controller.signal.aborted) setError(messageOf(failure)); });
    return () => controller.abort();
  }, [retry]);
  if (info?.mode === 'runner') return <RunnerWorkspace centerConsoleURL={info.center_console_url} />;
  if (info?.mode === 'center') return <CenterApp />;
  return <main className="local-entry"><span className="wordmark">Coordica<span className="wordmark-dot" /></span><section className="panel"><Feedback error={error} /><Empty title={error ? '尚未连接本机服务' : '正在打开本机工作台'}>{error ? '确认 Coordica 服务已启动，然后重新连接。' : '读取本机页面类型。'}</Empty>{error && <button className="primary" onClick={() => setRetry((value) => value + 1)}>重新连接</button>}</section></main>;
}

function CenterApp() {
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
  const connection = useConnectionInfo(client);
  const [page, setPage] = useState<Page>(() => hashPage() ?? 'overview');
  const [focusId, setFocusId] = useState<string | undefined>(hashFocus);
  const [viewKey, setViewKey] = useState(0);
  useEffect(() => {
    function change() {
      const next = hashPage() ?? 'overview';
      setPage(next); setFocusId(hashFocus()); setViewKey((value) => value + 1);
    }
    window.addEventListener('hashchange', change);
    return () => window.removeEventListener('hashchange', change);
  }, []);
  function navigate(next: Page, id?: string) {
    const hash = `#${next}${id ? `?id=${encodeURIComponent(id)}` : ''}`;
    if (window.location.hash !== hash) window.history.pushState(null, '', hash);
    setPage(next); setFocusId(id); setViewKey((value) => value + 1);
    window.scrollTo({ top: 0, behavior: 'auto' });
  }
  const counts: Partial<Record<Page, number>> = snapshot ? { tasks: snapshot.tasks?.length ?? 0, requirements: snapshot.requirements?.length ?? 0, runs: snapshot.runs?.length ?? 0, candidates: snapshot.candidates?.length ?? 0, blocks: blockRecords(snapshot).length, agents: agentRows(snapshot).length } : {};
  const project = snapshot?.project;
  const runnerURL = localConsoleURL(connection.info?.runner_console_url);
  return <div className="workspace">
    <a href="#main-content" className="skip-link">跳转到主要内容</a>
    <aside className="sidebar">
      <button className="wordmark brand-button" onClick={() => navigate('overview')} aria-label="Coordica 项目概览">Coordica<span className="wordmark-dot" /></button>
      <div className="project-label"><span className="project-monogram">{project?.name.slice(0, 1) || 'C'}</span><div><strong>{project?.name || '新项目'}</strong><small>{snapshot?.planning_document ? `Phase B · ${snapshot.planning_document.status} · v${snapshot.planning_document.version}` : 'Phase B · 尚无目标'}</small></div></div>
      <nav aria-label="工作台页面">{pages.map((item) => <button key={item.id} className={page === item.id ? 'nav-item active' : 'nav-item'} aria-current={page === item.id ? 'page' : undefined} onClick={() => navigate(item.id)}><span className="nav-number">{item.number}</span><span>{item.label}</span>{counts[item.id] !== undefined && <span className="nav-count">{counts[item.id]}</span>}</button>)}</nav>
      <div className="sidebar-bottom"><div className={`connection ${error ? 'offline' : loading ? 'loading' : 'online'}`}><span />{error ? '中心连接异常' : loading ? '正在连接中心' : '中心已连接'}</div><small>单中心 · 局域网</small>{runnerURL && <a className="text-button local-workspace-link" href={`${runnerURL}/`}>本机 Runner 默认入口 ↗</a>}<button className="text-button" onClick={logout}>断开工作台</button></div>
    </aside>
    <div className="main-shell">
      <div className="topbar"><span>{pages.find((item) => item.id === page)?.label}</span><div><span className="snapshot-time">{updatedAt ? `状态更新 ${time(updatedAt)}` : '等待中心快照'}</span><button className="text-button" onClick={() => { refresh(); connection.reload(); }} aria-label="刷新中心状态">刷新 ↻</button><button className="text-button mobile-logout" onClick={logout}>断开</button></div></div>
      <main id="main-content" tabIndex={-1}>
        <Feedback error={error ? `${error}${snapshot ? ' 当前显示上次成功取得的状态。' : ''}` : ''} />
        {page === 'connection' ? <ConnectionSettings client={client} {...connection} project={project} openPolicies={() => navigate('settings')} />
          : !snapshot ? <div className="panel connection-panel"><Empty title={loading ? '正在读取中心状态' : '尚未取得中心状态'}>{loading ? '仅显示服务端实际记录。' : '确认 owner token 与中心连接后重试。'}</Empty>{!loading && <div className="actions"><button className="primary" onClick={refresh}>重新连接</button><button className="secondary" onClick={logout}>更换凭据</button></div>}</div>
            : !project && !['agents', 'automation', 'planning'].includes(page) ? <ProjectSetup client={client} refresh={refresh} />
              : <div className="page-enter" key={`${page}-${viewKey}`}>
                {page === 'planning' && <Planning snapshot={snapshot} client={client} refresh={refresh} initialId={focusId} />}
                {page === 'automation' && <Automation settings={snapshot.automation} client={client} refresh={refresh} />}
                {page === 'overview' && <Overview snapshot={snapshot} navigate={navigate} />}
                {page === 'requirements' && <Requirements requirements={snapshot.requirements ?? []} tasks={snapshot.tasks ?? []} navigate={navigate} client={client} refresh={refresh} />}
                {page === 'tasks' && <Tasks snapshot={snapshot} client={client} refresh={refresh} initialId={focusId} />}
                {page === 'blocks' && <Blocks snapshot={snapshot} client={client} refresh={refresh} navigate={navigate} />}
                {page === 'agents' && <Agents snapshot={snapshot} client={client} refresh={refresh} navigate={navigate} />}
                {page === 'runs' && <Runs snapshot={snapshot} client={client} refresh={refresh} initialId={focusId} />}
                {page === 'candidates' && <Candidates snapshot={snapshot} client={client} refresh={refresh} initialId={focusId} />}
                {page === 'audit' && <Audit events={snapshot.events ?? []} />}
                {page === 'settings' && project && <Settings project={project} client={client} refresh={refresh} />}
              </div>}
        <footer className="workbench-footer"><span>COORDICA / CONTROLLED COLLABORATION</span><span>批准 · 运行 · 评审 · 验证 · 集成</span></footer>
      </main>
    </div>
  </div>;
}
