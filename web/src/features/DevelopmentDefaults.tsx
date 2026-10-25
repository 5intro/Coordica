import type { AutomationSettings, Capability, Node } from '../api.generated';
import { Field } from '../components/ui';
import { leaderModelLabel } from './initialization/leaderLabels';

export function developmentCapabilities(nodes: Node[]): Capability[] {
  const tools = new Map<string, Capability>();
  for (const node of nodes) {
    if (node.revoked) continue;
    for (const cap of node.capabilities ?? []) {
      if (!cap.available || !cap.structured || !cap.cancel || cap.model_discovery !== 'reported') continue;
      const previous = tools.get(cap.tool);
      const models = new Map((previous?.models ?? []).map((model) => [model.id, model]));
      for (const model of cap.models ?? []) models.set(model.id, model);
      tools.set(cap.tool, { ...cap, models: [...models.values()] });
    }
  }
  return [...tools.values()];
}

export function DevelopmentDefaults({ settings, nodes, disabled, onChange }: {
  settings: AutomationSettings; nodes: Node[]; disabled: boolean;
  onChange: (settings: AutomationSettings) => void;
}) {
  const tools = developmentCapabilities(nodes);
  const tool = tools.find((cap) => cap.tool === settings.development_tool);
  const model = tool?.models?.find((item) => item.id === settings.development_model);
  return <fieldset disabled={disabled} className="development-defaults">
    <legend>默认开发 Agent</legend>
    <Field label="默认开发工具"><select value={settings.development_tool} onChange={(event) => {
      const selected = tools.find((cap) => cap.tool === event.target.value);
      if (selected?.models?.[0]) onChange({ ...settings, development_tool: selected.tool, development_model: selected.models[0].id });
    }}>
      {!tool && <option value={settings.development_tool}>{settings.development_tool}（当前未报告）</option>}
      {tools.filter((cap) => cap.models?.length).map((cap) => <option key={cap.tool} value={cap.tool}>{cap.tool}</option>)}
    </select></Field>
    <Field label="默认开发模型"><select value={settings.development_model}
      onChange={(event) => onChange({ ...settings, development_model: event.target.value })}>
      {!model && <option value={settings.development_model}>{leaderModelLabel(settings.development_model)}（当前未报告）</option>}
      {tool?.models?.map((item) => <option key={item.id} value={item.id}>{leaderModelLabel(item.id, tool.models ?? [])}</option>)}
    </select></Field>
    <p className="muted">来自已配对主机的能力报告。Agent 管理中的模型与强度设置优先，已派出运行保留原参数。</p>
  </fieldset>;
}
