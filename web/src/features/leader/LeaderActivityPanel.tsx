import { useState } from 'react';
import type { LeaderActivityEvent, LeaderExecution } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { Status, time } from '../../components/ui';
import { mergeActivity, useLeaderActivity, useLeaderHistory } from './useLeaderActivity';

function executionTitle(execution: LeaderExecution) {
  if (execution.source_kind === 'scope_request') return '范围申请评估';
  return execution.ref.kind === 'initialization' ? '项目初始化' : execution.purpose === 'consultation'
    ? '咨询与建议' : execution.source_id ? '需求变更评估' : '目标规划';
}

function defaultExecution(executions: LeaderExecution[], events: LeaderActivityEvent[]) {
  const priority = (v: LeaderExecution) => ['running', 'unknown_live', 'cancel_requested'].includes(v.state) ? 2 : v.state === 'queued' ? 1 : 0;
  const latest = (v: LeaderExecution) => events.reduce((seq, e) => e.execution.id === v.ref.id ? Math.max(seq, e.sequence) : seq, 0);
  return [...executions].sort((a, b) => priority(b) - priority(a) || latest(b) - latest(a)
    || Number(b.ref.kind === 'planning') - Number(a.ref.kind === 'planning'))[0];
}

export function LeaderActivityPanel({ client }: { client: ApiClient }) {
  const activity = useLeaderActivity(client);
  const [selected, setSelected] = useState('');
  const executions = activity.page?.executions ?? [];
  const current = executions.find((item) => item.ref.id === selected)
    ?? (!selected ? defaultExecution(executions, activity.events) : undefined);
  const history = useLeaderHistory(client, selected, current?.ref.kind ?? '');
  const records = history.history
    ? mergeActivity(activity.events.filter((event) => event.execution.id === selected), history.history) : activity.events;
  const events = records.filter((event) => event.execution.id === current?.ref.id);
  return (
    <section className="panel leader-activity" aria-labelledby="leader-activity-title">
      <header className="leader-section-header">
        <h2 id="leader-activity-title">实时活动</h2>
        <span className="muted" role="status">{activity.connected ? '已连接' : '未连接'}</span>
      </header>
      {activity.error && <p role="alert">{activity.error}
        <button className="text-button" onClick={activity.reconnect}>重新连接</button>
      </p>}
      <label className="leader-execution-picker">查看记录
        <select value={current?.ref.id ?? ''} onChange={(event) => setSelected(event.target.value)}>
          {!executions.length && <option value="">暂无执行记录</option>}
          {executions.map((item, index) => <option key={item.ref.id} value={item.ref.id}>
            {executionTitle(item)} · 第 {executions.length - index} 条
          </option>)}
        </select>
      </label>
      {current && <p><Status value={current.state} />
        <small> {current.tree_stopped ? '中心已确认停止' : '尚无停止确认'}</small>
      </p>}
      {history.error && <p role="alert">{history.error} 请重新选择记录以重试。</p>}
      <ol className="leader-timeline" aria-label="Leader 公开活动">
        {events.map((event) => <li key={event.sequence} className={`leader-event leader-event-${event.kind}`}>
          <time dateTime={event.time}>{time(event.time)}
            {event.execution.generation !== current?.ref.generation && ' · 先前尝试'}
          </time>
          <p className="preserve-lines">{event.text}</p>
        </li>)}
      </ol>
      {!events.length && <p className="muted">{current ? '此执行尚无公开活动记录。' : 'Leader 开始工作后，公开活动会显示在这里。'}</p>}
      {(activity.truncated || history.history?.truncated) && <small>仅保留最近的活动，早期记录已省略。</small>}
      <footer className="muted">公开消息与工具摘要；执行结果以中心状态为准。
        {activity.page && <small> 更新于 {time(activity.page.queried_at)}</small>}
      </footer>
    </section>
  );
}
