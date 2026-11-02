# Coordica implementation rules

- Product direction follows [ROADMAP.md](ROADMAP.md) and the Owner's
  [R2026-11-02.3 requirements](planning/REQUIREMENTS.md). They supersede conflicting old product assumptions:
  Coordica is task-driven native-harness infrastructure; sessions are execution resources, not the root of work.
  Runtime views are Tasks and Attention, derived from source facts; requirements and business review pools belong
  to applications. See [view boundaries](planning/POOL_BOUNDARIES.md). Do not add a duplicate Blocker state machine.
  Programmatic and authorized event inputs must work without a user chat. Git, candidates, review and publication
  belong to the development application. See [task-driven design](planning/TASK_DRIVEN.md).
  This direction and planning work do not authorize new implementation, live harness calls, deployment or runtime tasks.
  Historical Phase A/B authorization does not automatically cover the new roadmap.
  The Owner's later cloud implementation authorization is recorded in [CLOUD_EXECUTION](planning/CLOUD_EXECUTION.md)
  and PROGRESS: implement C01–C08 with offline verification and the test/PR/review/merge loop; C09–C12 are preparation only.
  It does not authorize live harness subscription calls, deployment or business execution; keep unverified stage exits open.
- Before related work, read the roadmap, [design](planning/DESIGN.md), [audit](planning/AUDIT.md),
  [validation/handoff](planning/VALIDATION.md) and [progress log](planning/PROGRESS.md).
  Keep task status, decisions and fixed-version evidence current at each relevant handoff. Do not mark features complete
  from declarations, mocks or prose. These tracked files are the cloud handoff source; ignored private `docs/` are optional.
- Follow the implementation rules below within their applicable layer. Preserve current development behavior during
  incremental extraction; generic runtime tasks must not acquire mandatory Git or business acceptance requirements.
- Keep modules cohesive: domain types/rules, transactional application service, persistence, transports, Git operations, and process/tool adapters have distinct owners.
- HTTP, CLI, MCP, and background workers must call the same application use cases. Never duplicate authorization or state transitions in an entry point.
- `internal/gitops` and `internal/process` know nothing about HTTP, SQLite, or task state. `internal/agent` knows tool protocols, not scheduling.
- Prefer small consumer-owned interfaces at I/O boundaries. Do not add an interface per struct or general workflow framework.
- Prefer parallel execution of genuinely independent tasks when host resources and integration safety permit. Plan stable interfaces and clear scopes first; separate per-Run workspaces/outputs from real shared resources. Bound editing and heavy-test concurrency by actual host capacity/pressure, and explain serialization. Do not fragment work merely to inflate Agent counts or treat a configured slot count as resource verification. Preserve fixed candidates, stop evidence and version fences during parallel integration.
- Read [QUALITY.md](QUALITY.md) and staged cleanup tasks in [AGENT.md](AGENT.md). Review semantic cohesion and maintenance risks; numeric measurements are advisory, not automatic merge vetoes.
- Use [REVIEW.md](REVIEW.md) for evidence-based independent review and convergent re-review. Preserve unchanged acceptance and authority when routing failures to scoped repairs and fresh verification.
- Reference signals: 160 Unicode characters per line, 500 lines per file, 80 per function, 200 per class and 20 methods per class/Go receiver. Use judgment; do not chase syntax edge cases or churn formatting to hit a number. Focus on growing modules and mixed responsibilities.
- Keep formatting-only changes separate from behavior changes where practical. Split by responsibility, not arbitrary line counts. Preserve transaction atomicity, identity/generation fencing, cumulative accounting, process-stop evidence and final candidate/version checks during extraction.
- Run `make quality` before handoff. It reports physical-size/standard Go AST risk hints. Use project-native lint/build/type checks and regression tests; do not maintain a custom multi-language parser. Source inventory safety, scope/authorization, fixed versions and test/review evidence remain mandatory.
- Persist state, audit, and outbox together. Keep Git/process/network I/O outside database transactions.
- Never infer completion from model prose. Preserve fixed candidate, lease, authorization, and cumulative accounting invariants. Project used/reserved spending is telemetry, never a lifetime cap; cumulative allowances are optional and disabled by default; development uses progress observations with renewable leases. Finite operation timeouts and retry/repair limits remain independent.
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

- For new runtime interfaces, use the roadmap's instance and four-state capability model. Allow explicitly configured
  unverified options with honest status and policy checks; do not silently change model, harness or execution mode.
  Legacy defaults and development execution-intent contracts apply to their existing records/application only.
- Existing development chat profiles retain their current limited authority: one shared interaction profile per host,
  Owner-issued center credentials on admin/shared loopback, and node-bound remote chat credentials. New runtime APIs
  use explicit application/instance/node scopes as planned; they must not silently widen old credentials. Check persisted
  authority before replay; unscoped legacy credentials require renewal, never inferred authority from profile/source IDs.
- Consultation accounts remain cumulative statistics/reservation targets, not lifetime allowances; finite timeouts and
  bounded retries remain. Development progress checks default to 30 minutes and reuse heartbeats/structured activity.
  Missing activity alone is not failure. The former blanket runtime-mailbox prohibition is superseded for required
  durable messages, native questions/approvals and delegation results. Implement bounded delivery primitives through
  existing use-case/transaction boundaries, not a generic business mailbox or a new agent reasoning loop.
