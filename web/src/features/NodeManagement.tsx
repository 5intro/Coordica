import { useState } from 'react';
import type { FormEvent } from 'react';
import type { Node } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, Field, time } from '../components/ui';
import { useAction } from '../hooks/useAction';

export function NodeManagement({ client, refresh }: { client: ApiClient; refresh: () => void }) {
  const action = useAction(refresh);
  const [open, setOpen] = useState(false);
  const [pairing, setPairing] = useState<{ code: string; expires_at: string }>();
  async function pair(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    await action.execute(async () => setPairing(await client.write<{ code: string; expires_at: string }>('/pairings', { name: String(data.get('name')).trim() })), '一次性配对码已生成。');
  }
  return <><button className="secondary" onClick={() => setOpen(!open)}>{open ? '收起配对' : '配对节点 ＋'}</button><Feedback {...action} />{open && <form className="panel pairing-form" onSubmit={pair}><Field label="节点名称"><input name="name" required placeholder="开发电脑名称" /></Field><button className="primary" disabled={action.busy}>生成配对码</button>{pairing && <div className="pairing-result"><span className="eyebrow">一次性凭据</span><code>{pairing.code}</code><p>有效至 {time(pairing.expires_at)}。在 Runner 配对时使用此码。</p><button type="button" className="secondary" onClick={() => void action.execute(() => navigator.clipboard.writeText(pairing.code), '配对码已复制。')}>复制配对码</button></div>}</form>}</>;
}

export function RevokeNode({ node, client, refresh }: { node: Node; client: ApiClient; refresh: () => void }) {
  const action = useAction(refresh);
  return <><Feedback {...action} />{!node.revoked && <button className="text-button danger" disabled={action.busy} onClick={() => { if (window.confirm(`撤销“${node.name}”的节点凭据？`)) void action.execute(() => client.write(`/nodes/${encodeURIComponent(node.id)}/revoke`, {}), '节点凭据已撤销。'); }}>撤销节点</button>}</>;
}
