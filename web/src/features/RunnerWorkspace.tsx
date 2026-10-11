import { useMemo, useState } from 'react';
import type { FormEvent } from 'react';
import type { Run } from '../api.generated';
import { LocalClient } from '../api/localClient';
import { localConsoleURL } from '../api/connection';
import { Empty, Feedback, Field, SectionTitle, Status, duration, short, time } from '../components/ui';
import { useAction } from '../hooks/useAction';
import { useSnapshot } from '../hooks/useSnapshot';

const activeStates = new Set(['starting', 'running', 'cancel_requested']);
const stages: Record<string, string> = { implement: '实现', review: '独立评审', repair: '修复' };

function stage(run: Run) { return stages[run.kind] ?? run.kind; }

export function RunnerWorkspace({ client: providedClient, centerConsoleURL }: { client?: LocalClient; centerConsoleURL?: string }) {
  const client = useMemo(() => providedClient ?? new LocalClient(), [providedClient]);
  const { snapshot, error, updatedAt, loading, refresh } = useSnapshot(client);
  const [page, setPage] = useState<'activity' | 'requirements'>('activity');
  const action = useAction(refresh);
  const node = snapshot?.node;
  const centerURL = localConsoleURL(centerConsoleURL);
  const runs = [...(snapshot?.runs ?? [])].sort((a, b) => b.started_at.localeCompare(a.started_at));
  const activeRuns = runs.filter((run) => activeStates.has(run.state) || run.state === 'lost' && !run.quiescent);
  const requirements = [...(snapshot?.requirements ?? [])].sort((a, b) => b.created_at.localeCompare(a.created_at));

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const proposal = { title: String(data.get('title')).trim(), body: String(data.get('body')).trim() };
    if (await action.execute(() => client.submitRequirement(proposal), '需求已提交到中心需求池，等待形成获批任务。')) form.reset();
  }

  return <div className="workspace runner-workspace">
    <a href="#main-content" className="skip-link">跳转到主要内容</a>
    <aside className="sidebar">
      <button className="wordmark brand-button" onClick={() => setPage('activity')}>Coordica<span className="wordmark-dot" /></button>
      <div className="project-label"><span className="project-monogram">{node?.name.slice(0, 1) || 'R'}</span><div><strong>{node?.name || '本机 Runner'}</strong><small>{snapshot?.project_name || '等待项目连接'}</small></div></div>
      <nav aria-label="本机工作台页面">
        <button className={page === 'activity' ? 'nav-item active' : 'nav-item'} aria-current={page === 'activity' ? 'page' : undefined} onClick={() => setPage('activity')}><span className="nav-number">01</span>本机活动{activeRuns.length > 0 && <span className="nav-count">{activeRuns.length}</span>}</button>
        <button className={page === 'requirements' ? 'nav-item active' : 'nav-item'} aria-current={page === 'requirements' ? 'page' : undefined} onClick={() => setPage('requirements')}><span className="nav-number">02</span>提交需求</button>
      </nav>
      {centerURL && <nav className="local-center-navigation" aria-label="同机中心工作台"><span className="eyebrow">同机中心 · 需 owner 登录</span><a className="nav-item" href={`${centerURL}/#tasks`}>中心任务池 ↗</a><a className="nav-item" href={`${centerURL}/#requirements`}>中心需求池 ↗</a><a className="nav-item" href={`${centerURL}/#blocks`}>中心阻塞管理 ↗</a><a className="nav-item" href={`${centerURL}/#agents`}>中心 Agents 管理 ↗</a><a className="nav-item" href={`${centerURL}/#runs`}>中心运行与节点 ↗</a><a className="nav-item" href={`${centerURL}/#connection`}>中心配置与接入 ↗</a></nav>}
      <div className="sidebar-bottom"><div className={`connection ${error ? 'offline' : loading ? 'loading' : 'online'}`}><span />{error ? '中心连接异常' : loading ? '正在读取本机状态' : '中心已连接'}</div><small>本机页面 · 自动执行</small></div>
    </aside>
    <div className="main-shell">
      <div className="topbar"><span>{page === 'activity' ? '本机活动' : '提交需求'}</span><div><span className="snapshot-time">{updatedAt ? `状态更新 ${time(updatedAt)}` : '等待中心记录'}</span><button className="text-button" onClick={refresh}>刷新 ↻</button></div></div>
      <main id="main-content" tabIndex={-1}>
        <Feedback error={error ? `${error}${snapshot ? ' 当前显示上次成功取得的状态。' : ''}` : ''} />
        {!snapshot ? <section className="panel connection-panel"><Empty title={loading ? '正在读取本机活动' : '尚未取得本机状态'}>{loading ? '读取中心保存的节点与执行记录。' : '确认 Runner 已配对并正在运行，然后重试。'}</Empty>{!loading && <div className="actions"><button className="primary" onClick={refresh}>重新连接</button></div>}</section> : <>
          {snapshot.project_paused && <p className="hold">中心项目已暂停，新的执行等待中心恢复。</p>}
          {page === 'activity' ? <>
            <SectionTitle eyebrow="01 / LOCAL ACTIVITY" title="本机 Agent 在做什么" description="展示这台电脑的实际执行记录，每 3 秒更新。" />
            <section className="panel runner-node-summary">
              <div className="subheading"><h2>{node?.name}</h2><span className={`status ${node?.revoked ? 'red' : 'neutral'}`}>{node?.revoked ? '已撤销' : node?.platform}</span></div>
              <p className="muted">并发容量 {node?.capacity} · 最后上报 {time(node?.last_seen)} · {error ? '显示上次上报状态' : activeRuns.length ? `${activeRuns.length} 项执行或待确认活动` : '当前空闲，等待自动领取任务'}</p>
              <div className="capabilities">{(node?.capabilities ?? []).map((capability) => {
                const activities = activeRuns.filter((run) => run.tool === capability.tool);
                const activityLabel = activities.some((run) => run.state === 'lost') ? '失联 · 未确认停止' : activities.some((run) => run.state === 'cancel_requested') ? '正在停止' : activities.length ? `${activities.map(stage).join('、')}中` : capability.available ? '可用 · 空闲' : '不可用';
                return <div key={capability.tool}><strong>{capability.tool}</strong><code>{capability.version || '版本未知'}</code><span className={activities.some((run) => run.state === 'lost') ? 'danger' : capability.available ? 'available' : 'muted'}>{activityLabel}</span><small>{capability.protocol || '协议未报告'} · {capability.structured && capability.cancel ? '可受管执行' : '执行能力不足'} · {capability.review ? '支持独立评审' : '未声明评审'}</small>{capability.detail && <small>{capability.detail}</small>}</div>;
              })}</div>
              {!(node?.capabilities ?? []).length && <p className="muted">尚未报告 Agent 工具能力。</p>}
            </section>
            <section className="panel run-history"><div className="subheading"><h2>执行活动</h2><span className="count">{runs.length} 个 Run</span></div>
              {runs.length ? <div className="table-scroll"><table><thead><tr><th>任务 / Agent</th><th>阶段</th><th>状态</th><th>用量 / 授予</th><th>租约截止</th><th>进程</th></tr></thead><tbody>{runs.map((run) => <tr key={run.id}><td><strong>{(snapshot.tasks ?? []).find((task) => task.id === run.task_id)?.title ?? run.task_id}</strong><small>{run.tool} · <code>{short(run.id)}</code></small><details className="run-detail"><summary>执行详情</summary><p>{run.detail || '中心尚无补充记录。'}</p><code>B {run.base || '未记录'}<br />H {run.head || '未记录'}</code><p>开始 {time(run.started_at)}{run.finished_at ? ` · 结束 ${time(run.finished_at)}` : ''}</p></details></td><td>{stage(run)}</td><td><Status value={run.state} /></td><td>{duration(run.elapsed_seconds)}<small>/ {duration(run.grant_seconds)}</small></td><td>{time(run.lease_until)}</td><td><span className={`status ${run.quiescent ? 'green' : 'neutral'}`}>{run.quiescent ? '已确认静止' : '未确认静止'}</span></td></tr>)}</tbody></table></div> : <Empty title="本机尚无实际运行">Runner 会自动领取符合本机能力的获批任务。</Empty>}
            </section>
          </> : <>
            <SectionTitle eyebrow="02 / REQUIREMENT INTAKE" title="向中心提交需求" description="记录要解决的问题和成功条件，提交后可在中心需求池查看。" />
            <Feedback {...action} />
            <form className="panel form-stack runner-requirement-form" onSubmit={submit}><Field label="需求标题"><input name="title" required maxLength={200} /></Field><Field label="原始需求与成功条件"><textarea name="body" required rows={6} /></Field><div className="form-footer"><small>提交会保留本机来源。需求记录完成后，中心仍按授权、预算和门禁规则推进开发。</small><button className="primary" disabled={action.busy || !snapshot.project_name}>提交到中心</button></div>{!snapshot.project_name && <p className="muted">中心尚未创建项目，请先在中心电脑完成项目设置。</p>}</form>
            <section className="runner-submissions"><div className="subheading"><h2>本机提交记录</h2><span className="count">{requirements.length} 项</span></div>{requirements.length ? <div className="requirement-grid">{requirements.map((requirement) => <article className="panel" key={requirement.id}><div className="record-meta"><code>{requirement.id}</code><span>已记录 · v{requirement.version}</span></div><h2>{requirement.title}</h2><p className="preserve-lines">{requirement.body}</p><small className="muted">提交于 {time(requirement.created_at)}</small></article>)}</div> : <div className="panel"><Empty title="尚未提交需求">本机提交的需求及中心回执会保存在这里。</Empty></div>}</section>
          </>}
        </>}
        <footer className="workbench-footer"><span>COORDICA / LOCAL RUNNER</span><span>本机活动 · 自动领取 · 需求提交</span></footer>
      </main>
    </div>
  </div>;
}
