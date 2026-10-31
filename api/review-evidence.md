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
Historical issue provenance uses immutable source review receipts and their
source identity, not the candidate object's mutable current commit or rules.
Rebuilding the same candidate ID therefore requires fresh issue verification;
missing or inconsistent receipts cannot silently waive an issue. Older receipts
remain readable with their surviving candidate identity and authority binding.
For older issue records, the receipt's pinned rules digest is authoritative;
later dispositions also refresh the issue's rules metadata.

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
[{"provider":"codex","actor_id":123,"app_id":456,"kinds":["code_review","security_review"],"check_names":{"code_review":"Actual code-review check name","security_review":"Actual security-review check name"}}]
```

The numbers and check names are placeholders. Each check must represent a complete
review verdict for that kind, not merely a scan-completed notification. The source
App must publish the Check Run on the fixed head, and branch protection must require
that exact check name pinned to the same App ID. A deletable comment or legacy
commit status alone cannot approve integration. If the provider does not publish
such a check, use GitHub for manual review; this automatic gate remains unavailable.

The configured identities are explicit. Obtain the actual comment's `user.id` and
`performed_via_github_app.id` from GitHub before configuring them. Display names,
prose and a model's self-asserted identity cannot enroll a provider. Policy changes
require a paused, quiet project; an empty policy preserves existing GitHub gates
without adding these external gates. It does not assert that external review ran.

The current adapter reads bot comments containing one structured envelope:

```text
<!-- codex-review-state:v1 {"code_review":{"commit_sha":"<full current SHA>","status":"completed","has_any_findings":false,"has_unresolved_findings":false}} -->
```

Each configured kind must have both a complete current-head structured result from
the configured numeric actor/App and a matching App-owned Check Run. Both verdicts
must agree. A failing/timed-out check without actionable structured findings, or
contradictory sources, remains pending instead of consuming a code repair. The
latest applicable comment update supplies context; it never grants authority alone.
An explicit null result remains pending for that kind without invalidating a
complete sibling result in the same envelope. Enabling both kinds still requires
both to pass; a newer incomplete result replaces that kind's earlier success.
Old-head success, missing App attribution, malformed/contradictory/ambiguous results, incomplete
pagination and running/failed operations cannot approve a merge. Operational
failure waits for evidence instead of consuming code repairs. Only a completed
result with unresolved findings enters the existing repair flow and updates the
original PR. Provider-cleared findings are recorded as `resolved_external`, not
an invented code fix. The adapter does not scrape human prose for per-line issues
or automatically post review-trigger comments. A bounded, attributed raw comment
excerpt and source URL accompany a failed gate into the existing repair context;
missing private-report details are not invented. Other providers require explicit
protocol adapters; unsupported formats fail closed.

The center persists independent Check Run and comment-version receipts per task,
fixed head, enrolled provider and review kind. Lower check IDs and older comment
updates cannot replace newer observations, even when API reads span different
snapshots. Once a completed check regresses or disappears, that ID cannot
resurrect its former approval; a newer App-owned Check Run is required. An older
or disappeared terminal report requires a fresh comment update. Pending checks
may complete normally. Receipts survive restart and backup/restore, and are stored with audit
in the same transaction before a remote status can be reported. A writer deleting
a new comment before the first Center read still cannot hide the App-owned check.
Legacy comment-only policies and receipts never inherit approval.

The branch-protected `coordica/review` status stays pending until the configured
external gates and internal review pass. Already merged PRs still fetch external
evidence for the fixed head. External merges and lost-response reconciliation
cannot mark a task integrated or release its resources without that evidence;
waiting does not spend retry/repair counts or submit a second merge. Observing an
unauthorized external write does not undo it or turn it into an approval.

GitHub CI, branch protection, internal review, fixed head/base checks, protected
Owner approval and final candidate revalidation continue to apply independently.
External success does not replace any of them. These are trusted-private-host
controls, not OS isolation or protection against a malicious same-user harness.
