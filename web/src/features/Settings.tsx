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
  return <><SectionTitle eyebrow="07 / PROJECT CONTROL" title="政策与恢复" description="代码合并不等于控制版本启用。暂停、备份与授权各自留痕。" /><Feedback {...action} /><div className="settings-grid"><section className="panel"><div className="subheading"><h2>项目控制</h2><span className={`status ${project.paused ? 'neutral' : 'green'}`}>{project.paused ? '已暂停' : '允许调度'}</span></div><p>暂停阻止新派单和未派发的候选放行。已进入 dispatching 的外部动作按可能已发送处理。</p><button className={project.paused ? 'primary' : 'secondary'} disabled={action.busy} onClick={toggle}>{project.paused ? '核验并恢复项目' : '暂停项目'}</button><div className="detail-section"><h3>项目累计活动统计</h3><p>已使用 {duration(project.used_seconds)} · 当前预留 {duration(project.reserved_seconds)}</p><p className="muted">中心持续运行，无项目生命周期总额度。目标、任务和每次 Run 的有限限制分别执行；历史活动记录保留。</p></div></section><section className="panel"><h2>备份与导出</h2><p>一致备份保存在中心。状态导出用于查看与接手，不能替代代码对象和完整证据备份。</p><div className="actions"><button className="primary" disabled={action.busy} onClick={() => void action.execute(async () => { const result = await client.write<{ path: string }>('/backups', {}); setBackupPath(result.path); }, '一致备份已完成。')}>创建一致备份</button><button className="secondary" disabled={action.busy} onClick={() => void exportState()}>导出状态 JSON</button></div>{backupPath && <p className="backup-result"><span className="eyebrow">中心备份位置</span><code>{backupPath}</code></p>}<p className="fine-print">恢复与运行版本升级由用户在中心实施；本页不会自动启用 main 的新代码。</p></section><section className="panel policy-panel"><div className="subheading"><h2>当前有效政策</h2><code>v{project.policy_version}</code></div><dl className="facts"><dt>项目版本</dt><dd>v{project.version}</dd><dt>目标分支</dt><dd><code>{project.target_branch}</code></dd><dt>建立时间</dt><dd>{time(project.created_at)}</dd><dt>受保护路径</dt><dd className="code-list">{(project.protected_paths ?? []).map((path) => <code key={path}>{path}</code>)}{!(project.protected_paths ?? []).length && '未设置路径'}</dd></dl><h3>最低检查</h3><div className="policy-checks">{(project.checks ?? []).map((check, index) => <div key={`${check.name}-${index}`}><strong>{check.name}</strong><code>{check.command} {(check.args ?? []).join(' ')}</code><small>超时 {check.timeout_seconds} 秒</small></div>)}</div></section></div></>;
}
