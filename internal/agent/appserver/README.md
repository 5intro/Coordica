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

## Typed native control slice

`StartThread` and `ResumeThread` require an explicit model, approval policy and
sandbox mode. They use the trusted workspace, with symlinks canonicalized, and
return the native configuration separately. A known model/mode/workspace
mismatch returns `ErrConfiguration` with the observed native thread ID for
reconciliation; it starts no Turn and does not silently choose another model.
This is a deliberately small subset of the pinned native options, not a public
runtime API or a claim that the selected mode enforces OS isolation.

The immutable connection-owned `Thread` supports `Read` and `StartText`;
the returned `Turn` targets `SteerText` and `Interrupt` at that exact native pair.
Resume/read do not submit input, a stale steer is not queued, and interrupt RPC
success is separate from `turn/completed`, background stop or process-tree stop.
Effort is forwarded explicitly; native acceptance is not proof of the model's
actual reasoning effort. Missing/malformed response identity terminates the
connection and reports an unknown outcome instead of returning a usable handle.

```mermaid
flowchart LR
  Caller[Explicit caller] --> Handle[Thread / Turn target]
  Handle --> Peer[Bounded RPC peer]
  Peer --> Native[Native app-server]
  Native --> Events[Native events / requests]
  Events --> Caller
  Peer --> Supervisor[Process stop evidence]
```

The diagram describes adapter control, not completed Task persistence or
authorization. The supervised test child validates wire fields and counts
operations; its hand-written, schema-informed frames are not native recordings.
