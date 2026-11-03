# Experimental ACP initialization transport

This is the first partial C08 slice. It initializes a persistent supervised ACP
connection; it does not implement authentication, Session creation/loading,
prompt/cancel, native callbacks, runtime ports or a provider registration.
The production legacy `agent.executeACP` path is unchanged. That one-shot path
automatically starts a prompt and shuts down afterward, so it is not reused here.

The offline baseline is ACP v1 from the fixed
[v0.11.0 schema](https://github.com/agentclientprotocol/agent-client-protocol/blob/451c3cbc7df2f9bc164c25e25d36fa526003f085/schema/schema.json).
Only `initialize` is sent. There is no Codex `initialized` notification or assumed
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
to the trusted caller. Duplicate/unsolicited responses and callbacks, including
after initialization, close this unsupported connection rather than lose facts
or authorize a tool. There is no automatic retry or arbitrary Call API.

Tests use owned Go JSON-RPC children only. Antigravity is still the planned C08
provider; its release is not installed or executed here. Real authentication,
Session/work compatibility, callbacks and runtime composition remain unverified.
