import { Feedback, Field } from '../../components/ui';
import type { useLeaderConfiguration } from './useLeaderConfiguration';
import { leaderModelLabel } from './leaderLabels';

type LeaderControls = ReturnType<typeof useLeaderConfiguration>;

export function LeaderSelector({ leader, disabled }: { leader: LeaderControls; disabled: boolean }) {
  const { selection, capabilities, model } = leader;
  const tool = capabilities.find((cap) => cap.tool === selection?.tool);
  return <fieldset disabled={disabled || leader.loading} className="leader-selection">
    <legend>Leader</legend>
    <div className="leader-selection-fields">
      <Field label="Leader 工具">
        <select value={selection?.tool ?? ''} onChange={(event) => {
          const capability = capabilities.find((cap) => cap.tool === event.target.value);
          const first = capability?.models?.[0];
          if (capability && first) leader.setDraft({ tool: capability.tool, model: first.id });
        }}>
          {!tool && <option value={selection?.tool ?? ''}>{selection?.tool ? `${selection.tool}（当前不可用）` : '选择本机工具'}</option>}
          {capabilities.filter((cap) => cap.models?.length).map((cap) => <option key={cap.tool} value={cap.tool}>{cap.tool}</option>)}
        </select>
      </Field>
      <Field label="Leader 模型">
        <select value={selection?.model ?? ''} onChange={(event) => {
          if (selection) leader.setDraft({ tool: selection.tool, model: event.target.value });
        }}>
          {!model && <option value={selection?.model ?? ''}>{selection?.model ? leaderModelLabel(selection.model) : '尚未获取模型'}</option>}
          {tool?.models?.map((item) => <option key={item.id} value={item.id}>{leaderModelLabel(item.id, tool.models ?? [])}</option>)}
        </select>
      </Field>
      <Field label="Leader 思考强度">
        <select value={selection?.reasoning_effort ?? ''} onChange={(event) => {
          if (selection) leader.setDraft({ ...selection, reasoning_effort: event.target.value });
        }}>
          <option value="">工具默认</option>
          {model?.reasoning_efforts?.map((effort) => <option key={effort} value={effort}>{effort}</option>)}
        </select>
      </Field>
    </div>
    <Feedback error={leader.error} />
    <p className="muted" role="status">{leader.loading ? '正在探测中心主机…' :
      !leader.valid ? '所选工具或模型当前不可用。刷新能力或另选工具。' : '只显示中心主机实际报告的选项；更改只影响后续执行。'}</p>
    <button type="button" className="text-button" onClick={() => void leader.reload()}>刷新 Leader 能力</button>
  </fieldset>;
}
