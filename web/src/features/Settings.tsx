import { useState } from 'react';
import type { Project } from '../api.generated';
import { ApiClient } from '../api/client';
import { duration, Feedback, SectionTitle, time } from '../components/ui';
import { useAction } from '../hooks/useAction';

export function Settings({ project, client, refresh }: { project: Project; client: ApiClient; refresh: () => void }) {
  const action = useAction(refresh);
  const [backupPath, setBackupPath] = useState('');
  function toggle() {
    const command = project.paused ? 'resume' : 'pause';
    if (!project.paused && !window.confirm('暂停项目的新活动与候选放行，并请求在线 Runner 停止？已派发的远端操作仍需对账。')) return;
    void action.execute(() => client.write<Project>(`/project/${command}`, { expected_version: project.version }), project.paused ? '已请求恢复项目。' : '项目已暂停，实际进程状态请查看运行页。');
  }
  async function exportState() {
    await action.execute(async () => {
      const snapshot = await client.export();
      const url = URL.createObjectURL(new Blob([JSON.stringify(snapshot, null, 2)], { type: 'application/json;charset=utf-8' }));
      const anchor = document.createElement('a'); anchor.href = url; anchor.download = `coordica-export-${new Date().toISOString().slice(0, 10)}.json`; document.body.append(anchor); anchor.click(); anchor.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000);
    }, '状态导出已下载。导出不包含登录凭据。');
  }
  return <><SectionTitle title="项目设置" /><Feedback {...action} /><div className="settings-groups">
    <section aria-labelledby="project-control"><h2 id="project-control">项目控制</h2><div className="settings-group"><div className="settings-row"><div><strong>调度状态</strong><small>{project.paused ? '已暂停' : '允许调度'}</small></div><button className="secondary" disabled={action.busy} onClick={toggle}>{project.paused ? '核验并恢复项目' : '暂停项目'}</button></div><div className="settings-row"><span>累计活动</span><span>已使用 {duration(project.used_seconds)} · 预留 {duration(project.reserved_seconds)}</span></div><div className="settings-row"><span>目标分支</span><code>{project.target_branch}</code></div></div></section>
    <section aria-labelledby="project-backup"><h2 id="project-backup">备份与导出</h2><div className="settings-group"><div className="settings-row"><div><strong>一致备份</strong><small>保存在中心，包含完整恢复证据。</small></div><button className="secondary" disabled={action.busy} onClick={() => void action.execute(async () => { const result = await client.write<{ path: string }>('/backups', {}); setBackupPath(result.path); }, '一致备份已完成。')}>创建一致备份</button></div><div className="settings-row"><div><strong>状态导出</strong><small>下载 JSON，不包含登录凭据。</small></div><button className="secondary" disabled={action.busy} onClick={() => void exportState()}>导出状态 JSON</button></div>{backupPath && <p className="backup-result" role="status">中心备份位置 <code>{backupPath}</code></p>}</div></section>
    <section><h2>政策与恢复</h2><div className="settings-group"><div className="settings-row"><span>当前政策</span><span>v{project.policy_version}</span></div><details className="settings-advanced"><summary>技术详情：路径与检查</summary><dl className="facts"><dt>项目版本</dt><dd>v{project.version}</dd><dt>建立时间</dt><dd>{time(project.created_at)}</dd><dt>受保护路径</dt><dd className="code-list">{(project.protected_paths ?? []).map((path) => <code key={path}>{path}</code>)}</dd></dl><h3>最低检查</h3>{(project.checks ?? []).map((check, index) => <div key={index}><strong>{check.name}</strong><pre>{check.command} {(check.args ?? []).join(' ')}</pre><small>超时 {check.timeout_seconds} 秒</small></div>)}<p>恢复与运行版本升级由用户在中心实施；本页不会自动启用主线的新代码。</p></details></div></section>
  </div></>;
}
