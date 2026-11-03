# Experimental ACP connection and Session transport

These partial C08 slices initialize a persistent supervised ACP connection and
explicitly create one Session. They do not implement authentication, loading,
prompt/cancel, native request answers, runtime ports or a provider registration.
The production legacy `agent.executeACP` path is unchanged. That one-shot path
automatically starts a prompt and shuts down afterward, so it is not reused here.

The offline baseline is ACP v1 from the fixed
[v0.11.0 schema](https://github.com/agentclientprotocol/agent-client-protocol/blob/451c3cbc7df2f9bc164c25e25d36fa526003f085/schema/schema.json).
`Open` only sends `initialize`. There is no Codex `initialized` notification or assumed
immediate Turn ACK/native Turn ID. The client advertises no filesystem/terminal
callbacks. Future ACP prompt integration must retain its terminal response
semantics rather than reuse the short app-server start-ACK assumption.

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
configuration or isolation attestation. Optional mode/configuration declarations
are not retained or interpreted; only their container types are checked. No
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
ordering and full private content/completion handling remain absent. There is no prompt,
authentication fallback, Task execution, database persistence or automatic retry
across a new Peer. Future runtime composition must persist its own intent and
unknown binding before native I/O, as the existing Codex composition does.

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

This intentionally supports five of the fixed schema's nine update variants.
Plan/available-command/current-mode/config-option updates and non-text content
are valid protocol possibilities but currently unsupported, rather than silently
dropped or called schema-invalid. Notifications preceding a valid session/new
response are unsupported; the first observed Session ID is not guessed to be the
target. This metadata primitive is not lossless output, durable spool, complete
Load/Prompt support, permission handling or runtime composition.
