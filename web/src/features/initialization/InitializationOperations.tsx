import { useState } from 'react';
import type { Initialization } from '../../api.generated';
import { Feedback, Field } from '../../components/ui';
import type { PreparationControls, PreparationState } from './types';

const labels: Record<string, string> = {
  draft: '尚未准备', analyzing: 'Leader 正在分析', interview: '有问题待回答',
  awaiting_owner: '项目约定待确认', initialized: '项目约定已确认',
};
const guidance: Record<string, string> = {
  draft: '暂停项目并结束当前执行后，开始分析或访谈。',
  analyzing: '分析结果将保存在这里，关闭页面不影响执行。',
  interview: '回答下方问题，也可在已连接的中心交互层继续同一轮访谈。',
  awaiting_owner: '审阅草案与原始回答，再确认项目约定。',
  initialized: '这里保留已确认约定。开发目标需另行授权，当前调度设置不变。',
};

export function InitializationOperations({ project, current, client, action }: PreparationState & PreparationControls) {
  const [kind, setKind] = useState(current?.kind === 'new' ? 'new' : 'existing');
  const analyzing = current?.status === 'analyzing';
  const unknown = current?.operation_state === 'unknown_live';
  return (
    <section className="panel" aria-labelledby="initialization-status">
      <h2 id="initialization-status">{labels[current?.status ?? 'draft']}</h2>
      <p role="status">{guidance[current?.status ?? 'draft']}</p>
      {!project.checks?.length && <p role="status">当前尚无有效检查：可以先访谈，确认项目约定前请配置真实检查。</p>}
      {current?.detail && <p className="muted">{current.detail}</p>}
      <details open={!current?.kind}>
        <summary>{current?.status === 'initialized' ? '重新分析项目' : '分析选项'}</summary>
        <Field label="项目输入类型" hint="已有仓库分析固定提交；新项目通过访谈补齐需求与架构。">
          <select value={kind} onChange={(event) => setKind(event.target.value)}>
            <option value="existing">已有仓库 · 固定提交只读分析</option>
            <option value="new">新项目 · 需求与架构访谈</option>
          </select>
        </Field>
      </details>
      <button className="primary" disabled={action.busy || !project.paused || analyzing || unknown}
        onClick={() => void action.execute(() => client.write<Initialization>('/initialization/analyze', {
          expected_version: current?.version ?? 0, kind,
        }), '只读分析请求已登记。未授权任何开发任务。')}>
        {current?.status === 'initialized' ? '暂停后重新初始化（使旧任务授权失效）' : '打开项目并请求只读初始化'}
      </button>
      {analyzing && !unknown && (
        <button className="secondary" disabled={action.busy} onClick={() => void action.execute(
          () => client.write('/initialization/cancel', { expected_version: current.version }),
          '已请求取消；实际停止仍需进程回执。',
        )}>取消当前分析</button>
      )}
      {unknown && current && <StopEvidenceForm key={current.operation_id} current={current} client={client} action={action} />}
    </section>
  );
}

function StopEvidenceForm({ current, client, action }: PreparationControls & { current: Initialization }) {
  const [evidence, setEvidence] = useState('');
  const [version, setVersion] = useState(current.operation_version);
  const [error, setError] = useState('');
  return (
    <form onSubmit={(event) => {
      event.preventDefault();
      if (version !== current.operation_version) {
        setError('分析执行已变化，请重新核验当前进程并填写记录。'); return;
      }
      void action.execute(() => client.write('/initialization/confirm-stopped', {
        expected_version: version, evidence,
      }), '停止核验已登记，需重新分析。');
    }}>
      <Field label="实际停止检查记录" hint="请在原主机检查分析进程及子进程；超时不能作为停止证据。">
        <textarea required value={evidence} onChange={(event) => {
          if (!evidence) setVersion(current.operation_version);
          setEvidence(event.target.value);
        }} />
      </Field>
      <Feedback error={error} />
      <button className="secondary" disabled={action.busy || !evidence.trim()}>登记 Owner 停止核验</button>
    </form>
  );
}
