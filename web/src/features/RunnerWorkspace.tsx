import { useMemo, useState } from 'react';
import type { FormEvent } from 'react';
import type { Run } from '../api.generated';
import { LocalClient } from '../api/localClient';
import { localConsoleURL } from '../api/connection';
import { Empty, Feedback, Field, SectionTitle, Status, duration, time } from '../components/ui';
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
    <div className="main-shell">
      <main id="main-content" tabIndex={-1}>
        <header className="runner-heading">
          <div><h1>{node?.name || '本机 Runner'}</h1><p className="muted">{snapshot?.project_name || '等待项目连接'}</p></div>
          <div className="runner-status">
            <span className={`connection ${error ? 'offline' : loading ? 'loading' : 'online'}`}><span />{error ? '中心连接异常' : loading ? '正在读取本机状态' : '中心已连接'}</span>
            <span className="snapshot-time">{updatedAt ? `状态更新 ${time(updatedAt)}` : '等待中心记录'}</span>
            <button className="secondary" onClick={refresh}>刷新 ↻</button>
          </div>
        </header>
        <nav className="segmented" aria-label="本机工作台页面">
          <button aria-pressed={page === 'activity'} aria-current={page === 'activity' ? 'page' : undefined} onClick={() => setPage('activity')}>本机活动{activeRuns.length > 0 && <span> · {activeRuns.length}</span>}</button>
          <button aria-pressed={page === 'requirements'} aria-current={page === 'requirements' ? 'page' : undefined} onClick={() => setPage('requirements')}>提交需求</button>
        </nav>
        {centerURL && <details className="runner-center-access"><summary>同机中心 · 需 owner 登录</summary><nav className="runner-center-links" aria-label="同机中心工作台"><a href={`${centerURL}/#tasks`}>中心任务池 ↗</a><a href={`${centerURL}/#requirements`}>中心需求池 ↗</a><a href={`${centerURL}/#blocks`}>中心阻断管理 ↗</a><a href={`${centerURL}/#agents`}>中心 Agents 管理 ↗</a><a href={`${centerURL}/#runs`}>中心运行与节点 ↗</a><a href={`${centerURL}/#connection`}>中心配置与接入 ↗</a></nav></details>}
        <Feedback error={error ? `${error}${snapshot ? ' 当前显示上次成功取得的状态。' : ''}` : ''} />
        {!snapshot ? <section className="panel connection-panel"><Empty title={loading ? '正在读取本机活动' : '尚未取得本机状态'}>{loading ? '读取中心保存的节点与执行记录。' : '确认 Runner 已配对并正在运行，然后重试。'}</Empty>{!loading && <div className="actions"><button className="primary" onClick={refresh}>重新连接</button></div>}</section> : <>
          {snapshot.project_paused && <p className="hold">中心项目已暂停，新的执行等待中心恢复。</p>}
          {page === 'activity' ? <>
            <SectionTitle title="本机 Agent 在做什么" />
            <section className="panel runner-node-summary">
              <div className="subheading"><h2>{node?.name}</h2><span className={`status ${node?.revoked ? 'red' : 'neutral'}`}>{node?.revoked ? '已撤销' : node?.platform}</span></div>
              <p className="muted">并发容量 {node?.capacity} · 最后上报 {time(node?.last_seen)} · {error ? '显示上次上报状态' : activeRuns.length ? `${activeRuns.length} 项执行或待确认活动` : '当前空闲，等待自动领取任务'}</p>
              <details>
                <summary>工具能力与诊断 · {(node?.capabilities ?? []).filter((capability) => capability.available).length} 项可用</summary>
                <div className="capabilities">{(node?.capabilities ?? []).map((capability) => <div key={capability.tool}>
                  <strong>{capability.tool}</strong>
                  <span className={capability.available ? 'available' : 'muted'}>{capability.available ? '探测可用' : '探测不可用'}</span>
                  {capability.version && <code>{capability.version}</code>}
                  {capability.protocol && <small>协议：{capability.protocol}</small>}
                  <small>结构化输出：{capability.structured ? '支持' : '未声明'} · 取消：{capability.cancel ? '支持' : '未声明'} · 独立评审：{capability.review ? '支持' : '未声明'}</small>
                  <small>只读：{capability.read_only ? '支持' : '未声明'} · 恢复：{capability.resume ? '支持' : '未声明'}</small>
                  {capability.detail && <small>{capability.detail}</small>}
                </div>)}</div>
                {!(node?.capabilities ?? []).length && <p className="muted">尚未报告 Agent 工具能力。</p>}
              </details>
            </section>
            <section className="panel run-history"><div className="subheading"><h2>执行活动</h2><span className="count">{runs.length} 个 Run</span></div>
              {runs.length ? <div className="table-scroll"><table><thead><tr><th>任务 / Agent</th><th>阶段</th><th>状态</th><th>用量 / 授予</th><th>租约截止</th><th>进程</th></tr></thead><tbody>{runs.map((run) => <tr key={run.id}><td><strong>{(snapshot.tasks ?? []).find((task) => task.id === run.task_id)?.title ?? '未命名任务'}</strong><small>{run.tool}</small><details className="run-detail"><summary>技术详情</summary><code>{run.id}</code><p>{run.detail || '中心尚无补充记录。'}</p><code>B {run.base || '未记录'}<br />H {run.head || '未记录'}</code><p>开始 {time(run.started_at)}{run.finished_at ? ` · 结束 ${time(run.finished_at)}` : ''}</p></details></td><td>{stage(run)}{(run.state === 'starting' || run.state === 'running') && '中'}</td><td><Status value={run.state} />{run.state === 'lost' && !run.quiescent && <small className="danger">失联 · 未确认停止</small>}</td><td>{duration(run.elapsed_seconds)}<small>/ {duration(run.grant_seconds)}</small></td><td>{time(run.lease_until)}</td><td><span className={`status ${run.quiescent ? 'green' : 'neutral'}`}>{run.quiescent ? '已确认静止' : '未确认静止'}</span></td></tr>)}</tbody></table></div> : <Empty title="本机尚无实际运行">Runner 会自动领取符合本机能力的获批任务。</Empty>}
            </section>
          </> : <>
            <SectionTitle title="向中心提交需求" />
            <Feedback {...action} />
            <form className="panel form-stack runner-requirement-form" onSubmit={submit}><Field label="需求标题"><input name="title" required maxLength={200} /></Field><Field label="原始需求与成功条件"><textarea name="body" required rows={6} /></Field><div className="form-footer"><small>提交会保留本机来源。需求记录完成后，中心仍按授权、预算和门禁规则推进开发。</small><button className="primary" disabled={action.busy || !snapshot.project_name}>提交到中心</button></div>{!snapshot.project_name && <p className="muted">中心尚未创建项目，请先在中心电脑完成项目设置。</p>}</form>
            <section className="runner-submissions"><div className="subheading"><h2>本机提交记录</h2><span className="count">{requirements.length} 项</span></div>{requirements.length ? <div className="requirement-grid">{requirements.map((requirement) => <article className="panel" key={requirement.id}><div className="record-meta"><details><summary>技术详情</summary><code>{requirement.id}</code></details><span>已记录 · v{requirement.version}</span></div><h2>{requirement.title}</h2><p className="preserve-lines">{requirement.body}</p><small className="muted">提交于 {time(requirement.created_at)}</small></article>)}</div> : <div className="panel"><Empty title="尚未提交需求">本机提交的需求及中心回执会保存在这里。</Empty></div>}</section>
          </>}
        </>}
        <footer className="workbench-footer"><span>Coordica · 本机Runner</span></footer>
      </main>
    </div>
  </div>;
}
