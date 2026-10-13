import { useState } from 'react';
import type { FormEvent } from 'react';
import type { Check, Project } from '../api.generated';
import { ApiClient } from '../api/client';
import { Feedback, Field, SectionTitle, lines } from '../components/ui';
import { useAction } from '../hooks/useAction';

type CheckDraft = { key: number; name: string; command: string; args: string; timeout: string };
const firstCheck: CheckDraft = { key: 0, name: 'Go 测试', command: 'go', args: 'test\n./...', timeout: '900' };

export function ProjectSetup({ client, refresh }: { client: ApiClient; refresh: () => void }) {
  const [checks, setChecks] = useState<CheckDraft[]>([firstCheck]);
  const [draftOnly, setDraftOnly] = useState(false);
  const [nextKey, setNextKey] = useState(1);
  const action = useAction(refresh);
  function change(key: number, field: keyof Omit<CheckDraft, 'key'>, value: string) {
    setChecks((previous) => previous.map((check) => check.key === key ? { ...check, [field]: value } : check));
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const body: Pick<Project, 'name' | 'repository_url' | 'target_branch' | 'protected_paths' | 'checks' | 'source_repository'> = {
      name: String(data.get('name')).trim(), repository_url: String(data.get('repository')).trim(),
      target_branch: String(data.get('branch')).trim(), source_repository: String(data.get('source_repository') ?? '').trim(),
      protected_paths: lines(String(data.get('protected'))),
      checks: draftOnly ? [] : checks.map((check): Check => ({ name: check.name.trim(), command: check.command.trim(), args: lines(check.args), timeout_seconds: Number(check.timeout) })),
    };
    await action.execute(() => client.write<Project>(draftOnly ? '/project/open' : '/project', body), '项目已登记并暂停；需完成共享基线初始化。');
  }
  return <><SectionTitle eyebrow="00 / INITIAL SETUP" title="建立第一个项目" description="确认仓库与最低门禁。此后每个候选按已批准政策验证。" /><form className="panel setup-form" onSubmit={submit}><div className="form-grid"><Field label="项目名称"><input name="name" defaultValue="Coordica" required /></Field><Field label="仓库地址" hint="公开仓库 HTTPS 地址，或中心可访问的本机绝对路径。"><input name="repository" required placeholder="https://github.com/你的账号/Coordica.git" /></Field><Field label="固定本地源码输入" hint="目标为 HTTPS 时，填写中心已有本地 clone 的绝对路径；普通聊天不能选择或读取其它目录。"><input name="source_repository" placeholder="/projects/Coordica" /></Field><Field label="目标分支"><input name="branch" defaultValue="main" required /></Field></div><Field label="受保护路径" hint="一行一个模式。匹配内容需要对固定候选特别批准。"><textarea name="protected" rows={4} defaultValue={'internal/app/**\ninternal/domain/**\ninternal/process/**\n.github/**'} /></Field><div className="subheading"><h2>最低检查</h2><button type="button" className="secondary" onClick={() => { setChecks((previous) => [...previous, { key: nextKey, name: '', command: '', args: '', timeout: '300' }]); setNextKey((v) => v + 1); }}>添加检查</button></div><label><input type="checkbox" checked={draftOnly} onChange={(e) => setDraftOnly(e.target.checked)} />先登记暂停草案，访谈后由 Owner 配置真实检查</label><p className="muted">空检查可以开始访谈，但不能确认基线或执行任务。直接调用可执行文件。参数一行一个，不拼接 shell 命令。</p>{!draftOnly && checks.map((check, index) => <fieldset className="check-draft" key={check.key}><legend>检查 {index + 1}</legend><div className="form-grid"><Field label="名称"><input required value={check.name} onChange={(e) => change(check.key, 'name', e.target.value)} /></Field><Field label="可执行文件"><input required value={check.command} onChange={(e) => change(check.key, 'command', e.target.value)} /></Field><Field label="参数"><textarea rows={2} value={check.args} onChange={(e) => change(check.key, 'args', e.target.value)} /></Field><Field label="超时（秒）"><input required type="number" min="1" max="3600" value={check.timeout} onChange={(e) => change(check.key, 'timeout', e.target.value)} /></Field></div>{checks.length > 1 && <button type="button" className="text-button danger" onClick={() => setChecks((previous) => previous.filter((item) => item.key !== check.key))}>移除此检查</button>}</fieldset>)}<Feedback {...action} /><div className="form-footer"><small>登记后先初始化共享基线；空门禁不能放行。</small><button className="primary" disabled={action.busy}>{action.busy ? '正在建立…' : '确认并建立项目'}</button></div></form></>;
}
