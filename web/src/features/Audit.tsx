import { useState } from 'react';
import type { Event } from '../api.generated';
import { Empty, SectionTitle, short, time } from '../components/ui';

export function Audit({ events }: { events: Event[] }) {
  const [query, setQuery] = useState('');
  const filtered = [...events].sort((a, b) => b.sequence - a.sequence).filter((event) => `${event.kind} ${event.actor} ${event.detail} ${event.object_id}`.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
  return <><SectionTitle eyebrow="06 / AUDIT TRAIL" title="审计记录" description="谁依据哪个对象执行了什么动作。状态、审计与待发送消息一起持久化。" /><label className="search audit-search"><span className="sr-only">搜索审计</span><input type="search" placeholder="搜索动作、操作者或对象" value={query} onChange={(e) => setQuery(e.target.value)} /></label><div className="panel">{filtered.length ? <div className="table-scroll"><table className="audit-table"><thead><tr><th>序号 / 时间</th><th>动作</th><th>操作者</th><th>对象与依据</th></tr></thead><tbody>{filtered.map((event) => <tr key={event.sequence}><td><code>#{event.sequence}</code><small>{time(event.time)}</small></td><td><strong>{event.kind}</strong></td><td><code>{event.actor}</code></td><td><code title={event.object_id}>{short(event.object_id, 24)}</code><p className="preserve-lines">{event.detail}</p></td></tr>)}</tbody></table></div> : <Empty title="没有匹配的记录">中心尚无事件，或当前搜索没有结果。</Empty>}</div></>;
}
