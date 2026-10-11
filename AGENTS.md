# Coordica implementation rules

- Follow the implementation rules below. Maintainers also keep local design notes under ignored `docs/`; those private files are not required to build or use the published project. Phase A and Phase B implementation are authorized as of 2026-10-11; this does not grant runtime goal, task, scope, protected-candidate, or external permissions.
- Keep modules cohesive: domain types/rules, transactional application service, persistence, transports, Git operations, and process/tool adapters have distinct owners.
- HTTP, CLI, MCP, and background workers must call the same application use cases. Never duplicate authorization or state transitions in an entry point.
- `internal/gitops` and `internal/process` know nothing about HTTP, SQLite, or task state. `internal/agent` knows tool protocols, not scheduling.
- Prefer small consumer-owned interfaces at I/O boundaries. Do not add an interface per struct or general workflow framework.
- Persist state, audit, and outbox together. Keep Git/process/network I/O outside database transactions.
- Never infer completion from model prose. Preserve fixed candidate, lease, authorization, and cumulative-budget invariants.
- Tests should exercise actual failure modes: contention, stale requests, duplicate messages, cancellation, scope violations, candidate/main changes, and restart recovery.
- Do not claim an OS/tool combination is verified without running it. Cross-compilation is recorded separately.
- Use native Codex search/browser tools. Do not import Claude web-access or CDP proxy workflows.
- Use native Codex CLI with explicit `gpt-6.1-sol` only for Leader planning and development. Do not invoke nested agents, CLI/model wrappers, Astra, or any fallback model. Claude and dsh are for tests only (including independent read-only review acceptance), not development.
