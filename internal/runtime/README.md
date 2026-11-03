# Experimental durable task ledger

This package contains in-process C02 and partial C03 slices. It stores explicit work without a Git
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
- `RecordContinuation` / `ReadInput`: record at most 64 KiB for an exact
  Run/generation/context version. `recorded` is not queued, delivered or consumed.
- `CancelRun` / `CancelTask`: cancel unstarted or proven session-stopped records. No native stop,
  process-tree or completion evidence is invented. A new Run requires an explicit
  registration after cancelling the current one; its generation/attempt advances.

Task ownership is fixed to its application, while every Run independently pins
its target. Context versions remain immutable and readable through old Runs.
Active turns, effects/ACKs and durable native request reconciliation still belong
to the later C03–C06 integration.

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

## Explicit session binding slice

`NewBindingProducer(db, config, port)` is trusted host wiring. It creates a fresh
private owner identity without changing persisted state, taking over a resource
or calling the port. The host resolves instance/config, profile, workspace,
protocol and effective permission modes. References and fingerprints do not
prove login health or OS isolation. Applications cannot supply native IDs or
arbitrary executable, environment, cwd, RPC method or prompt through this API.

- `PrepareBinding` requires the separate `session.control` action, the current
  grant and exact Run authorization/config/target/context snapshot. It stores a
  unique per-Run binding and pending command transactionally. An old Run cannot
  gain native authority merely because its grant is later widened.
- `Produce` durably claims that command once, then calls the narrow host `Create`
  port outside SQLite. Its typed input contains fixed configuration and command
  identity, with no Task/context body or Turn. A second transaction revalidates
  current authority and owner/generation before publishing `ready`. Duplicate or
  unknown calls do not create again. Effective configuration must match exactly.
- `ReadBinding` reads scoped ledger facts only. Native read/history and resume
  are not implemented here, and reading never starts or resumes work.
- `Release` closes only this producer's known idle binding. It requires an exact
  binding/owner/native-ID `TreeStopped` proof before `released/session_stopped`.
  It retains Task/context and does not cancel them. A nil Stop error or completed
  Turn alone is not such proof.
- `RecoverUnknown` explicitly advances the binding generation and fences the old
  owner. It performs no native I/O, recreation, resume or automatic takeover.
  A late result remains an observation of its original command and cannot make
  the new generation ready. Reopening SQLite does not automatically recover it.

Logical Task, Run and Binding IDs remain distinct from provider-native IDs.
Pending, bound or unknown sessions block the older immediate local cancellation
path and retain the current Run slot. Explicit rejected/not-sent creation may
return to `unbound/not_started` only after matching cleanup stop evidence is
saved in the same transaction. Otherwise the binding remains unknown even if
cleanup stopped the owned process; observed native IDs/configuration and stop
proof remain on the original command. This does not authorize a new create.

This slice supplies no production C07 adapter, native read/resume, active-turn
control, unknown-resource reconciliation or background recovery. Offline tests
use real SQLite and counted ports; one owned fake protocol child verifies the
DB-outside boundary and actual process-tree stop. Those tests do not establish
real harness compatibility, native authentication or production integration.
