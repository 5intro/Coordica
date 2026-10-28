import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Candidate, Project } from '../api.generated';
import { ApiClient } from '../api/client';
import { centerProject } from '../test/connectionInfo';
import { PublicationSettings } from './PublicationSettings';
import { PullRequestStatus } from './PullRequestStatus';

const candidate: Candidate = { baseline_version: 1, baseline_digest: 'fixture', id: 'candidate-1', version: 7, task_id: 'task-1', source_run_id: 'run-1', authorization_version: 2, policy_version: 1, base: 'base', head: 'head', main: 'main', commit: 'fixed-commit', state: 'ready_to_land', detail: '', protected: false, approved_commit: '', checks: [], created_at: '', pull_request: { number: 7, url: 'https://github.com/example/repo/pull/7', branch: 'coordica/task-1', head: 'fixed-commit', base: 'main', state: 'open', ci: 'pending', review: 'none', checks: [{ name: 'verify (windows-latest)', state: 'pending', app_id: 42, url: '' }], reviews: [], merge_commit: '', merge_ready: false, detail: '等待必需检查', synced_at: '2026-10-19T08:00:00Z' } };

describe('GitHub publication', () => {
  it('requires safe numeric source identities and a review kind before saving a trusted provider', async () => {
    const project: Project = { ...centerProject, paused: true, publication: { mode: 'github', required_checks: ['ci'] } };
    const client = new ApiClient('fixture');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ ...project, version: 2 });
    render(<PublicationSettings project={project} client={client} refresh={vi.fn()} />);
    fireEvent.click(screen.getByLabelText('要求 Codex 外部审查'));
    const save = screen.getByRole('button', { name: '保存发布方式' }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('机器人用户 ID'), { target: { value: '9007199254740992' } });
    fireEvent.change(screen.getByLabelText('GitHub App ID'), { target: { value: '99' } });
    expect(save.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('机器人用户 ID'), { target: { value: '42' } });
    fireEvent.click(screen.getByLabelText('代码审查'));
    fireEvent.click(screen.getByLabelText('安全审查'));
    expect(save.disabled).toBe(true);
    fireEvent.click(screen.getByLabelText('安全审查'));
    fireEvent.click(save);
    await waitFor(() => expect(write).toHaveBeenCalledWith('/project/publication', { expected_version: 1, policy: { mode: 'github', required_checks: ['ci'], review_providers: [{ provider: 'codex', actor_id: 42, app_id: 99, kinds: ['security_review'] }] } }));
  });

  it('shows exact-head external review results without unsafe report links', () => {
    render(<PullRequestStatus candidate={{ ...candidate, pull_request: { ...candidate.pull_request!, external_reviews: [{ provider: 'codex', kind: 'security_review', actor_id: 42, app_id: 99, report_id: 10, commit: 'old', state: 'pending', url: 'javascript:alert(1)', summary: 'incomplete' }] } }} client={new ApiClient('fixture')} refresh={vi.fn()} />);
    expect(screen.getByText('安全审查')).toBeTruthy();
    expect(screen.getByText(/等待检查 · 历史版本/)).toBeTruthy();
    expect(screen.queryByRole('link', { name: '查看报告' })).toBeNull();
  });
  it('shows PR and CI without introducing a second approval', async () => {
    const client = new ApiClient('fixture-token');
    const write = vi.spyOn(client, 'write').mockResolvedValue(candidate);
    render(<PullRequestStatus candidate={candidate} client={client} refresh={vi.fn()} />);
    expect(screen.getByRole('link', { name: '在 GitHub 查看' }).getAttribute('href')).toBe(candidate.pull_request?.url);
    expect(screen.queryByRole('button', { name: /批准|合并/ })).toBeNull();
    expect(screen.getByText('等待必需检查')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '同步 PR 状态' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/candidates/candidate-1/sync', { expected_version: 7 }));
  });

  it('identifies historical evidence and blocks unsafe external links', () => {
    render(<PullRequestStatus candidate={{ ...candidate, pull_request: { ...candidate.pull_request!, head: 'old-commit', ci: 'success', url: 'javascript:alert(1)' } }} client={new ApiClient('fixture')} refresh={vi.fn()} />);
    expect(screen.getByText(/历史版本/)).toBeTruthy();
    expect(screen.queryByRole('link', { name: '在 GitHub 查看' })).toBeNull();
  });

  it('keeps local Git candidates free of PR controls', () => {
    const { container } = render(<PullRequestStatus candidate={{ ...candidate, pull_request: undefined }} client={new ApiClient('fixture')} refresh={vi.fn()} />);
    expect(container.textContent).toBe('');
  });

  it('saves only publication policy, preserving pause and credential boundaries', async () => {
    const project: Project = { ...centerProject, paused: true };
    const client = new ApiClient('fixture-token');
    const write = vi.spyOn(client, 'write').mockResolvedValue({ ...project, version: 2 });
    render(<PublicationSettings project={project} client={client} refresh={vi.fn()} />);
    fireEvent.change(screen.getByLabelText('发布方式'), { target: { value: 'github' } });
    fireEvent.change(screen.getByLabelText('必需的 GitHub CI 检查'), { target: { value: 'verify (windows-latest)\nverify (macos-latest)' } });
    fireEvent.click(screen.getByRole('button', { name: '保存发布方式' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/project/publication', { expected_version: 1, policy: { mode: 'github', required_checks: ['verify (windows-latest)', 'verify (macos-latest)'] } }));
    expect(write).toHaveBeenCalledTimes(1);
    expect(screen.queryByLabelText(/token|凭据/i)).toBeNull();
  });

  it('preserves unsaved input after failure and refuses a stale project version', async () => {
    const project: Project = { ...centerProject, paused: true };
    const client = new ApiClient('fixture');
    vi.spyOn(client, 'write').mockRejectedValue(new Error('候选尚未结束'));
    const { rerender } = render(<PublicationSettings project={project} client={client} refresh={vi.fn()} />);
    fireEvent.change(screen.getByLabelText('发布方式'), { target: { value: 'github' } });
    fireEvent.change(screen.getByLabelText('必需的 GitHub CI 检查'), { target: { value: 'ci' } });
    fireEvent.click(screen.getByRole('button', { name: '保存发布方式' }));
    await screen.findByText('候选尚未结束');
    expect((screen.getByLabelText('必需的 GitHub CI 检查') as HTMLTextAreaElement).value).toBe('ci');
    rerender(<PublicationSettings project={{ ...project, version: 2 }} client={client} refresh={vi.fn()} />);
    expect((screen.getByRole('button', { name: '保存发布方式' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('cannot change publication mode while the project is running', () => {
    render(<PublicationSettings project={centerProject} client={new ApiClient('fixture')} refresh={vi.fn()} />);
    expect((screen.getByRole('button', { name: '保存发布方式' }) as HTMLButtonElement).disabled).toBe(true);
  });
});
