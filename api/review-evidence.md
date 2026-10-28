# Review evidence contract

The center owns issue identity, versions and dispositions. Model output is an
attributed claim; it cannot accept risk, enroll a trusted provider, change
commands or authorize development. Local Git mode remains available.

## Internal review v1

New candidates pin the complete applicable `AGENTS.md`, `REVIEW.md`, `QUALITY.md`
and `SECURITY.md` from immutable target main, including ancestor directories of
changed paths. The snapshot contains its base SHA and a SHA-256 content digest.
Changing a rule file marks the candidate protected: the old rules apply to that
review, and Owner approval of the exact candidate is required before integration.
Proposed policy text never grants permissions.

Runners advertise `review_contract_version: 1`. A review assignment includes the
same version, `review_rules_digest`, the candidate's `review_rules`, and applicable
historical issues. Old Runners cannot claim these reviews. Already-prepared
legacy candidates retain their explicit legacy contract; this upgrade does not
retroactively claim that historical reviews used pinned rules.

The review result keeps `candidate`, `verdict`, `summary`, `run_id: ""` and
`findings: []`, and requires:

- `rules_digest`: exactly the assigned digest.
- `issues`: explicit array (at most 32), with stable `key`, `severity` P0..P3,
  `title`, `path`, `line` (0 when unavailable), `trigger`, `evidence`, `impact`.
- `resolutions`: explicit array (at most 64), with `issue_id`,
  `expected_version`, `disposition` (`fixed` or `false_positive`) and current-code
  evidence in `reason`.

Approval cannot contain new issues or omit applicable unresolved issues. A new
commit requires new verification of previous dispositions. Re-reporting a stable
key reopens the same issue; omitting it does not close it. The center rechecks
source authority, exact versions and stop receipts transactionally. Legacy
string findings are retained as unclassified excerpts, not invented locations
or severity. Every accepted review and issue revision is durably retained.

The Owner-only admin-loopback API exposes:

- `GET /api/v1/review-issues?task_id=...&after=...&limit=32`: cursor page, limit 1..64.
- `POST /api/v1/review-issues/{id}/resolve`: `expected_version`, `candidate_id`,
  `commit`, `disposition` (`false_positive`, `accepted_risk`, `superseded`), `reason`.
  Uses the standard idempotency header. An optional `request_recheck: true`
  with `candidate_version` requests another review of the same stopped, rejected
  candidate after all internal issues are disposed. This consumes a finite retry,
  preserves previous counts and verdicts, and leaves the task paused. A running
  replacement, changed authority or exhausted retry allowance rejects the whole
  mutation. Recording a disposition never approves or merges the candidate.

The Web candidate detail provides these operations. External issues cannot be
waived through this API. The limited interaction MCP and Runner credentials do
not acquire Owner issue-disposition authority. History outside the current
authority remains visible in the Owner ledger without being current review proof.
Complete execution context is bounded; more than 64 task issues creates a visible
context-limit hold rather than silently dropping evidence.

## Trusted external results

In GitHub publication settings, Owner may configure `review_providers`:

```json
[{"provider":"codex","actor_id":123,"app_id":456,"kinds":["code_review","security_review"]}]
```

The numbers are placeholders. Obtain the actual comment's `user.id` and
`performed_via_github_app.id` from GitHub before configuring them. Display names,
prose and a model's self-asserted identity cannot enroll a provider. Policy changes
require a paused, quiet project; an empty policy preserves existing GitHub gates
without adding these external gates. It does not assert that external review ran.

The current adapter reads bot comments containing one structured envelope:

```text
<!-- codex-review-state:v1 {"code_review":{"commit_sha":"<full current SHA>","status":"completed","has_any_findings":false,"has_unresolved_findings":false}} -->
```

Each configured kind must have a complete current-head result from the configured
numeric actor and App. The latest applicable update wins. Old-head success,
missing App attribution, malformed/contradictory/ambiguous results, incomplete
pagination and running/failed operations cannot approve a merge. Operational
failure waits for evidence instead of consuming code repairs. Only a completed
result with unresolved findings enters the existing repair flow and updates the
original PR. Provider-cleared findings are recorded as `resolved_external`, not
an invented code fix. The adapter does not scrape human prose for per-line issues
or automatically post review-trigger comments. A bounded, attributed raw comment
excerpt and source URL accompany a failed gate into the existing repair context;
missing private-report details are not invented. Other providers require explicit
protocol adapters; unsupported formats fail closed.

GitHub CI, branch protection, internal review, fixed head/base checks, protected
Owner approval and final candidate revalidation continue to apply independently.
External success does not replace any of them. These are trusted-private-host
controls, not OS isolation or protection against a malicious same-user harness.
