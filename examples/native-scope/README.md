# Native scope intent fixture

This source-only fixture exercises an ordinary scope extension for one existing
task. It contains no runtime state, credentials, or service configuration.

The required behavior is precise: an omitted or `undefined` name returns
`Hello, guest!`. String names have Unicode whitespace trimmed and collapsed to
one ASCII space; an empty normalized string uses `guest`, and Unicode letters
are preserved. Every other non-string argument, including `null`, throws
`TypeError`. These rules match the immutable owner verifier.

`goal.json` permits source and test paths. `task.json` initially permits only
`test/**`, so the requested behavior requires the exact additional path
`src/greeting.mjs`. Both are input templates, not ready-to-submit admission
requests. The following steps describe reproduction by an authorized operator;
reading this fixture grants no authority and starts no service.

1. Use a dedicated project containing the baseline `src/`, `test/`, and `tools/`
   files. Its `total_budget_seconds` must be at least 2400 to fit the goal's
   2400-second budget; cumulative spending and reservations must still fit.
   Include `tools/**` in project `protected_paths`, and configure both owner
   checks from `flow.json`. Keep the verifier immutable. Configure actual node
   capabilities and permissions so Codex implements and a separate, fresh
   Claude session performs read-only test review of the fixed candidate.
   Leader and development use native Codex with explicit `gpt-6.1-sol`, with no
   fallback model. Enable system automation with finite planning budget and
   retries to permit ordinary task admission and scope assessment.
2. Create the real requirement with Owner `POST /api/v1/requirements` using
   `title` and `body` that include the full task description. Record its returned
   ID. Add that actual ID to the goal's `requirement_ids`; the Leader's original
   task proposal must reference the same ID. The template deliberately contains
   no invented persisted IDs. Do not POST `task.json` as a replacement for
   Leader admission.
3. Save the goal through Owner `POST /api/v1/planning/save`. Its body is a
   `PlanningSaveRequest`: `expected_version` plus a nested `document` containing
   the goal fields and actual `requirement_ids`. Use 0 only when no goal exists;
   otherwise read the current planning document version from
   `GET /api/v1/state`. For example, from this fixture directory, this command
   emits a complete save body for a new goal after `REQUIREMENT_ID` is set to
   the actual returned ID:

   ```sh
   python3 - 0 "$REQUIREMENT_ID" <<'PY'
   import json
   import sys
   from pathlib import Path
   document = json.loads(Path('goal.json').read_text())
   document['requirement_ids'] = [sys.argv[2]]
   print(json.dumps({'expected_version': int(sys.argv[1]), 'document': document}))
   PY
   ```

   Prepare request bodies outside the Agent's fixed worktree. All Owner writes
   require Owner authentication and an `Idempotency-Key`. Posting raw
   `goal.json` does not satisfy the wrapper or grant admission. After saving,
   authorize the goal once with `POST /api/v1/planning/authorize`, body
   `{"expected_version": N}`, where N is the saved document's returned version.
   Authorization alone does not admit development.
4. Submit the initial Leader evaluation using Owner
   `POST /api/v1/planning/evaluate` (or `planning/messages`), body
   `{"expected_version": N, "text": "..."}` with the current document version.
   Replace the illustrative text with the complete `task.json` contents,
   especially its description, and the actual requirement ID. Explicitly
   request exactly one original task with initial proposal `paths=["test/**"]`,
   the template's finite budget/counters, and no initial source authorization.
   The goal's broader `allowed_roots` are a boundary for later assessment,
   not a request to pre-authorize `src`. Inspect the persisted Leader result
   and admitted task before execution: if it has source authority or multiple
   tasks, this is not the intended fixture run. Development requires actual
   Leader admission and the enabled automation policy, not model prose.
5. The initial native Agent must return its own valid execution intent v1
   `scope_requested` for exactly `src/greeting.mjs`, before any edit.
   `expected-scope-intent.json` is a reference for that output; never submit it
   through an Owner-authored synthetic HTTP scope request. Runner must verify
   the actual process tree stopped and the fixed worktree stayed unchanged,
   persist and deliver the exact Run/generation request, then hand off durable
   stopped/quiescent evidence. Leader assesses the extension of the same task
   only after stop evidence. A new Codex generation implements within
   `test/**` plus exactly `src/greeting.mjs`; cumulative spending, reservations,
   retries, and repairs remain fenced. No second Owner goal authorization or
   manual scope approval substitutes for this automatic ordinary extension.
   Independent Claude review and owner checks evaluate the fixed candidate
   before integration. `flow.json` lists the required runtime observations.

Run the baseline regression with `node --test test/greeting.test.mjs` from this
directory. The immutable owner check, `node tools/verify-acceptance.mjs`, is
expected to fail on the baseline source. It must pass after the authorized
implementation meets the string, omitted/undefined, and TypeError rules above. Both task
scopes exclude `tools/**`; keep that verifier unchanged.

Contractgen and packaging tests validate this public fixture's contract and
inventory; baseline Node tests validate only the initial examples. They do not
certify a native collaboration run. Separately recorded local native acceptance
applies to the actual run and evidence recorded by its operator; copying this
fixture does not reproduce that certification or verify another deployment.
