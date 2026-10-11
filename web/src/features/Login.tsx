import { useState } from 'react';
import type { FormEvent } from 'react';
import { Field } from '../components/ui';

export const TOKEN_KEY = 'coordica.owner.session';
export function Login({ onConnect }: { onConnect: (token: string, remember: boolean) => void }) {
  const [token, setToken] = useState('');
  const [remember, setRemember] = useState(false);
  function submit(event: FormEvent) { event.preventDefault(); if (token.trim()) onConnect(token.trim(), remember); }
  return <main className="login-layout"><div className="login-intro"><span className="wordmark">Coordica<span className="wordmark-dot" /></span><div><span className="eyebrow">MULTI-AGENT WORKBENCH / PHASE A</span><h1>明确目标，<br />让协作有据可查。</h1><p>任务、运行与集成共享一套事实。<br />连接你的中心，开始受控协作。</p></div><small>Go center · Local network · Owner access</small></div><section className="login-form"><span className="eyebrow">连接中心</span><h2>进入工作台</h2><p className="muted">使用中心初始化时生成的 owner token。</p><form onSubmit={submit}><Field label="Owner token"><input type="password" autoComplete="off" autoFocus required value={token} onChange={(e) => setToken(e.target.value)} placeholder="输入访问凭据" /></Field><label className="checkbox"><input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />在当前标签页会话中记住凭据</label><button className="primary" disabled={!token.trim()}>连接工作台 <span aria-hidden="true">→</span></button></form><p className="fine-print">凭据只用于当前中心的 API 请求。记住凭据时存入 sessionStorage，关闭标签页后清除。</p></section></main>;
}
