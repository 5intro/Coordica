# Experimental Codex app-server transport

This is the first offline C07 slice, pinned against the locally generated
Codex CLI 0.163.0 wire schema recorded in `planning/C01_REVIEW.md`.
It is not yet connected to the production Runner or a public runtime API.
The legacy `codex exec` driver remains in place.

`Open` uses a trusted absolute executable and workspace with an explicit
environment, then sends `initialize` and `initialized`. It does not log in,
select a model, create a thread or start work. `Info` exposes native startup
facts; these do not establish authentication or capability availability.

`Call` correlates an explicit native method with its response. `Events` exposes
structured notifications and server requests. Numeric and string IDs remain
distinct. Callbacks belong to that connection and exact request, and can be
answered once. `Respond`/`Reject` return local `queued` receipts; JSON-RPC has no
generic acknowledgement that a question or approval reply was consumed.

Frames are limited to 256 KiB, pending calls to 32, events to 64 and native
request identity tombstones to 1024 per connection. Exhausted receive capacity
fails the transport and stops its supervised process, rather than silently
dropping a request or reporting success. RPC operations have a ten-second bound.
Timeout/disconnect after queue acceptance returns `ErrOutcomeUnknown`; there is
no automatic replay. `Close` retains actual process-tree stop evidence.

The calling C02–C06 control layer still needs durable Task context/inputs,
explicit Run/instance selection, authorization, binding/turn generation checks,
event/effect persistence and request reconciliation. It must drain the adapter
independently of UI subscribers, redact private native parameters before public
display, and keep Turn completion separate from background-work stop evidence.
This transport does not implement those policies or a new inference/tool loop.
The eventual common API wraps protocol differences while retaining visible
instance/Run identity, progress, artifacts, requests and failures. It must not
hide capability asymmetry or expose unredacted native data as normal UI output.

The offline supervised fixture tests cover two turns on one connection,
resume without replay, steer/interrupt targets, native callback identity,
out-of-order replies, duplicate/conflicting input, disconnects, timeouts and
backpressure. Fixture success does not establish real harness or cross-platform
runtime compatibility. No ordinary test invokes a real model or account.
