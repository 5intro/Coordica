import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Candidate, ReviewIssue } from '../../api.generated';
import { ApiClient } from '../../api/client';
import { ReviewIssues } from './ReviewIssues';

const candidate: Candidate = {
  id: 'candidate_one', version: 7, task_id: 'task_one', source_run_id: 'run_implement', authorization_version: 1, policy_version: 1,
  baseline_version: 1, baseline_digest: 'baseline', base: 'base', head: 'head', main: 'main', commit: 'current', state: 'rejected',
  detail: '', protected: false, approved_commit: '', checks: null, created_at: '',
  review_rules: { version: 1, base: 'main', digest: 'rules_digest', files: [{ path: 'REVIEW.md', text: 'Review retry safety.' }], changed: ['REVIEW.md'] },
};
const issue: ReviewIssue = {
  id: 'issue_one', version: 3, task_id: 'task_one', source: 'internal', state: 'open',
  finding: { key: 'retry', severity: 'P1', title: '重复写入', path: 'src/store.go', line: 12, trigger: '重复请求', evidence: '同一键产生两条记录', impact: '数据重复' },
  first_candidate: 'old', last_candidate: 'old', source_run_id: 'review_old', rules_digest: 'old_rules', resolution_commit: '', resolution_reason: '', resolved_by: '', created_at: '', updated_at: '',
};

function setup(items: ReviewIssue[] = [issue]) {
  const client = new ApiClient('fixture');
  const read = vi.spyOn(client, 'read').mockResolvedValue({ items, next: '' });
  const refresh = vi.fn();
  const view = render(<ReviewIssues candidate={candidate} client={client} refresh={refresh} />);
  return { client, read, refresh, ...view };
}

describe('review issue ledger', () => {
  it('lazily loads evidence, preserves rules and binds an Owner recheck to the displayed versions', async () => {
    const { client, read, refresh } = setup();
    const write = vi.spyOn(client, 'write').mockResolvedValue({ ...issue, version: 4, state: 'false_positive' });
    expect(read).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: '审查问题' }));
    await screen.findByText('P1 · 重复写入');
    expect(read.mock.calls[0][0]).toBe('/review-issues?task_id=task_one&limit=64&after=');
    fireEvent.change(screen.getByLabelText('处置依据：重复写入'), { target: { value: '事务回归证实重复键被拒绝' } });
    fireEvent.click(screen.getByLabelText('全部问题处置后重新审查此版本，保持暂停'));
    fireEvent.click(screen.getByRole('button', { name: '记录处置' }));
    await waitFor(() => expect(write).toHaveBeenCalledWith('/review-issues/issue_one/resolve', {
      expected_version: 3, candidate_id: 'candidate_one', commit: 'current', disposition: 'false_positive', reason: '事务回归证实重复键被拒绝', request_recheck: true, candidate_version: 7,
    }));
    expect(refresh).toHaveBeenCalled();
    expect(screen.getByText('Review retry safety.')).toBeTruthy();
  });

  it('keeps disposition input and error after a rejected write without silently submitting a fresh version', async () => {
    const { client, refresh } = setup();
    const write = vi.spyOn(client, 'write').mockRejectedValue(new Error('候选版本已变化'));
    fireEvent.click(screen.getByRole('button', { name: '审查问题' }));
    await screen.findByText('P1 · 重复写入');
    fireEvent.change(screen.getByLabelText('处置依据：重复写入'), { target: { value: '保留这段依据' } });
    fireEvent.click(screen.getByRole('button', { name: '记录处置' }));
    await screen.findByText('候选版本已变化');
    expect((screen.getByLabelText('处置依据：重复写入') as HTMLTextAreaElement).value).toBe('保留这段依据');
    expect(refresh).not.toHaveBeenCalled();
    expect(write).toHaveBeenCalledTimes(1);
  });

  it('uses a real cursor to append more records without losing the first page', async () => {
    const { read } = setup();
    read.mockResolvedValueOnce({ items: [issue], next: 'issue_one' }).mockResolvedValueOnce({ items: [{ ...issue, id: 'issue_two', finding: { ...issue.finding, title: '另一个问题' } }], next: '' });
    fireEvent.click(screen.getByRole('button', { name: '审查问题' }));
    await screen.findByText('P1 · 重复写入');
    fireEvent.click(screen.getByRole('button', { name: '加载更多问题' }));
    await screen.findByText('P1 · 另一个问题');
    expect(screen.getByText('P1 · 重复写入')).toBeTruthy();
    expect(read.mock.calls[1][0]).toContain('after=issue_one');
  });

  it('never offers local risk waivers for external findings and labels old dispositions', async () => {
    setup([{ ...issue, source: 'external/codex/security_review/42/99', state: 'resolved_external', resolution_commit: 'old' }]);
    fireEvent.click(screen.getByRole('button', { name: '审查问题' }));
    await screen.findByText('P1 · 重复写入');
    expect(screen.getByText(/历史处置，待当前版本复核/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: '记录处置' })).toBeNull();
  });

  it('reports read failures and supports empty legacy responses', async () => {
    const { read } = setup();
    read.mockRejectedValueOnce(new Error('读取失败')).mockResolvedValueOnce({ items: null, next: '' });
    fireEvent.click(screen.getByRole('button', { name: '审查问题' }));
    await screen.findByRole('alert');
    fireEvent.click(screen.getByRole('button', { name: '刷新问题' }));
    await screen.findByText('暂无问题记录。');
  });
});
