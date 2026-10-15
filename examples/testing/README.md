# Project testing

Test suites are versioned Owner configuration. `stage` is `merge`, `project`,
or `release`. Categories such as `black-box`, `white-box`, `smoke`, `regression`,
`golden-journey`, `e2e` and `longevity` are overlapping labels, not separate
schedulers. Test-authoring work uses ordinary scoped development tasks and the
same independent code-review/integration pipeline.

The current executor runs commands on the **center host** in an isolated
checkout. It does not send executable commands to paired Runner machines.
Declare `platforms` explicitly when a suite requires a particular OS. Other
platforms remain unverified; cross-compilation does not count as execution.

## Configure and execute

Use the local Owner API through `coordica ctl`. Keep the token file private.
All POST operations require a stable idempotency key. Example suite input:

```json
{
  "expected_version": 0,
  "suite": {
    "id": "project-regression",
    "name": "Project regression",
    "description": "Run the project's Go regression suite",
    "stage": "project",
    "enabled": true,
    "categories": ["white-box", "regression"],
    "match_paths": [],
    "platforms": [],
    "requirement_ids": [],
    "checks": [
      {"name": "go-tests", "command": "go", "args": ["test", "./..."], "timeout_seconds": 3600}
    ]
  }
}
```

Save with `POST tests/suites`. Update with the exact current suite version.
Changing a merge suite requires a paused project and confirmed process
quiescence. It invalidates pending candidate checks and review approval.

Declare generated directories in optional `output_paths`, for example
`["web/node_modules/**", "web/*.tsbuildinfo"]`. Source must be completely clean
before checks start. After checks, only declared untracked/ignored outputs may
remain; tracked code, index changes and undeclared files still fail verification.
The declared paths are part of the pinned suite/candidate evidence. Development
Agents must remove their own generated caches before handing code to Runner.

Request `POST tests/run` with `suite_id`, `expected_version`, a full fixed
`commit`, `budget_seconds`, `max_run_seconds`, `max_retries` (0..2), and optional
`requirement_ids`. This explicitly admits a finite verification task under the
existing approved baseline/goal. A paused project queues it without execution.
Task pause/cancel/resume and stop-confirmation use the existing task/Run APIs.

`GET state` includes `test_suites`, verification tasks and Run `test_report`.
Reports record command exit status, bounded output, platform, suite version,
code version and process-tree stop proof. Failed checks create task blocks;
repair work is a normal development task associated with the failing Run.
Writing tests and executing them do not recursively generate each other.

`GET tests/readiness?commit=<full-commit>` reports required, passed, missing and
failed project/release suite-platform pairs for that exact version. No configured
suite, a pending/cancelled execution, a changed suite or another code version
cannot produce a green release result. Uncovered requirements are reported
separately; suite association is not a claim of complete behavioral coverage.
Release tooling should require `ready == true` before publication. This API
does not itself publish a release or authorize an external deployment.

Set `block_merges_on_failure: true` for critical project/release suites. A
failure then blocks candidate publication until the same suite passes on the
actual main or candidate version. Passing unrelated code, cancellation and a
pending rerun do not clear the fence. Changes to this setting remain explicit
Owner policy edits. Failed tasks/reports retain their original versions;
resolved historical tasks may be archived using the existing cancel action.

## Merge checks

The baseline `project.checks` always run. Enabled `merge` suites add checks:
matching changed paths select relevant suites; unknown impact or protected
changes select all merge suites. Long project/release suites do not run on
every task. Path selection is a conservative routing aid, not dependency
analysis; shared contracts must be included in the project's suite patterns.

The fixed merge candidate passes its selected program checks **before** Agent
code review. Approval reuses those results without another identical run.
Mainline changes rebuild the candidate; merge-test policy changes invalidate
its evidence. A late/cancelled/unknown process cannot validate a new execution.
The center's latest test-policy version is checked again before publication.

Long checks have explicit finite timeouts (up to the existing seven-day Run
limit) and execute in a separate worker from candidate integration. A restart
fences unknown old processes until stop proof is available; it never infers
quiescence from the center disappearing.
