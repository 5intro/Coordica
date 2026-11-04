# Experimental durable task ledger

This package is an in-process C02 slice. It stores explicit work without a Git
repository, chat, development Goal, Candidate, review policy or running Leader.
Constructing it starts no process, harness, scheduler, service or background loop.
There are no new HTTP, CLI or MCP routes.

`NewManager(db)` is a trusted host configuration capability, equivalent to already
holding the Store. The host retains it privately; applications and legacy
Owner/node/chat routes must not receive it. It does not authenticate an external
administrator. The host explicitly configures grants and instances with CAS.
Grant tokens must be independently generated secrets of at least 32 bytes; the
ledger stores only SHA-256 hashes, compares them in constant time, and never
copies credentials or context into the globally visible legacy audit. Existing
profile/source IDs and credentials confer no new permission.

`New(db)` exposes the application ledger. Each call presents a principal ID and
token. The current stored grant determines the application, allowed actions and
exact instance/node pairs; caller-selected IDs are not authentication. Grant App
ownership and instance App/node/harness identity cannot be rebound. Configuration
changes advance the instance version. Capability support is four-state metadata,
not proof of authentication, execution permission or actual native compatibility.

The initial API is:

- `SubmitTask` / `ReadTask`: create or read an application-owned open task and
  its immutable, bounded context snapshot.
- `UpdateContext`: store a complete replacement snapshot of at most 64 KiB and
  an immutable source input. It does not deliver work or change old Run snapshots.
- `RegisterRun` / `ReadRun`: explicitly pin instance/config version, node,
  harness, model, effort, opaque workspace reference, current context and grant
  version. State is `registered`, native disposition `not_started`.
  Model and effort are independently optional: empty means unspecified. Supplied
  nonempty values remain exact, bounded identifiers; no default is selected or
  inferred. Required target identity/scope/config checks remain in force. A
  specific native adapter can still reject a missing selection before execution;
  registration does not prove the target is executable or configured effectively.
- `RecordContinuation` / `ReadInput`: record at most 64 KiB for an exact
  Run/generation/context version. `recorded` is not queued, delivered or consumed.
- `CancelRun` / `CancelTask`: cancel unstarted local records. No native stop,
  process-tree or completion evidence is invented. A new Run requires an explicit
  registration after cancelling the current one; its generation/attempt advances.

Task ownership is fixed to its application, while every Run independently pins
its target. Context versions remain immutable and readable through old Runs.
Unknown outcomes, native sessions, active-turn ownership, effects/ACKs and durable
request reconciliation still belong to the later C03–C06 integration.

All mutations use the existing SQLite transaction. Current credentials, action,
ownership and applicable target scope are checked before request replay. CAS and
lifecycle checks apply only to first writes. Exact successful retries return
historical receipts, including after cancellation; these receipts do not describe
the latest object state or authorize later native execution. Read the current
Task/Run and revalidate authority before any future execution integration.

Source identity is the unambiguous digest of application/source/event/task,
stored under the records primary key. A duplicate source with identical content
returns its original immutable input; different content or target conflicts.
Explicit multi-task fanout remains distinct. State, source input, audit/outbox
notification and request receipt commit or roll back together. Audit/outbox here
is a notification only, not a native effect queue, claim or delivery receipt.

The independent `runtime.*` record/event kinds are additive within the existing
store schema. Old development `task`/`run` scans cannot dispatch these records.
Reopening SQLite restores facts and receipts; it does not resume or replay work.
The current trusted private host boundary remains unchanged and is not an OS
isolation claim.
