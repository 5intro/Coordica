import { SOURCE_VERSION } from './api.generated';
import { useEffect, useMemo, useState } from 'react';
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
import { Planning } from './features/leader/LeaderWorkspace';
import { Automation } from './features/Automation';
import { Overview } from './features/Overview';
import { Requirements } from './features/Requirements';
import { Tasks } from './features/Tasks';
import { Agents } from './features/Agents';
import { Blocks } from './features/Blocks';
import { useWorkspaceRoute } from './hooks/useWorkspaceRoute';
import { Testing } from './features/testing/TestingWorkspace';
import { Runs } from './features/Runs';
import { Candidates } from './features/Candidates';
import { Audit } from './features/Audit';
import { Settings } from './features/Settings';
import { ConnectionSettings } from './features/ConnectionSettings';
import { RunnerWorkspace } from './features/RunnerWorkspace';
import { SettingsNavigation, WorkspaceDock } from './features/WorkspaceNavigation';
import { DetailPanel } from './components/material/DetailPanel';
import { Icon } from './components/material/icons';
import { RequirementComposer } from './features/RequirementComposer';
import { WorkspaceSearch } from './features/WorkspaceSearch';

export type Page = 'initialization' | 'planning' | 'automation' | 'blocks' | 'agents' | 'overview' | 'requirements' | 'tasks' | 'runs' | 'candidates' | 'audit' | 'settings' | 'connection' | 'testing';
const pages: { id: Page; label: string; number: string }[] = [
  { id: 'testing', label: '测试中心', number: '' },
  { id: 'initialization', label: '项目准备', number: '00' },
  { id: 'planning', label: 'Leader 对话追踪', number: 'B1' }, { id: 'automation', label: '系统自动化', number: 'B2' },
  { id: 'overview', label: '项目概览', number: '01' }, { id: 'requirements', label: '需求池', number: '02' },
  { id: 'tasks', label: '任务池', number: '03' }, { id: 'runs', label: '运行与节点', number: '04' },
  { id: 'blocks', label: '阻断池', number: '09' }, { id: 'agents', label: 'Agent 管理', number: '10' },
  { id: 'candidates', label: '集成候选', number: '05' }, { id: 'audit', label: '审计记录', number: '06' },
  { id: 'settings', label: '政策与恢复', number: '07' }, { id: 'connection', label: '中心配置与接入', number: '08' },
];
function storedToken() { try { return sessionStorage.getItem(TOKEN_KEY) ?? ''; } catch { return ''; } }


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
  const { page, focusId, visited, navigate } = useWorkspaceRoute(pages);
  const [composer, setComposer] = useState(false);
  const [requirementNotice, setRequirementNotice] = useState('');
  const [searching, setSearching] = useState(false);
  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => { if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); setSearching((previous) => !previous); } };
    window.addEventListener('keydown',shortcut); return () => window.removeEventListener('keydown',shortcut);
  }, []);
  const project = snapshot?.project;
  const runnerURL = localConsoleURL(connection.info?.runner_console_url);
  return <div className="workspace">
    <a href="#main-content" className="skip-link" onClick={(event) => { event.preventDefault(); document.getElementById('main-content')?.focus(); }}>跳转到主要内容</a>
    <div className="workspace-ambient" aria-hidden="true"/>
    <header className="workspace-topbar"><button className="workspace-brand" aria-label="Coordica 项目概览" onClick={() => navigate('overview')}><span className="wordmark">Coordica<span className="wordmark-dot"/></span></button><div className="workspace-top-actions"><button className="workspace-search" aria-label="搜索工作台" onClick={() => setSearching(true)}><Icon name="search" size={17}/><kbd>⌘ K</kbd></button></div></header>
    <div className="main-shell">
      <main id="main-content" tabIndex={-1}>
        {(error || loading) && <div className="workspace-status"><span>{error ? '中心连接异常' : loading ? '正在连接中心' : '中心已连接'} · {updatedAt ? time(updatedAt) : '等待快照'}</span></div>}
        <SettingsNavigation page={page} navigate={navigate} />
        <Feedback notice={requirementNotice} error={error ? `${error}${snapshot ? ' 当前显示上次成功取得的状态。' : ''}` : ''} />
        {page === 'connection' ? <ConnectionSettings client={client} {...connection} project={project} openPolicies={() => navigate('settings')} />
          : !snapshot ? <div className="panel connection-panel"><Empty title={loading ? '正在读取中心状态' : '尚未取得中心状态'}>{loading ? '仅显示服务端实际记录。' : '确认 owner token 与中心连接后重试。'}</Empty>{!loading && <div className="actions"><button className="primary" onClick={refresh}>重新连接</button><button className="secondary" onClick={logout}>更换凭据</button></div>}</div>
            : !project && !['agents', 'automation', 'planning'].includes(page) ? <ProjectSetup client={client} refresh={refresh} />
              : <div className={`feature-page feature-${page}`}>
                {page === 'initialization' && <ProjectInitialization snapshot={snapshot} client={client} refresh={refresh} />}
                {page === 'planning' && <Planning snapshot={snapshot} client={client} refresh={refresh} initialId={focusId} />}
                {page === 'automation' && <Automation settings={snapshot.automation} client={client} refresh={refresh} />}
                {page === 'overview' && <Overview snapshot={snapshot} navigate={navigate} onNewRequirement={() => { setRequirementNotice(''); setComposer(true); }} />}
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
        <footer className="workbench-footer"><span>Coordica {SOURCE_VERSION}</span>{!error && !loading && <span>中心已连接 · {updatedAt ? time(updatedAt) : '等待快照'}</span>}<button className="text-button" onClick={() => { refresh(); connection.reload(); }} aria-label="刷新中心状态">刷新状态</button>{runnerURL && <a className="text-button" href={`${runnerURL}/`}>本机 Runner ↗</a>}<button className="text-button" onClick={logout}>断开工作台</button></footer>
      </main>
    </div>
    <WorkspaceDock page={page} navigate={navigate} />
    {composer && <DetailPanel title="新需求" onClose={() => setComposer(false)}><h2>新需求</h2><RequirementComposer client={client} refresh={refresh} onSaved={() => { setComposer(false); setRequirementNotice('需求已记录。'); }}/></DetailPanel>}
    {searching && snapshot && <WorkspaceSearch snapshot={snapshot} onClose={() => setSearching(false)} navigate={(page,id) => { setSearching(false); navigate(page,id); }}/>}
  </div>;
}
