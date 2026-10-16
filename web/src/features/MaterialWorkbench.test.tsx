import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { App } from '../App';
import { ApiClient } from '../api/client';
import { LocalClient } from '../api/localClient';
import type { Candidate } from '../api.generated';
import { centerSnapshot, connectionInfo } from '../test/connectionInfo';
import { localTask } from '../test/nodeSnapshot';
import { TOKEN_KEY } from './Login';
import { Overview } from './Overview';
import { WorkItemPanel } from './WorkItemPanel';

const requirement = { id:'requirement-private-id', version:1, title:'保持原文', body:'需求原文内容', assistant_summary:'助手独立摘要', created_at:'' };
const task = { ...localTask, title:'关联开发工作', requirement_ids:[requirement.id] };
const snapshot = { ...centerSnapshot, requirements:[requirement], tasks:[task] };
afterEach(() => { cleanup(); sessionStorage.clear(); window.history.replaceState(null,'','/'); vi.restoreAllMocks(); });

it('opens a real homepage requirement without writing, preserves a rejected draft, and acknowledges only accepted writes', async () => {
  sessionStorage.setItem(TOKEN_KEY,'local-test-owner');
  window.history.replaceState(null,'','/#overview');
  vi.spyOn(LocalClient.prototype,'info').mockResolvedValue({ mode:'center' });
  vi.spyOn(ApiClient.prototype,'snapshot').mockResolvedValue(snapshot);
  vi.spyOn(ApiClient.prototype,'read').mockResolvedValue(connectionInfo);
  const write = vi.spyOn(ApiClient.prototype,'write').mockRejectedValueOnce(new Error('中心拒绝了此次写入')).mockResolvedValueOnce({});
  render(<App/>);
  await screen.findByRole('heading',{ name: snapshot.project!.name });
  expect(write).not.toHaveBeenCalled();
  const pool = within(screen.getByRole('region',{name:'需求池'}));
  expect(pool.getByRole('link',{name:'需求池'})).toBeTruthy();
  expect(pool.queryByText('查看全部')).toBeNull();
  fireEvent.click(screen.getByRole('link',{name:'提交需求'}));
  const dialog = within(screen.getByRole('dialog',{name:'新需求'}));
  expect(dialog.getByRole('button',{name:'保存需求'})).toHaveProperty('disabled',true);
  fireEvent.change(dialog.getByLabelText('需求标题'),{target:{value:'  一项新需求  '}});
  fireEvent.change(dialog.getByLabelText('原始需求与成功条件'),{target:{value:'用户的原始要求'}});
  fireEvent.click(dialog.getByRole('button',{name:'保存需求'}));
  await dialog.findByRole('alert');
  expect(dialog.getByLabelText('需求标题')).toHaveProperty('value','  一项新需求  ');
  expect(screen.queryByText('需求已记录。')).toBeNull();
  fireEvent.click(dialog.getByRole('button',{name:'保存需求'}));
  await screen.findByText('需求已记录。');
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(write).toHaveBeenCalledTimes(2);
  expect(write).toHaveBeenLastCalledWith('/requirements',{title:'一项新需求',body:'用户的原始要求'});
});

it('keeps source focus through related inspection and removes stale content when a record leaves the snapshot', async () => {
  const navigate = vi.fn();
  const view = render(<Overview snapshot={snapshot} navigate={navigate}/>);
  const origin = within(screen.getByRole('region',{name:'需求池'})).getByRole('button',{name:/保持原文/});
  origin.focus(); fireEvent.click(origin);
  expect(navigate).not.toHaveBeenCalled();
  const first = within(screen.getByRole('dialog',{name:'保持原文'}));
  expect(first.getByText('需求原文内容')).toBeTruthy();
  fireEvent.click(first.getByRole('tab',{name:'关联 1'}));
  fireEvent.click(first.getByRole('button',{name:/关联开发工作/}));
  const related = within(screen.getByRole('dialog',{name:'关联开发工作'}));
  fireEvent.click(related.getByRole('button',{name:'返回上一项'}));
  expect(screen.getByRole('dialog',{name:'保持原文'})).toBeTruthy();
  view.rerender(<Overview snapshot={{...snapshot,requirements:[]}} navigate={navigate}/>);
  expect(screen.getByRole('dialog',{name:'记录不在当前快照中'})).toBeTruthy();
  expect(screen.queryByText('需求原文内容')).toBeNull();
  fireEvent.keyDown(document,{key:'Escape'});
  expect(screen.queryByRole('dialog')).toBeNull();
  // The source disappeared; closing must not try to focus a detached element.
  expect(document.activeElement?.isConnected).toBe(true);
  view.rerender(<Overview snapshot={snapshot} navigate={navigate}/>);
  const restored = within(screen.getByRole('region',{name:'需求池'})).getByRole('button',{name:/保持原文/});
  restored.focus(); fireEvent.click(restored);
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button',{name:'关闭详情面板'})));
  fireEvent.keyDown(document,{key:'Escape'});
  expect(document.activeElement).toBe(restored);
});

it('does not label another fixed candidate’s historical review as approval', () => {
  const candidate: Candidate = { id:'fixed', version:1, task_id:task.id, source_run_id:'run', authorization_version:1, policy_version:1, baseline_version:1, baseline_digest:'b', base:'B', head:'H', main:'M', commit:'current-C', state:'reviewing', detail:'',protected:false,approved_commit:'',checks:[],created_at:'',review:{candidate:'old-C',verdict:'approve',summary:'旧版本通过',findings:[],run_id:'old-review'} };
  render(<WorkItemPanel snapshot={{...snapshot,candidates:[candidate]}} target={{page:'candidates',id:candidate.id}} onClose={vi.fn()} open={vi.fn()} navigate={vi.fn()}/>);
  const panel = within(screen.getByRole('dialog'));
  expect(panel.getByText('尚无当前固定版本的独立评审结论。')).toBeTruthy();
  expect(panel.getByText('尚无检查结果，不算通过。')).toBeTruthy();
  expect(panel.queryByText('旧版本通过')).toBeNull();
  expect(panel.queryByText('评审通过')).toBeNull();
});

it('preserves the search field focus after the panel animation frame and restores its origin on close', async () => {
  const { WorkspaceSearch } = await import('./WorkspaceSearch');
  const frames: FrameRequestCallback[] = [];
  vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => { frames.push(callback); return frames.length; });
  const origin = document.createElement('button'); document.body.append(origin); origin.focus();
  const onClose = vi.fn(); const view = render(<WorkspaceSearch snapshot={snapshot} onClose={onClose} navigate={vi.fn()}/>);
  const input = screen.getByRole('searchbox', { name: '搜索工作' });
  for (const frame of frames) frame(0);
  expect(document.activeElement).toBe(input);
  fireEvent.change(input, { target: { value: '保持原文' } });
  expect(input).toHaveProperty('value', '保持原文');
  fireEvent.keyDown(document, { key: 'Escape' }); expect(onClose).toHaveBeenCalledTimes(1);
  view.unmount(); expect(document.activeElement).toBe(origin); origin.remove();
});

it('inspects overview blocks with their decision evidence instead of generic source details', () => {
  const decision = { id:'decision_fixture', version:1, source_kind:'task', source_id:task.id, source_version:task.version, document_version:1, authorization_version:1, kind:'scope', evidence:'需要 Owner 判断的新范围证据', actions:['edit_scope'], status:'open', resolution:'' };
  render(<Overview snapshot={{...snapshot, tasks:[{...task,hold_reason:'scope'}], decisions:[decision]}} navigate={vi.fn()}/>);
  fireEvent.click(within(screen.getByRole('region',{name:'阻断池'})).getByRole('button'));
  const panel = within(screen.getByRole('dialog'));
  expect(panel.getByText('需要 Owner 判断的新范围证据')).toBeTruthy();
  expect(panel.getByRole('button',{name:'打开阻断页面 →'})).toBeTruthy();
});
