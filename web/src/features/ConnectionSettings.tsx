import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import type { ConnectionInfo, Project } from '../api.generated';
import { ApiClient, messageOf } from '../api/client';
import { connectionInstruction, localConsoleURL, remoteRunnerURL } from '../api/connection';
import type { GeneratedConnection, PairingReceipt } from '../api/connection';
import { duration, Empty, Feedback, Field, SectionTitle, time } from '../components/ui';

interface Props {
  client: ApiClient;
  info?: ConnectionInfo;
  error: string;
  loading: boolean;
  reload: () => void;
  project?: Project | null;
  openPolicies: () => void;
}

export function ConnectionSettings({ client, info, error, loading, reload, project, openPolicies }: Props) {
  const [name, setName] = useState('');
  const [generated, setGenerated] = useState<GeneratedConnection>();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState('');
  const [notice, setNotice] = useState('');
  const [now, setNow] = useState(Date.now);
  const inFlight = useRef(false);
  const pendingPairing = useRef<{ name: string; key: string } | undefined>(undefined);
  const expired = generated ? new Date(generated.expiresAt).getTime() <= now : false;
  const remoteURL = info ? remoteRunnerURL(info.runner_url) : undefined;
  const runnerURL = localConsoleURL(info?.runner_console_url);

  useEffect(() => {
    if (!generated) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [generated]);

  async function copy(value: GeneratedConnection) {
    if (!navigator.clipboard?.writeText) throw new Error('浏览器无法自动复制，请在下面文本框中手动复制。');
    await navigator.clipboard.writeText(value.text);
    setNotice('接入配置已复制。发给另一台电脑的 Agent 即可开始初始化。');
  }

  async function generate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!info || !remoteURL || error || loading || !name.trim() || inFlight.current) return;
    inFlight.current = true;
    setBusy(true); setFailure(''); setNotice('');
    try {
      const nodeName = name.trim();
      if (pendingPairing.current?.name !== nodeName) pendingPairing.current = { name: nodeName, key: crypto.randomUUID() };
      const pairing = await client.write<PairingReceipt>('/pairings', { name: nodeName }, pendingPairing.current.key);
      const result = connectionInstruction(info, pairing, nodeName);
      pendingPairing.current = undefined;
      setGenerated(result);
      setNow(Date.now());
      if (new Date(result.expiresAt).getTime() <= Date.now()) setFailure('接入配置已到期，请重新生成。');
      else {
        try { await copy(result); }
        catch { setFailure('接入配置已生成，但未能自动复制。请重新复制，或手动复制下面的内容。'); }
      }
    } catch (cause) { setFailure(messageOf(cause)); }
    finally { inFlight.current = false; setBusy(false); }
  }

  async function copyExisting() {
    if (!generated || expired || inFlight.current) return;
    inFlight.current = true;
    setBusy(true); setFailure(''); setNotice('');
    try { await copy(generated); }
    catch (cause) { setFailure(messageOf(cause)); }
    finally { inFlight.current = false; setBusy(false); }
  }

  function downloadCertificate() {
    if (!info?.certificate_pem) return;
    const url = URL.createObjectURL(new Blob([info.certificate_pem], { type: 'application/x-pem-file' }));
    const anchor = document.createElement('a');
    anchor.href = url; anchor.download = 'coordica-center.crt'; document.body.append(anchor); anchor.click(); anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  return <>
    <SectionTitle eyebrow="08 / CENTER & CONNECTION" title="中心配置与接入" description="在中心电脑管理项目，把一次性接入配置交给其他电脑的 Agent。" actions={<button className="secondary" onClick={reload} disabled={loading}>刷新部署信息</button>} />
    <Feedback error={error} />
    {!info ? <section className="panel"><Empty title={loading ? '正在读取中心部署' : '尚未取得中心部署信息'}>中心部署信息需要当前 owner 凭据。</Empty></section> : <>
      <div className="settings-grid connection-settings">
        <section className="panel"><h2>当前中心部署</h2><dl className="facts">
          <dt>运行版本</dt><dd><code>{info.version}</code></dd>
          <dt>中心工作台</dt><dd><code>{info.console_url}</code></dd>
          <dt>控制台监听</dt><dd><code>{info.console_listen}</code></dd>
          <dt>Runner 入口</dt><dd><code>{info.runner_url || '未设置可分享地址'}</code></dd>
          <dt>Runner 监听</dt><dd><code>{info.runner_listen}</code></dd>
          <dt>中心数据目录</dt><dd><code>{info.data_dir}</code></dd>
        </dl><p className="muted">中心工作台仅在此电脑访问。其他电脑通过 HTTPS Runner 入口接入。</p>{runnerURL && <><a className="secondary local-workspace-link" href={`${runnerURL}/`}>本机 Runner 默认入口 ↗</a><p className="muted">这是本机页面入口，是否已启动请以 Runner 实际状态为准。</p></>}</section>
        <section className="panel"><h2>中心公共证书</h2>{info.certificate_pem ? <><p>公共 CA 可以交给其他电脑，用于验证中心的 HTTPS 连接。</p><dl className="facts"><dt>证书 SHA-256（DER）</dt><dd><code>{info.certificate_sha256}</code></dd></dl><button className="secondary" onClick={downloadCertificate}>下载公共 CA</button><details className="certificate-details"><summary>查看公共证书 PEM</summary><pre>{info.certificate_pem}</pre></details></> : <p className="muted">当前没有公共 CA。远端接入需要中心提供有效的 HTTPS 证书。</p>}</section>
        <section className="panel policy-panel"><h2>为另一台电脑生成接入配置</h2><p>让那台电脑的 Agent 安装 MCP、写入公共 CA，并初始化自己的后台 Runner。</p>{!remoteURL && <p className="feedback error">当前没有可供其他电脑访问的 HTTPS Runner 入口。请在中心启动配置中设置真实局域网地址；回环地址和通配监听地址不能分享。</p>}
          <Feedback error={failure} notice={notice} />
          <form className="pairing-form" onSubmit={generate}><Field label="接入节点名称"><input required value={name} onChange={(event) => setName(event.target.value)} placeholder="例如 Windows-dsh" maxLength={200} /></Field><button className="primary" disabled={busy || loading || !!error || !remoteURL || !name.trim()}>{generated ? '重新生成并复制接入配置' : '生成并复制接入配置'}</button></form>
          <p className="muted">每份配置仅供一个节点使用一次；到期后请重新生成。配对码只保存在当前页面，刷新或离开页面后清除。</p>
          {generated && <div className="connection-generated"><p><strong>{generated.name}</strong> · {expired ? '已到期，请重新生成' : `有效至 ${time(generated.expiresAt)}`}</p><Field label="一次性接入指令"><textarea readOnly rows={14} value={generated.text} spellCheck={false} /></Field><div className="actions"><button className="secondary" disabled={busy || expired} onClick={() => void copyExisting()}>复制已有接入配置</button><small>复制失败时可手动复制上面的完整指令，不必再生成配对码。</small></div></div>}
        </section>
        <section className="panel policy-panel"><div className="subheading"><h2>当前有效项目设置</h2>{project && <button className="text-button" onClick={openPolicies}>管理政策与恢复 →</button>}</div>{project ? <><dl className="facts"><dt>项目</dt><dd>{project.name}</dd><dt>目标分支</dt><dd><code>{project.target_branch}</code></dd><dt>有效政策</dt><dd>v{project.policy_version} · {project.paused ? '已暂停' : '允许调度'}</dd><dt>受保护路径</dt><dd className="code-list">{(project.protected_paths ?? []).map((path) => <code key={path}>{path}</code>)}{!(project.protected_paths ?? []).length && '未设置路径'}</dd></dl><p>累计活动 {duration(project.used_seconds)} · 当前预留 {duration(project.reserved_seconds)}（统计，无项目总额度）</p><h3>最低检查</h3><div className="policy-checks">{(project.checks ?? []).map((check, index) => <div key={`${check.name}-${index}`}><strong>{check.name}</strong><code>{check.command} {(check.args ?? []).join(' ')}</code><small>超时 {check.timeout_seconds} 秒</small></div>)}</div></> : <p className="muted">中心尚未创建项目。可以先配对节点，再从项目概览完成项目设置。</p>}</section>
      </div>
    </>}
  </>;
}
