import type { Initialization } from '../api.generated';
export const readyInitialization: Initialization = {
  version: 2, status: 'awaiting_owner', kind: 'existing', source_sha: 'a'.repeat(40), policy_digest: 'c'.repeat(64),
  available_paths: ['README.md', 'src/main.go'], covered_paths: ['README.md', 'src/main.go'], omissions: ['ignored private files excluded'],
  feedback: [{ id: 'feedback', version: 1, actor_id: 'owner', question_id: 'purpose', original_text: '保留原始用户回答', source: { session_id: 'owner-console', message_id: 'original' }, created_at: '' }],
  result: { generation: 1, initialization_version: 2, operation_id: 'init', source_sha: 'a'.repeat(40), ready: true, inspected_paths: ['README.md', 'src/main.go'], facts: [{ path: 'src/main.go', fact: '源码模块责任' }], suggestions: ['架构建议待 Owner 审阅'], questions: [], baseline: { requirements: '需求', acceptance: '验收', architecture: 'Go 与 SQLite', modules: 'src/: 业务', conventions: '小接口', testing: '真实失败测试', functionality: '功能与未完成项', risks: '待核验风险', allowed_roots: ['src'], protected_paths: ['internal/app/**'], checks: [{ name: 'Go 测试', command: 'go', args: ['test', './...'], timeout_seconds: 300 }] } },
  digest: 'b'.repeat(64), approved_version: 0, approved_digest: '', approved_baseline: null,
  operation_id: 'init', operation_generation: 1, operation_version: 3, operation_state: 'completed', detail: '',
};
