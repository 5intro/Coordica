# Coordica implementation rules

- Follow the implementation rules below. Maintainers also keep local design notes under ignored `docs/`; those private files are not required to build or use the published project. Phase A and Phase B implementation are authorized as of 2026-10-11; this does not grant runtime goal, task, scope, protected-candidate, or external permissions.
- Keep modules cohesive: domain types/rules, transactional application service, persistence, transports, Git operations, and process/tool adapters have distinct owners.
- HTTP, CLI, MCP, and background workers must call the same application use cases. Never duplicate authorization or state transitions in an entry point.
- `internal/gitops` and `internal/process` know nothing about HTTP, SQLite, or task state. `internal/agent` knows tool protocols, not scheduling.
- Prefer small consumer-owned interfaces at I/O boundaries. Do not add an interface per struct or general workflow framework.
- Persist state, audit, and outbox together. Keep Git/process/network I/O outside database transactions.
- Never infer completion from model prose. Preserve fixed candidate, lease, authorization, and cumulative accounting invariants. Project used/reserved spending is telemetry, never a lifetime cap; goal/task/Run policies remain finite and independent.
- Tests should exercise actual failure modes: contention, stale requests, duplicate messages, cancellation, scope violations, candidate/main changes, and restart recovery.
- Do not claim an OS/tool combination is verified without running it. Cross-compilation is recorded separately.
- Use native Codex search/browser tools. Do not import Claude web-access or CDP proxy workflows.
- Use native Codex CLI for Leader planning and development. The default model remains `gpt-6.1-sol`; Owner may explicitly select any model and reasoning effort actually reported by a Runner's Codex tool. Pin the selected values per Run, with no silent model fallback. Do not invoke nested agents or CLI/model wrappers. Claude and dsh are for tests only (including independent read-only review acceptance), not development.

- Each physical host reuses one limited interaction MCP/profile. Center chat credentials are Owner-issued and may contact Leader only on admin/shared loopback. Paired Runner hosts mint node-bound chat credentials; remote TLS accepts only node-chat, shared safe facts, requirements and own-node safe runtime queries. Persisted scope/node registration is checked transactionally before replay. Legacy unscoped credentials require explicit renewal; never infer authority from profile flags/source IDs.
- Consultation accounts are cumulative statistics/reservation targets, not lifetime allowances. Each consultation retains a finite timeout and bounded retries. Do not add progress polling timers or a runtime mailbox module.
