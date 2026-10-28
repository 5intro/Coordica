import { useState } from 'react';
import type { Project, ExternalReviewPolicy } from '../api.generated';
import { ExternalReviewSettings } from './ExternalReviewSettings';
import { ApiClient } from '../api/client';
import { Feedback } from '../components/ui';
import { useAction } from '../hooks/useAction';

export function PublicationSettings({ project, client, refresh }: { project: Project; client: ApiClient; refresh: () => void }) {
  const action = useAction(refresh);
  const [mode, setMode] = useState(project.publication?.mode === 'github' ? 'github' : 'git');
  const [checks, setChecks] = useState((project.publication?.required_checks ?? []).join('\n'));
  const [version, setVersion] = useState(project.version);
  const [provider, setProvider] = useState<ExternalReviewPolicy | undefined>(project.publication?.review_providers?.[0]);
  const sourceInvalid = mode === 'github' && provider && (!Number.isSafeInteger(provider.actor_id) || provider.actor_id < 1 || !Number.isSafeInteger(provider.app_id) || provider.app_id < 1 || !(provider.kinds?.length));
  const stale = version !== project.version;
  return <section aria-labelledby="publication-settings"><h2 id="publication-settings">代码集成</h2><form className="settings-group" onSubmit={(event) => {
    event.preventDefault();
    if (sourceInvalid) return;
    void action.execute(async () => { const saved = await client.write<Project>('/project/publication', { expected_version: version, policy: { mode, required_checks: mode === 'github' ? checks.split('\n').map((value) => value.trim()).filter(Boolean) : [], ...(mode === 'github' && provider ? { review_providers: [provider] } : {}) } }); setVersion(saved.version); }, '发布方式已保存。');
  }}>
    <div className="settings-row publication-mode-row"><label htmlFor="publication-mode">发布方式</label><select id="publication-mode" value={mode} onChange={(event) => setMode(event.target.value)}><option value="git">本地 Git / Git 远端</option><option value="github">GitHub PR</option></select></div>
    {mode === 'github' && <div className="settings-advanced"><label htmlFor="publication-checks">必需的 GitHub CI 检查</label><textarea id="publication-checks" rows={3} required value={checks} onChange={(event) => setChecks(event.target.value)} placeholder="每行一项，使用 GitHub 中的完整检查名称" /><small>同一任务复用一个 PR。中心另上报 coordica/review，不增加一轮人工批准。</small><details><summary>连接与分支条件</summary><p>GitHub API 和 Git 凭据只配置在中心主机。目标分支须强制必需检查、保持最新并约束管理员，包含这里的 CI 和 coordica/review；允许 merge commit。平台不会自动修改 GitHub 权限。</p></details></div>}
    {!project.paused && <p className="muted">暂停项目后可修改。</p>}{stale && <p className="hold">项目版本已变化，请重新打开设置再核对。</p>}
    {mode === 'github' && <ExternalReviewSettings value={provider} change={setProvider} />}
    <div className="settings-row"><span>{project.publication?.mode === 'github' ? '当前：GitHub PR' : '当前：Git 发布'}</span><button className="secondary" type="submit" disabled={action.busy || !project.paused || stale || !!sourceInvalid}>保存发布方式</button></div><Feedback {...action} />
  </form></section>;
}
