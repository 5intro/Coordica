# Experimental ledger / Codex composition

This package implements the runtime-owned `SessionPort` using C07's typed
`StartThread`, `ResumeThread`, `Thread.Read` and `Peer.Close`. It is not wired to
the production Runner or HTTP/CLI/MCP routes. No constructor starts a process,
discovers credentials, probes native capabilities or changes configuration.

`New(hostLifetime, bindingConfig, appserverConfig)` is a trusted host capability.
The host must resolve the persisted node/instance/config/profile/workspace
references to the selected local executable, directory and explicit environment.
The environment is defensively copied; it is never inherited implicitly. Do not
expose this constructor or its executable/environment to application inputs or
legacy chat credentials. Profile/workspace/protocol references are host metadata,
not native authentication facts or an OS isolation guarantee.

Create/resume validates the exact configuration selected by the ledger. C07
checks native model, permission modes, canonical cwd and thread identity; the
remaining host references are retained selections, not independently attested
native fields. Effort is pinned and forwarded only for separately authorized
explicit work; forwarding does not prove actual reasoning effort. Create/resume
starts no Turn and passes no Task/context body.

Each in-memory lease is fixed to binding/owner/generation/config/native ID. The
map reserves opening before any RPC. Per-lease locks serialize read/close with
cancellable waiting; the map lock is not held across native I/O. A stopped lease
may only be replaced by explicit resume with a newer generation and the same ID.
Read requires an existing owned handle and never opens or resumes one.

JSON-RPC rejection and pre-send cancellation retain their distinct outcomes.
Configuration errors may carry an allocated native ID: its configuration is
retained privately for reconciliation, the owned Peer is closed, and no fallback
create or Turn is attempted. After uncertain resume, the original verified
resume target remains unchanged. Raw read history is private transient output;
the ledger rechecks authorization/fences before returning it, including errors.
Create/resume failures also recheck disclosure authority after cleanup; stale
callers cannot inspect native or cleanup private error objects through unwrap.

Stop uses `Peer.Close` and its actual `TreeStopped` result. Missing map entries,
nil Peers or opening failure do not manufacture stop evidence. Open can fail
after creating and closing a process without returning a Peer/proof; that case
stays conservative unknown. A released logical binding retains its Task/context.
New host/producer construction does not take over ready/unknown resources.

There is no automatic work execution, UI event subscription, request answer routing,
unknown-resource reconciliation or automatic retry/recovery here. Offline tests use real
SQLite and owned Go JSON-RPC fixture children with explicit minimal environments.
They are hand-written protocol simulations, not native transcripts or evidence
of real account, subscription, model or platform compatibility.

## Local event capture

Each Peer has an independent drainer using a separate small lock, never the
lease RPC lock, SQLite or a subscriber. It keeps at most 32 normalized facts.
Native text/questions/answers are discarded from the typed metadata mapping;
callback identity/kind is an observation, not a durable answerable C06 request.
Unknown methods, unsupported shapes or overflow close this owned Peer and retain
an explicit terminal gap and actual process-tree stop evidence.

SourceEpoch is the concrete Peer's initial Create/Resume command ID. Later
Read/Release command IDs cannot change it. C07 provides no native event sequence:
the drainer labels its monotone sequence and receipt IDs `local_observed`. Two
identical deltas are both observations. Reconnection uses another source epoch;
this is not native replay, a durable spool or cross-restart exactly-once delivery.

`Captured` snapshots a bounded batch without dequeueing; `ReleaseCaptured` only
releases positions after the runtime ledger commits them. It is not a native ACK.
Explicit `BindingProducer.DrainEvents` bridges the buffer and the indexed ledger.
Slow consumers do not hold protocol/DB locks; a prolonged backlog instead fails
at the explicit capacity boundary. Host crash can lose uncommitted capture, so
the implementation does not claim full C04 lossless critical-event retention.

## Explicit text work

`StartWork` is the narrow runtime-owned WorkPort adapter. It uses only the
already-owned Thread, exact binding/owner/generation/config/native identity and
fixed effort. It calls C07 `Thread.StartText` once with the separately supplied
text; it does not choose configuration or fetch Task context/continuations.
The concrete Peer's source epoch and pre-call observation boundary accompany
the native Turn ID/status. Accepted and terminal RPC evidence remain distinct.
The durable runtime command, permission, busy-slot and late-event policies stay
in `internal/runtime`, not in the protocol package. Queue/steer and
native request answers are not connected by this slice.

`InterruptWork` uses the original typed C07 Turn returned by StartText, cached
as one bounded target on its physical lease with logical Work ID, start command
and generation. It checks that target and the original source epoch, then calls
only `Turn.Interrupt`; it cannot forge a handle from a caller-supplied native ID.
Stop/new generation/resume does not transfer this target. ACK is only RPC
acceptance; terminal events and actual TreeStopped remain separate evidence.
