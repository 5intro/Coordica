import { useState } from 'react';
import type { FormEvent } from 'react';
import { Field } from '../components/ui';

export const TOKEN_KEY = 'coordica.owner.session';
export function Login({ onConnect }: { onConnect: (token: string, remember: boolean) => void }) {
  const [token, setToken] = useState('');
  const [remember, setRemember] = useState(false);
  function submit(event: FormEvent) { event.preventDefault(); if (token.trim()) onConnect(token.trim(), remember); }
  return <main className="login-layout"><section className="login-form"><span className="wordmark">Coordica<span className="wordmark-dot" /></span><h1>连接中心</h1><form onSubmit={submit}><Field label="Owner token"><input type="password" autoComplete="off" autoFocus required value={token} onChange={(e) => setToken(e.target.value)} placeholder="输入访问凭据" /></Field><label className="checkbox"><input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />本次会话记住</label><small className="muted">关闭标签页清除</small><button className="primary" disabled={!token.trim()}>连接工作台</button></form><details><summary>如何获取凭据</summary><p>使用中心初始化时生成的 Owner token。凭据仅用于当前中心；选择记住时保存在当前标签页会话。</p></details></section></main>;
}
