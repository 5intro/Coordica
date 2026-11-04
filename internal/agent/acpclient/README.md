# Experimental ACP connection and Session transport

These partial C08 slices initialize a persistent supervised ACP connection and
explicitly create one Session. They do not implement authentication, loading,
runtime ports or a provider registration. Native permission replies are explicit
and bounded as described below; other callbacks remain unsupported. Explicit text
Prompt supports an invocation-owned cancellation signal and terminal response.
The production legacy `agent.executeACP` path is unchanged. That one-shot path
automatically starts a prompt and shuts down afterward, so it is not reused here.

The offline baseline is ACP v1 from the fixed
[v0.11.0 schema](https://github.com/agentclientprotocol/agent-client-protocol/blob/451c3cbc7df2f9bc164c25e25d36fa526003f085/schema/schema.json).
`Open` only sends `initialize`. There is no Codex `initialized` notification or assumed
immediate Turn ACK/native Turn ID. The client advertises no filesystem/terminal
callbacks. Prompt uses its full terminal response, retaining the ACP distinction
from app-server's short start ACK.

`OpenWithLifetime(host, operation, Config)` accepts trusted absolute executable
and canonicalized directory plus explicit argv/environment slices, which it
copies. No profile discovery, login, environment inheritance or fallback occurs.
The host owns process lifetime; cancelling a successfully completed operation
does not stop it. Initialization is bounded to ten seconds or the shorter caller
deadline, with one correlated response and a 256 KiB frame limit. Stdout must be
unambiguous JSON-RPC 2.0; stderr is drained without copying it to metadata/errors.

`Info()` defensively copies bounded agent declarations and authentication-method
IDs/names. Optional defaults follow the fixed schema, while null/type/version
violations fail closed. Unknown metadata is not interpreted as permission or
implemented capability. Declarations are neither authentication proof nor an
effective configuration/OS isolation attestation; treat them as private host
metadata before any later public persistence.

Explicit RPC rejection and unsupported version remain distinct from uncertain
initialization after send, cancellation, disconnect or malformed response.
`InitializationError.Process` and `Close()` carry actual supervised process-tree
evidence; a nil Peer alone says nothing about stopping. RPC error text excludes
native Message/Data, although their structured private objects remain available
to the trusted caller. Duplicate/unsolicited responses and unsupported callbacks,
including after initialization, close this connection rather than lose facts
or authorize a tool. There is no automatic retry or arbitrary Call API.

Tests use owned Go JSON-RPC children only. Antigravity is still the planned C08
provider; its release is not installed or executed here. Real authentication,
Session/work compatibility, callbacks and runtime composition remain unverified.

## Explicit Session creation

`NewSession(ctx)` makes one `session/new` allocation attempt per Peer with its
fixed canonical host directory and explicit `mcpServers: []`. The returned
immutable Session binds the validated native ID to that Peer and requested
directory. The directory is a selected request value, not an independent native
configuration or isolation attestation. Optional mode declarations receive only
container checks; bounded configuration options are retained as native reports. No
model, effort, permission mode or work input is selected by this operation.

The once-per-Peer policy includes known RPC rejection, and is a conservative
client limit rather than an ACP protocol restriction. Pre-cancelled calls do not
consume it or stop the Peer. Concurrent/repeated calls cannot create another
Session. A successful operation's context can end without killing the host Peer.
Sent cancellation, disconnect or invalid response closes and waits for the owned
process; `SessionCreationError` retains its actual Process proof and any validated
native ID already observed. Local TreeStopped does not prove deletion of a
native or remote Session. No native `session/close` or release RPC is invented.

`LoadSession` returns unsupported without RPC even if advertised: history replay
ordering and full private content/completion handling remain absent. There is no
authentication fallback, Task execution, database persistence or automatic retry
across a new Peer. Future runtime composition must persist its own intent and
unknown binding before native I/O. The runtime/runtimecodex composition in the
frozen checkpoint is a separate dependency, not part of this standalone package.

## Bounded update metadata

Only `session/update` notifications for this confirmed Session are captured.
Three text chunk kinds and `tool_call`/`tool_call_update` yield bounded metadata:
kind, UTF-8 byte count, tool reference and any explicitly supplied valid status.
Text, thought bodies, titles, tool input/output and history are not retained.
Absent/null tool-update status is not an invented transition. A tool completion
does not mean a prompt/Turn or Task completed, or a permission was granted.

The connection receives a fresh local epoch and observation sequence. Identical
deltas remain separate facts; these are not native event IDs, replay cursors or
cross-reconnect deduplication. `Session.Updates()` returns at most 32 facts with a
16 KiB metadata budget and a defensive copy. `ReleaseUpdates` advances only an
offered local prefix, sends no native ACK and cannot clear a gap or restart the
Peer. The read loop never waits for consumers, a database or an external callback.

Count/byte overflow preserves bounded accepted facts plus an observed high-water,
explicit backpressure/gap and actual process-stop evidence. Wrong Session,
unsupported/invalid updates and native request callbacks also fail closed without
answers. Diagnostics remain readable through the original private Session after
failure. Process.TreeStopped is set only after supervised Wait. Any close leaves
the final observation window unconfirmed, including intentional Close; clearing
the FIFO never claims complete history or removes that sticky gap.

All nine fixed-schema update tags now have bounded metadata handling, including
config-option snapshots described below. `plan`, `available_commands_update` and
`current_mode_update` validate their declared shapes and append only their kind.
Plan content/status/priority, command names/descriptions/input hints, mode IDs and
extension values are discarded; these facts do not dispatch work, execute a
command, select a mode, approve permissions or confirm effective configuration.
A completed plan entry does not complete a Prompt or prove cancellation. Only
the original correlated Prompt response supplies its terminal result.

Plan entries require content, priority and status without defaults; the fixed
priority/status enums remain strict. Command names/descriptions are required;
input may be absent/null, otherwise its object requires a string hint. Empty
lists, repeated declarations, empty strings and opaque native mode IDs are
accepted where the schema permits them. Unknown extension fields are ignored,
while malformed known fields, invalid metadata containers and duplicate JSON
keys fail closed. No smaller string or item-count limit is imposed: the existing
256 KiB whole-frame bound limits transient parsing, and each validated status
uses one slot in the same 32-fact/16 KiB metadata FIFO. A status flood preserves
the existing sticky gap/backpressure policy rather than blocking the read loop.

Unknown update tags and non-text content remain unsupported, rather than being
silently dropped or called schema-invalid. Notifications preceding a valid session/new
response are unsupported; the first observed Session ID is not guessed to be the
target. This metadata primitive is not lossless output, durable spool, complete
plan/command/mode history, Load/full-output support, permission handling or runtime
composition. Accepting these observations does not implement history loading.

## Explicit terminal text Prompt

`Session.PromptText(ctx, text)` sends exactly one bounded text content block to
the original Session and waits for the full `session/prompt` response. UTF-8 text
is nonempty and at most 64 KiB; the final JSON frame must also fit 256 KiB before
claim or send. One in-flight call is allowed per Session, including response
validation and writer reaping. Busy/invalid/pre-cancel calls do not send or stop
the Peer. A later call is always explicit; there is no queue, context injection
or automatic retry. Local request IDs increase and are never native Turn IDs.

The operation uses a five-minute client cap, shortened by the caller's deadline.
This is not an ACP or Task limit, nor the ten-second initialize/new deadline.
The receipt keeps the native `end_turn`, `max_tokens`, `max_turn_requests`,
`refusal` or `cancelled` stopReason with `rpc_terminal` evidence. It is neither
an immediate ACK, output text, business success nor a process-stop claim.
Updates remain Session-level metadata without a native Prompt ID: late updates
are not assigned to a nearby call, and old unconsumed facts are not cleared.
Consumers must keep draining/releasing the bounded capture or accept backpressure.

Known RPC rejection preserves its private structured error and normally leaves
the Peer available; it does not prove absence of earlier output/tools/effects.
Sent cancellation, disconnect, malformed response, unsupported callback or
overflow fails closed with `PromptError.Process` carrying actual cleanup proof.
Context cancellation never synthesizes native session/cancel, authentication or
tool replies. Explicit native cancellation uses the separate API below. A valid
terminal already observed during a ctx/EOF/writer race remains in the result;
transport failure is returned separately as disconnected with actual stop proof,
so the receipt does not imply the Peer can be reused. Caller ctx ending after
a successful completed operation does not end the independent host lifetime.

This protocol primitive does not select or attest model/effort/permissions,
preserve output bodies, answer tools, implement Load or authorize a Task.
Runtime composition is not connected. The frozen checkpoint's `ProduceWork`
ten-second start-ACK path needs a separate whole-turn adaptation before ACP use;
that runtime code is not included here. Real provider execution, authentication
and effective configuration remain unverified.

## Explicit native Prompt cancellation

`PromptTextWithCancel(ctx, text, cancellation)` extends the same whole-turn
operation with a caller-owned `<-chan struct{}` for that invocation. `PromptText`
uses a nil signal and retains its existing behavior. A pre-signaled call returns
local `context.Canceled` with no work, native notification or terminal receipt;
this is not a native `cancelled` result. Closing/signaling after the call returns
has no effect on a later Prompt. Use a fresh signal per invocation.

The fixed schema's `session/cancel` is a notification with only the original
Session ID, no request ID and no ACK. The first observed signal allows at most
one attempt, after the original full prompt write has completed. The worker
rechecks the exact pending invocation, Session/epoch and connection; an already
received response suppresses the write. Prompt/config singleflight remains held
until both writers are reaped, including terminal/cancellation/Close races. No
signal watcher survives return, no lock is held over blocking I/O, and no local
request ID is consumed by the notification.

Successful notification writing never completes this call or proves a stop.
Only the original correlated `session/prompt` response provides its native
`stopReason`: `cancelled` is the native cancellation claim, not OS process/tree,
background-tool or Task stop evidence. Late supported `session/update` facts
remain accepted through the existing bounded capture. A healthy cancelled Prompt
leaves the persistent Peer available for a later explicit Prompt.

If cancellation may have been emitted but the original response instead reports
another valid terminal or a structured RPC rejection, that exact report is
preserved with `ErrCancelUnconfirmed` and actual `PromptError.Process` cleanup
evidence. The client conservatively retires the Peer: local Write completion
cannot prove native handling order or prevent a delayed session-scoped cancel
from reaching a future turn. This containment policy is not an ACP requirement
and does not reclassify a known response as an unknown Prompt outcome.

No valid original response, write failure, disconnect, malformed/spurious ACK
or expiry retains the existing unknown-outcome/owned-process cleanup behavior.
An original terminal already observed during such failure remains a historical
fact with the transport failure separately reported. The signal never resets
or extends the shorter caller deadline/five-minute Prompt cap. Caller-context
cancellation still closes the Peer and does not itself request native cancel.

Tests use hand-written Go protocol children and controlled real pipe barriers,
not provider transcripts. Native provider cancellation and actual tool stopping
remain unverified; no runtime event, authentication or release path is added by
this cancellation primitive. Explicit permission replies are described below.

## Explicit native permission replies

`Session.PermissionRequests()` returns defensive private copies of currently
selectable `session/request_permission` callbacks. `SelectPermission(ctx, request,
optionID)` accepts only an exact option ID from that original callback, including
an empty ID if actually offered. The response preserves the option's original
native JSON value rather than reconstructing it from display text. No kind/name
mapping, default approval, fallback,
remembered client policy or arbitrary response API exists. A native
`allow_always`/`reject_always` choice is forwarded only when explicitly selected;
the client does not convert it into application or filesystem authority.

The opaque handle binds the Peer, local connection epoch, original Session,
local Prompt receive window and request serial. Native ACP callbacks contain a
Session ID, not a Prompt/Turn ID: this local fence cannot attest native per-turn
causality. Only an active explicit Prompt accepts callbacks. Wrong Session,
duplicate/reused native request IDs and unsupported callbacks close the Peer.
The no-reuse policy is a conservative connection-local subset, not an ACP rule.
Invalid options, stale/cross-binding handles and already-cancelled caller contexts
do not claim a response or send a native frame.

Permission request IDs retain their native string/integer/null type. Strings
and numbers with the same spelling remain distinct; signed int64 literal IDs
and null are supported. Decimal/exponent numeric spellings are explicitly
unsupported locally, including those that represent integers. Options require
native ID, name and one of the fixed four kinds; absent/null fields and unknown
kinds fail closed. Empty options remain empty, with no fabricated choice.
Duplicate option IDs are an unsupported ambiguous subset. Opaque IDs are not
parsed as commands, paths, modes or principals.

`ID()`, `ToolCall()` and `Options()` return defensive private-data copies. Tool
context may include sensitive titles, paths and raw input/output. Known shallow
ToolCallUpdate fields receive type/container checks; nested content/location
items, tool arguments and extension payloads remain opaque. Accepting them is
not complete ToolCallContent schema validation or verified authorization scope.
The caller owns its current principal/policy checks and presentation/redaction;
this package does not execute or independently approve the described tool.
None of these private payloads is copied into `UpdateFact` or generic errors.

The callback params cap is 64 KiB, with at most 32 options, 512-byte ID strings
and 1024-byte option names. The original 256 KiB frame bound remains in force.
A Peer keeps at most 64 request-ID tombstones and a 64 KiB aggregate private
payload budget for unretired callbacks. These are local client limits. Overflow
fails closed with an explicit gap and actual cleanup evidence; it never drops a
permission and continues as though permission had been granted. Responded or
invalidated payloads are released; caller-owned copies remain the caller's
responsibility. This inbox is not durable approval storage, complete history,
Load/replay, a runtime PendingRequest service or an event stream.

Selections claim once under the Peer mutex. Concurrent opposite answers have
one winner; invalid local selections do not consume the handle. After a claim,
write failure, context expiry or disconnect cannot authorize a resend. The
operation has a ten-second local cap, shortened by the caller. `PermissionReceipt`
uses `local_write` evidence only: writing bytes is not a native ACK, proof that
the option was acted upon, tool execution or a background-process stop.
Uncertain replies close and join the owned Peer with actual
`PermissionReplyError.Process` facts. Already observed local write completion
remains a historical receipt with disconnection separately reported.

Explicit native Prompt cancellation also handles the fixed schema's mandatory
`cancelled` permission outcomes. It latches cancellation under the same mutex,
prevents new selections, joins previously claimed selected writers, then sends
`session/cancel`. Unanswered callbacks, including late arrivals in that local
window, receive only `cancelled` after that notification write. This is a
consequence of the caller's explicit cancellation, never an approval or option
selection. A choice claimed before cancellation may already have been sent;
cancellation does not undo that earlier native decision or attest tool stopping.

Original Prompt terminal/rejection invalidates unanswered handles before it is
published. An original terminal with an unresolved callback is retained with
`ErrPermissionUnresolved` and connection retirement, rather than being rewritten
as an unknown Prompt outcome. Prompt/config singleflight remains held until all
response writers finish. Close and context abort join both permission and cancel
writers without holding the mutex over I/O. Transport abort sends no fabricated
native permission outcome or ACK. Real provider compatibility, persistent
approval handling and the full C08 exit remain unverified.

## Explicit reported configuration selection

`Session.ConfigOptions()` returns only this private Session's bounded defensive
`native_reported` snapshot. NewSession absent/null means unavailable; an advertised
empty array is available but empty. Local Version records receive order, never
server CAS, authentication, effective model/effort, permissions or OS isolation.
Select options support flat choices or grouped choices, nullable UX metadata and
unknown categories. Categories and names never choose a model or grant authority.
The client bounds the encoded option array to 64 KiB, 64 options, 256 total
choices and individual strings to 512/1024/2048 bytes. Duplicate option IDs or
choice values within an option are rejected as an unsupported ambiguous subset.

`SetConfigOption(ctx, id, value)` sends exactly one `session/set_config_option`
for an exact currently advertised ID/value. Missing advertisements and unknown
choices return local `ErrConfigUnavailable` before send; this is a conservative
client subset, not proof of native non-support or R6's full explicit-unverified
configuration path. There is no default, category mapping, automatic selection,
fallback, authentication or retry. Setter and Prompt share singleflight through
decode/writer reaping and one increasing local request-ID space. The setter has
a ten-second client operation cap, shortened by the caller deadline.

Successful response requires a complete non-null configOptions array. Its
`ConfigSelection` keeps the advertisement version used and exact historical
response snapshot. Only the exact ID/currentValue match sets Confirmed; a valid
wrong/missing value returns `ErrConfigMismatch` while retaining the actual report,
without claiming rollback. Known RPC rejection preserves its private object and
normally keeps the Peer; neither rejection nor mismatch proves absence of native
side effects. Sent uncertainty closes with actual `ConfigSelectionError.Process`
proof, without resend. Already observed valid response/rejection survives ctx/EOF
or writer failure as a historical fact plus disconnected/process evidence.

`config_option_update` requires a non-null full array and replaces the snapshot,
not a patch. NewSession, setter responses and notifications are applied only by
the read loop in wire order. A late returning setter cannot overwrite a newer
notification; its receipt remains an older observation. Notification metadata
contains only local ConfigVersion, not private option names/values or raw content,
and uses the existing bounded FIFO/gap policy. It remains Session-level rather
than Prompt-correlated. Snapshot access after failure is diagnostic history,
not an assertion that the report is still effective. Load, explicit mode selection, runtime
composition, automatic tools and real provider compatibility remain unimplemented
or unverified; configuration declarations do not grant fs/terminal/Task authority.
