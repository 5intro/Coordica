import { SOURCE_VERSION } from './api.generated';
import { useEffect, useMemo, useRef, useState } from 'react';
import { ApiClient, messageOf } from './api/client';
import { LocalClient } from './api/localClient';
import type { LocalInfo } from './api/localClient';
import { localConsoleURL } from './api/connection';
import { Empty, Feedback, time } from './components/ui';
import { useSnapshot } from './hooks/useSnapshot';
import { useConnectionInfo } from './hooks/useConnectionInfo';
import { Login, TOKEN_KEY } from './features/Login';
import { ProjectInitialization } from './features/ProjectInitialization';
import { ProjectSetup } from './features/ProjectSetup';
import { Planning } from './features/Planning';
import { Automation } from './features/Automation';
import { Overview } from './features/Overview';
import { Requirements } from './features/Requirements';
import { Tasks } from './features/Tasks';
import { Agents } from './features/Agents';
import { Blocks } from './features/Blocks';
import { routeEvent } from './hooks/useRouteField';
import { Testing } from './features/Testing';
import { Runs } from './features/Runs';
import { Candidates } from './features/Candidates';
import { Audit } from './features/Audit';
import { Settings } from './features/Settings';
import { ConnectionSettings } from './features/ConnectionSettings';
import { RunnerWorkspace } from './features/RunnerWorkspace';

export type Page = 'initialization' | 'planning' | 'automation' | 'blocks' | 'agents' | 'overview' | 'requirements' | 'tasks' | 'runs' | 'candidates' | 'audit' | 'settings' | 'connection' | 'testing';
const pages: { id: Page; label: string; number: string }[] = [
  { id: 'testing', label: '测试中心', number: '' },
  { id: 'initialization', label: '项目初始化', number: '00' },
  { id: 'planning', label: '目标规划与 Leader', number: 'B1' }, { id: 'automation', label: '系统自动化', number: 'B2' },
  { id: 'overview', label: '项目概览', number: '01' }, { id: 'requirements', label: '需求池', number: '02' },
  { id: 'tasks', label: '任务池', number: '03' }, { id: 'runs', label: '运行与节点', number: '04' },
  { id: 'blocks', label: '阻断池', number: '09' }, { id: 'agents', label: 'Agent 管理', number: '10' },
  { id: 'candidates', label: '集成候选', number: '05' }, { id: 'audit', label: '审计记录', number: '06' },
  { id: 'settings', label: '政策与恢复', number: '07' }, { id: 'connection', label: '中心配置与接入', number: '08' },
];
const poolPages: { id: Page; label: string }[] = [{ id: 'requirements', label: '需求' }, { id: 'tasks', label: '任务' }, { id: 'candidates', label: '审查' }, { id: 'blocks', label: '阻断' }];
const managementPages: { id: Page; label: string }[] = [
  { id: 'settings', label: '项目 · 政策与恢复' }, { id: 'automation', label: '系统自动化' }, { id: 'connection', label: '中心配置与接入' },
  { id: 'initialization', label: '项目初始化' }, { id: 'planning', label: '目标规划与 Leader' }, { id: 'runs', label: '运行与节点' }, { id: 'audit', label: '审计记录' },
];
const dockPages: { id: Page; label: string; short: string }[] = [
  { id: 'overview', label: '总览', short: '总览' }, { id: 'requirements', label: '需求池', short: '需求' },
  { id: 'tasks', label: '任务池', short: '任务' }, { id: 'candidates', label: '审查池', short: '审查' },
  { id: 'blocks', label: '阻断池', short: '阻断' }, { id: 'agents', label: 'Agent 管理', short: 'Agent' },
  { id: 'testing', label: '测试中心', short: '测试' }, { id: 'settings', label: '设置', short: '设置' },
];
function DockIcon({ index }: { index: number }) {
  const paths = ['M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z', 'M6 3h12v18H6z M9 8h6 M9 12h6 M9 16h4', 'M4 6h3 M10 6h10 M4 12h3 M10 12h10 M4 18h3 M10 18h10', 'M12 3l8 4v6c0 4-8 8-8 8s-8-4-8-8V7z M8 12l3 3 5-6', 'M8 3h8l5 5v8l-5 5H8l-5-5V8z M12 7v6 M12 16v1', 'M5 7h14v13H5z M9 11v3 M15 11v3 M9 17h6 M12 3v4', 'M9 3h6 M10 3v6L4 19q0 2 2 2h12q2 0 2-2L14 9V3 M8 15h8', 'M4 7h16 M4 17h16 M8 4v6 M16 14v6'];
  return <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={paths[index]} /></svg>;
}
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
  const [compact, setCompact] = useState(() => window.matchMedia?.('(max-width: 640px)').matches ?? false);
  useEffect(() => {
    const media = window.matchMedia?.('(max-width: 640px)');
    if (!media) return;
    const change = () => setCompact(media.matches);
    media.addEventListener('change', change);
    return () => media.removeEventListener('change', change);
  }, []);
  const [page, setPage] = useState<Page>(() => hashPage() ?? 'overview');
  const [focusId, setFocusId] = useState<string | undefined>(hashFocus);
  const [visited, setVisited] = useState<Set<Page>>(() => new Set([hashPage() ?? 'overview']));
  const positions = useRef<Partial<Record<Page, number>>>({});
  const currentPage = useRef(page);
  const locations = useRef<Partial<Record<Page, string>>>({ [page]: window.location.hash });
  useEffect(() => {
    let frame: number | undefined;
    function change() {
      if (frame !== undefined) cancelAnimationFrame(frame);
      const next = hashPage() ?? 'overview';
      locations.current[next] = window.location.hash;
      currentPage.current = next;
      setPage(next); setFocusId(hashFocus());
      setVisited((previous) => new Set([...previous, next]));
      frame = requestAnimationFrame(() => window.scrollTo({ top: window.history.state?.scroll ?? positions.current[next] ?? 0, behavior: 'auto' }));
    }
    function remember() {
      positions.current[currentPage.current] = window.scrollY;
      window.history.replaceState({ ...window.history.state, scroll: window.scrollY }, '');
    }
    for (const event of ['hashchange', 'popstate', routeEvent]) window.addEventListener(event, change);
    window.addEventListener('scroll', remember, { passive: true });
    return () => {
      for (const event of ['hashchange', 'popstate', routeEvent]) window.removeEventListener(event, change);
      window.removeEventListener('scroll', remember);
      if (frame !== undefined) cancelAnimationFrame(frame);
    };
  }, []);
  function navigate(next: Page, id?: string) {
    positions.current[page] = window.scrollY;
    window.history.replaceState({ ...window.history.state, scroll: window.scrollY }, '');
    const hash = !id && locations.current[next] ? locations.current[next]! : `#${next}${id ? `?id=${encodeURIComponent(id)}` : ''}`;
    if (window.location.hash !== hash) window.history.pushState({ scroll: positions.current[next] ?? 0 }, '', hash);
    // The dock can be clicked before the first passive route listener mounts.
    // Apply local navigation immediately; the event still restores URL fields
    // and scroll through the same shell path used by browser history.
    currentPage.current = next;
    locations.current[next] = hash;
    setPage(next); setFocusId(hashFocus());
    setVisited((previous) => new Set([...previous, next]));
    window.dispatchEvent(new Event(routeEvent));
  }
  const project = snapshot?.project;
  const runnerURL = localConsoleURL(connection.info?.runner_console_url);
  return <div className="workspace">
    <a href="#main-content" className="skip-link" onClick={(event) => { event.preventDefault(); document.getElementById('main-content')?.focus(); }}>跳转到主要内容</a>
    <div className="main-shell">
      <main id="main-content" tabIndex={-1}>
        <div className="workspace-status"><span>{error ? '中心连接异常' : loading ? '正在连接中心' : '中心已连接'} · {updatedAt ? time(updatedAt) : '等待快照'}</span><button className="text-button" onClick={() => { refresh(); connection.reload(); }} aria-label="刷新中心状态">刷新状态</button></div>
        {['requirements', 'tasks', 'candidates', 'blocks'].includes(page) && <nav className="pool-switch" aria-label="工作池切换">{poolPages.map((item) => <button key={item.id} aria-current={page === item.id ? 'page' : undefined} onClick={() => navigate(item.id)}>{item.label}</button>)}</nav>}
        {['settings', 'automation', 'connection', 'initialization', 'planning', 'audit', 'runs'].includes(page) && <nav className="settings-switch" aria-label="管理与设置">{managementPages.map((item) => <button key={item.id} aria-current={page === item.id ? 'page' : undefined} onClick={() => navigate(item.id)}>{item.label}</button>)}</nav>}
        <Feedback error={error ? `${error}${snapshot ? ' 当前显示上次成功取得的状态。' : ''}` : ''} />
        {page === 'connection' ? <ConnectionSettings client={client} {...connection} project={project} openPolicies={() => navigate('settings')} />
          : !snapshot ? <div className="panel connection-panel"><Empty title={loading ? '正在读取中心状态' : '尚未取得中心状态'}>{loading ? '仅显示服务端实际记录。' : '确认 owner token 与中心连接后重试。'}</Empty>{!loading && <div className="actions"><button className="primary" onClick={refresh}>重新连接</button><button className="secondary" onClick={logout}>更换凭据</button></div>}</div>
            : !project && !['agents', 'automation', 'planning'].includes(page) ? <ProjectSetup client={client} refresh={refresh} />
              : <div>
                {page === 'initialization' && <ProjectInitialization snapshot={snapshot} client={client} refresh={refresh} />}
                {page === 'planning' && <Planning snapshot={snapshot} client={client} refresh={refresh} initialId={focusId} />}
                {page === 'automation' && <Automation settings={snapshot.automation} client={client} refresh={refresh} />}
                {page === 'overview' && <Overview snapshot={snapshot} navigate={navigate} />}
                {visited.has('requirements') && <div hidden={page !== 'requirements'}><Requirements nodes={snapshot.nodes ?? []} requirements={snapshot.requirements ?? []} tasks={snapshot.tasks ?? []} navigate={navigate} client={client} refresh={refresh} /></div>}
                {visited.has('tasks') && <div hidden={page !== 'tasks'}><Tasks snapshot={snapshot} client={client} refresh={refresh} /></div>}
                {visited.has('blocks') && <div hidden={page !== 'blocks'}><Blocks snapshot={snapshot} client={client} refresh={refresh} navigate={navigate} /></div>}
                {page === 'agents' && <Agents snapshot={snapshot} client={client} refresh={refresh} navigate={navigate} />}
                {page === 'runs' && <Runs snapshot={snapshot} client={client} refresh={refresh} initialId={focusId} />}
                {visited.has('candidates') && <div hidden={page !== 'candidates'}><Candidates snapshot={snapshot} client={client} refresh={refresh} /></div>}
                {visited.has('testing') && <div hidden={page !== 'testing'}><Testing snapshot={snapshot} client={client} refresh={refresh} centerPlatform={['darwin', 'linux', 'windows'].includes(connection.info?.platform ?? '') ? connection.info?.platform as 'darwin' | 'linux' | 'windows' : undefined} /></div>}
                {page === 'audit' && <Audit events={snapshot.events ?? []} />}
                {page === 'settings' && project && <Settings project={project} client={client} refresh={refresh} />}
              </div>}
        <div className="workspace-account">{runnerURL && <a className="text-button" href={`${runnerURL}/`}>本机 Runner 默认入口 ↗</a>}<button className="text-button" onClick={logout}>断开工作台</button></div>
        <footer className="workbench-footer"><span>Coordica {SOURCE_VERSION}</span></footer>
      </main>
    </div>
    <nav className="workbench-dock" aria-label="工作台页面" onKeyDown={(event) => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
      const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('button')).filter((button) => button.getClientRects().length);
      const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
      const target = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length;
      buttons[target]?.focus(); event.preventDefault();
    }}>{dockPages.map((item, index) => <button key={item.id} className={poolPages.some((pool) => pool.id === item.id) ? `dock-item dock-pool ${item.id === 'tasks' ? 'dock-pool-entry' : ''}` : 'dock-item'} aria-label={compact && item.id === 'tasks' ? '工作池' : item.label} aria-current={page === item.id || item.id === 'tasks' && ['requirements', 'candidates', 'blocks'].includes(page) && compact || item.id === 'settings' && managementPages.some((entry) => entry.id === page) ? 'page' : undefined} onClick={() => navigate(item.id)}><DockIcon index={index} /><span className="dock-label">{item.short}</span>{item.id === 'tasks' && <span className="dock-mobile-label">工作池</span>}</button>)}</nav>
  </div>;
}
