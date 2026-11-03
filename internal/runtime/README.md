# Experimental durable task ledger

This package contains in-process C02 and partial C03–C05 slices. It stores explicit work without a Git
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
Full input control, effects/ACKs and durable native request reconciliation still
belong to the later C05/C06 integration.

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
- `ReadBinding` reads scoped ledger facts only. `ReadNative` separately requires
  `session.control` and this producer's existing ready connection. It records
  only command outcome metadata; private native history is transient. Both
  success and error paths revalidate current authority and physical binding
  after I/O. Expired readers receive neither history nor private native errors.
- `ResumeBinding` accepts only a released binding with a matching persisted
  process-stop proof. It claims a new owner/command/generation, then restores
  the original native ID outside SQLite. It does not inject context or start a
  Turn. Unknown or unconfirmed old ownership cannot use this path. Duplicate
  commands do not repeat native resume; the caller reads current binding facts.
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

The separate `internal/runtimecodex` composition supplies a thin C07 port with
explicit host configuration. The protocol package does not depend on this
ledger. This is not production Runner integration, active-turn control,
unknown-resource reconciliation or background recovery. Offline tests
use real SQLite and counted ports; one owned fake protocol child verifies the
DB-outside boundary and actual process-tree stop. Those tests do not establish
real harness compatibility, native authentication or production integration.

Resume observations cannot replace the previously verified native resume
target, including when native returns a different ID. That observation and
private configuration evidence stay on its own command. An existing native
identity index may be reused only by the same binding; it is never reassigned.
Read never opens or resumes a Peer. A read connection failure closes the owned
Peer and records unknown; it does not transparently retry on a new connection.
Create/resume error exits separately recheck current authority and owner/generation
after cleanup. Expired callers receive safe fence/unknown errors without native
or cleanup error objects carrying private data. An authorized explicit rejection
retains its original RPC error classification; observations stay on its command.

## Bounded event facts and cursors

`OpenEventStream` / `AcceptEvent` bind normalized facts to the current
application, Task, Run, Binding, native ID and exact owner/generation. They do
not start work or infer Task success. Event kinds and payload fields are typed
and bounded: status, byte/token counts and native target/request references.
There is no free-form text, raw JSON params, native history or credential field.
Private text is not copied into global audit, outbox or idempotency responses.

An additive `runtime_events` SQL table uses application/stream/local sequence
keyset queries and separate unique constraints for event ID and source sequence.
It does not scan JSON records for high-frequency lookup. Stream-local commit
positions do not reveal another application's traffic. Facts, sequence/count
metadata, audit and notification commit or roll back in the same transaction.
Exact duplicate facts retain their original position without another audit;
ID/sequence reuse with different immutable content conflicts.
Historical source receipts are application facts, distinct from private client
cursor state. Another principal with the current required action/target scope
may obtain the same exact receipt through the same host owner/generation; it
does not write another fact or gain new execution authority. New first writes
still require the live Run's original authorization contract. Cross-application,
revoked/out-of-scope, conflicting-content and stale-generation requests fail.
New terminal actions on a possibly active source additionally require the live
Run's original principal and authorization version before changing the stream
or resource. A body claiming a new stop does not bypass that fence. Diagnostics
for a persistently known-stopped resource remain shareable under current scope,
but do not advance Binding/Run state or versions. Drain authorization failure
does not stop the Peer or release capture: cleanup I/O has its own current
execution/owner/generation fence. Host-internal overflow safety is separate.

The persisted DatasetEpoch survives reopening SQLite. Each SourceEpoch names a
specific producer connection. Native sources without event IDs/sequences use
explicit `local_observed` receipts; identical deltas are distinct observations.
There is no payload-hash deduplication or cross-reconnect exactly-once claim.
Late/out-of-order observations retain their source position; an observed gap is
sticky even when later rows fill it or a client acknowledges the page.

`OpenEventCursor` / `ReadEvents` / `AckEvents` hold server-side cursor metadata
bound to application, principal, fixed stream query and dataset/source epoch.
Every call rechecks current credentials and target scope. Pages contain at most
64 rows; response loss repeats the unacknowledged page. ACK is monotone and may
not exceed an offered position. Offered does not prove client receipt, and ACK
does not acknowledge native delivery, a tool approval or Task work consumption.
Same-owner/generation unknown-resource facts remain readable. A changed binding
owner/generation or dataset/source epoch expires the old cursor explicitly.

This slice has hard limits of 256 facts per stream and 10,000 per dataset. It
does not delete rows or deduplication keys, prune retention windows, or silently
reset the dataset. Capacity exhaustion records a sticky gap/failure and fences
the current binding unknown before returning backpressure. Recovery requires
explicit reconciliation; constructing another producer does not reset capacity.
These event limits are not a whole-host disk quota or a retention platform.

`DrainEvents` explicitly copies one bounded native capture batch, committing each
fact before releasing its local buffer position. Failed DB commits do not accept
the fact. Losing the local release acknowledgement may repeat the same fact and
is deduplicated. Subscribers read persisted pages independently of protocol I/O.
Terminal observed high-water marks never decrease; new valid stop evidence is
merged independently. A late closed callback cannot erase a failure/gap, and a
late diagnostic cannot rewrite an explicitly released resource's stop facts.

There is no native durable Runner spool, retention deletion, reliable external
effect delivery, automatic restart ingestion, C05/C06 routing or public event
subscription API here. Uncommitted in-memory capture can be lost on host crash;
reopening restores committed facts/cursors, not unknown native events. This is
an offline-tested partial C04 implementation, not completion of its full exit.

## Explicit single-work Turn slice

`PrepareWork` requires the independent `turn.start` action, the original Run
principal/authorization version and current target/configuration. Session control
alone cannot execute work, and widening a grant cannot upgrade an old Run.
The host first opens/drains the binding's event stream. Prepare stores one
bounded nonempty explicit text (at most 64 KiB) in private `runtime.work-input`,
with a durable command, fixed Run/context/binding/source snapshot and a single
active Work slot. It does not submit Task context or recorded continuations.
The receipt, global audit/outbox and idempotency response contain no text.

`ProduceWork` durably claims once, then invokes the consumer-owned `WorkPort`
outside SQLite. The C07 composition forwards only that text and fixed effort to
the existing owned Thread. Both confirmation and cleanup require current
execution authority, exact logical Work/command/slot, physical lifecycle command,
binding owner/generation and live source. Repeated/concurrent calls do not resend.
Publication failure, disconnect, malformed results or an expired fence retains
`delivery_unknown` and the occupied slot; restart does not replay it. Applicable
cleanup stop proof stays with the original Work; it does not prove nonexecution.

Work IDs are logical and independent of provider-native Turn IDs. A native ID
is optional: a protocol such as ACP can correlate an entire prompt RPC and return
terminal evidence without one. The generic offline port test covers this shape;
it does not establish an ACP adapter or real provider compatibility.

States distinguish `prepared`, `claimed`, `native_accepted`, `native_ended`,
`rejected` and `delivery_unknown`. Acceptance is not completion. C04 terminal
facts use a small indexed metadata record, scoped to the exact source epoch,
binding/owner/generation/native Turn and observed sequence boundary. An early
terminal is stored before the start response without guessing its Work identity;
the response then links it. Late facts after quarantine/unknown/stopped lifecycle
remain observations and cannot restore resource readiness or clear the Work slot.
`ReadWork` returns scoped metadata, never the private message body.

Prepared/active/unknown work blocks Release/Cancel from silently clearing or
stopping it. A valid native terminal permits the next explicit work on the same
healthy Peer, but does not cancel the Task, complete its business goal or prove
background/TreeStopped. Explicit Release still needs its separate actual stop
evidence. Reading a busy Peer tolerates logical Work receipt/terminal version
changes while retaining physical identity, authority and lifecycle fences.
An expired reader cannot close or quarantine the Peer. Unknown-read cleanup
rechecks current authority and the original busy Work/command/generation/slot;
a Work started or replaced during the read does not become its cleanup target.

This is not the complete C05 queue/withdraw/steer/interrupt API, C06 response
routing, automatic dispatcher or restart reconciliation. A prepared or unknown
slot is not automatically cancelled or retried. Only offline SQLite and owned
JSON-RPC fixture children are verified; real subscription execution remains
unverified and outside this development slice.

### Explicit single-Turn interrupt

`InterruptWork` requires the independent `turn.interrupt` action already present
in the fixed Run authorization. It targets one native-accepted Work with a real
native Turn ID, the current busy slot and exact physical/source identity. Missing
native IDs or typed port support return unsupported; there is no Stop fallback.
The transaction commits one claimed control command per Work before native I/O.
Same-key retries return its current receipt; another key cannot send it again.

The control receipt records `claimed`, `acknowledged`, `rejected`, `not_sent` or
`delivery_unknown` independently of Work state. An ACK never clears busy, ends a
Turn, stops its background work or releases the Session. Existing precise C04
terminal facts remain responsible for the Work result. A late ACK can only add
the old command's observation, including after the next Work is prepared.

Unknown delivery fences an unchanged active resource without Stop or slot
release; expired authority or changed ownership cannot mutate its replacement.
There is no automatic retry, including after a claimed command survives restart
without a result. This conservative once-per-Work policy also does not resend
explicitly rejected/not-sent commands. Scoped `ReadWorkInterrupt` returns only
metadata. Queue, steer and interruption of targets without native IDs remain
unimplemented, as does real-harness compatibility verification.
