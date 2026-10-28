import { useState } from 'react';
import type { FormEvent } from 'react';
import { MAX_RUN_DURATION_SECONDS, MAX_TASK_BUDGET_SECONDS } from '../api.generated';
import type { AutomationRequest, AutomationSettings, Node } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, Field, SectionTitle } from '../components/ui';
import { useAction } from '../hooks/useAction';
import { DevelopmentDefaults, developmentCapabilities } from './DevelopmentDefaults';
import { leaderModelLabel } from './initialization/leaderLabels';

export function Automation({ settings, nodes = [], client, refresh }: {
  settings: AutomationSettings; nodes?: Node[]; client: ApiClient; refresh: () => void;
}) {
  const [draft, setDraft] = useState(settings);
  const [error, setError] = useState('');
  const action = useAction(refresh);
  const changedDeveloper = draft.development_tool !== settings.development_tool ||
    draft.development_model !== settings.development_model;
  const dirty = changedDeveloper || draft.enabled !== settings.enabled ||
    draft.planning_timeout_seconds !== settings.planning_timeout_seconds ||
    draft.planning_budget_seconds !== settings.planning_budget_seconds ||
    draft.planning_max_retries !== settings.planning_max_retries ||
    draft.enforce_time_limits !== settings.enforce_time_limits ||
    draft.progress_check_seconds !== settings.progress_check_seconds;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (draft.version !== settings.version) { setError('系统版本已变化，请载入最新设置后重新核对。'); return; }
    if (!Number.isSafeInteger(draft.planning_timeout_seconds) || draft.planning_timeout_seconds <= 0 ||
      draft.planning_timeout_seconds > MAX_RUN_DURATION_SECONDS ||
      !Number.isSafeInteger(draft.planning_budget_seconds) || draft.planning_budget_seconds < 0 ||
      draft.planning_budget_seconds > MAX_TASK_BUDGET_SECONDS ||
      draft.enforce_time_limits && draft.planning_budget_seconds > 0 && draft.planning_budget_seconds < draft.planning_timeout_seconds ||
      !Number.isSafeInteger(draft.progress_check_seconds ?? 1800) ||
      (draft.progress_check_seconds ?? 1800) < 1 || (draft.progress_check_seconds ?? 1800) > 86400 ||
      !Number.isSafeInteger(draft.planning_max_retries) || draft.planning_max_retries < 0 || draft.planning_max_retries > 2) {
      setError(`规划超时必须为 1–${MAX_RUN_DURATION_SECONDS} 秒的整数；进度检查间隔必须为 1–86400 秒的整数；可选累计额度必须为 0– ${MAX_TASK_BUDGET_SECONDS} 秒的整数；重试次数必须为 0–2 的整数。`);
      return;
    }
    if (changedDeveloper && !developmentCapabilities(nodes).some((cap) => cap.tool === draft.development_tool &&
      cap.models?.some((model) => model.id === draft.development_model))) {
      setError('所选开发工具或模型已不在主机能力报告中，请刷新后核对。');
      return;
    }
    setError('');
    void action.execute(() => client.write('/system/automation', {
      expected_version: draft.version, settings: draft,
    } satisfies AutomationRequest), '系统设置保存请求已完成。');
  }

  return <div className="phase-b-page">
    <SectionTitle title="系统自动化" />
    <form className="panel settings-form" onSubmit={submit}>
      <Feedback {...action} error={error || action.error} />
      <button type="button" className="text-button" onClick={refresh}>刷新系统设置</button>
      <button type="button" className="text-button" onClick={() => { setDraft(settings); setError(''); }}>载入最新设置</button>
      <p role="status">{action.busy ? '正在保存系统设置…' : dirty ?
        '未保存：修改仅在当前表单，中心仍使用下列已生效设置。' : '当前显示已生效的中心设置。'}</p>
      <p>当前中心设置 v{settings.version} · {settings.enabled ? '开启' : '关闭'}</p>
      <Field label="自动化开关"><input type="checkbox" checked={draft.enabled}
        onChange={(event) => setDraft({ ...draft, enabled: event.target.checked })} /></Field>
      <details><summary>工具与模型</summary>
        <DevelopmentDefaults settings={draft} nodes={nodes} disabled={action.busy} onChange={setDraft} />
        <p>Leader 工具：{settings.leader_tool} · 模型：{leaderModelLabel(settings.leader_model)}</p>
        <a href="#initialization">在项目准备中选择 Leader</a>
        <p>各开发 Agent 的模型与思考强度在 Agent 管理页设置。</p>
      </details>
      <Field label="进度检查间隔（分钟）">
        <input type="number" min={1 / 60} max={1440} step="any"
          value={(draft.progress_check_seconds ?? 1800) / 60}
          onChange={(event) => setDraft({ ...draft, progress_check_seconds: Math.round(event.target.valueAsNumber * 60) })} />
      </Field>
      <p className="muted">检查已有活动；没有回复或暂无活动时待确认，不直接判为失败。</p>
      <Field label="启用累计额度与开发限时">
        <input type="checkbox" checked={draft.enforce_time_limits ?? false}
          onChange={(event) => setDraft({ ...draft, enforce_time_limits: event.target.checked })} />
      </Field>
      <p className="muted">默认仅统计用时。启用后，正数额度与开发限时用于后续新执行；0 表示不设累计额度。</p>
      {(['planning_timeout_seconds', 'planning_budget_seconds', 'planning_max_retries'] as const).map((key) =>
        <Field key={key} label={{ planning_timeout_seconds: '规划 / 初始化超时（秒）',
          planning_budget_seconds: '规划累计预算（秒）', planning_max_retries: '规划 / 初始化最大重试次数' }[key]}>
          <input type="number" min={key === 'planning_timeout_seconds' ? 1 : 0}
            max={key === 'planning_timeout_seconds' ? MAX_RUN_DURATION_SECONDS :
              key === 'planning_budget_seconds' ? MAX_TASK_BUDGET_SECONDS : 2}
            step="1" value={draft[key]} onChange={(event) => setDraft({ ...draft, [key]: event.target.valueAsNumber })} />
        </Field>)}
      <button className="primary" disabled={action.busy}>保存系统自动化</button>
    </form>
  </div>;
}
