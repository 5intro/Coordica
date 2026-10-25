# Coordica implementation rules

- Follow the implementation rules below. Maintainers also keep local design notes under ignored `docs/`; those private files are not required to build or use the published project. Phase A and Phase B implementation are authorized as of 2026-10-11; this does not grant runtime goal, task, scope, protected-candidate, or external permissions.
- Keep modules cohesive: domain types/rules, transactional application service, persistence, transports, Git operations, and process/tool adapters have distinct owners.
- HTTP, CLI, MCP, and background workers must call the same application use cases. Never duplicate authorization or state transitions in an entry point.
- `internal/gitops` and `internal/process` know nothing about HTTP, SQLite, or task state. `internal/agent` knows tool protocols, not scheduling.
- Prefer small consumer-owned interfaces at I/O boundaries. Do not add an interface per struct or general workflow framework.
- Read [QUALITY.md](QUALITY.md) and staged cleanup tasks in [AGENT.md](AGENT.md). Review semantic cohesion and maintenance risks; numeric measurements are advisory, not automatic merge vetoes.
- Reference signals: 160 Unicode characters per line, 500 lines per file, 80 per function, 200 per class and 20 methods per class/Go receiver. Use judgment; do not chase syntax edge cases or churn formatting to hit a number. Focus on growing modules and mixed responsibilities.
- Keep formatting-only changes separate from behavior changes where practical. Split by responsibility, not arbitrary line counts. Preserve transaction atomicity, identity/generation fencing, cumulative accounting, process-stop evidence and final candidate/version checks during extraction.
- Run `make quality` before handoff. It reports physical-size/standard Go AST risk hints. Use project-native lint/build/type checks and regression tests; do not maintain a custom multi-language parser. Source inventory safety, scope/authorization, fixed versions and test/review evidence remain mandatory.
- Persist state, audit, and outbox together. Keep Git/process/network I/O outside database transactions.
- Never infer completion from model prose. Preserve fixed candidate, lease, authorization, and cumulative accounting invariants. Project used/reserved spending is telemetry, never a lifetime cap; goal/task/Run policies remain finite and independent.
- Tests should exercise actual failure modes: contention, stale requests, duplicate messages, cancellation, scope violations, candidate/main changes, and restart recovery.
- Do not claim an OS/tool combination is verified without running it. Cross-compilation is recorded separately.
- Use native Codex search/browser tools. Do not import Claude web-access or CDP proxy workflows.
- Leader configuration uses any compatible harness/model/effort actually reported on the center host. Keep the legacy
  default `codex` / `gpt-6.1-sol` for unselected and old records; pin explicit choices per
  initialization/planning/consultation, with no silent fallback. Configuration is not development authorization. For
  this repository's implementation use native Codex or the primary assistant when Owner asks it to develop personally;
  Claude and dsh remain test-only unless Owner explicitly authorizes an experiment. Do not invoke nested agents or
  CLI/model wrappers.
- Product scheduling has no Codex-only development allowlist. Owner may select a registered compatible tool/model
  reported by a paired Runner; node role permissions and explicit execution options remain authoritative. Pin actual
  selections per Run, require the structured execution-intent contract for goal-bound development, and reject
  unsupported selections without falling back. This product capability does not change this repository's personal
  implementation restriction above.
- Follow [SECURITY.md](SECURITY.md): the supported execution environment is a trusted private host. Integration scope,
  process groups, permission modes and registered read-only roles do not attest OS isolation or same-user credential
  protection. Do not silently replace this boundary with a sandbox claim or weaken an externally provisioned sandbox.

- Each physical host reuses one limited interaction MCP/profile. Center chat credentials are Owner-issued and may contact Leader only on admin/shared loopback. Paired Runner hosts mint node-bound chat credentials; remote TLS accepts only node-chat, shared safe facts, requirements and own-node safe runtime queries. Persisted scope/node registration is checked transactionally before replay. Legacy unscoped credentials require explicit renewal; never infer authority from profile flags/source IDs.
- Consultation accounts are cumulative statistics/reservation targets, not lifetime allowances. Each consultation retains a finite timeout and bounded retries. Do not add progress polling timers or a runtime mailbox module.
