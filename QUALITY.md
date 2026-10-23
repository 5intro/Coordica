# Maintainability acceptance and merge policy

This policy applies to Coordica and to projects developed through Coordica.
Functional correctness, maintainability and fixed-version evidence are separate
required acceptance conditions. A successful test suite or reviewer approval
cannot waive a structural violation.

## Required limits for authored source

| Metric | Limit |
| --- | --- |
| Physical line length | 160 Unicode characters, excluding CR/LF |
| Source file | 500 physical lines |
| Function/method | 80 physical lines |
| Class | 200 physical lines |
| Methods per class / Go receiver | 20; Go methods aggregate across package files |

Limits are upper bounds, not design targets. A shorter component with unrelated
responsibilities is still unacceptable. Review domain boundaries, dependency
direction, ownership and change impact. Keep transactions/audit/idempotency
atomic and Git/process/network I/O outside transactions.

Do not evade limits by compressing statements, renaming, splitting a god
object's methods across files, moving authored code into artifacts, claiming
code is generated or adding a broad exclusion. Use responsibility-based
components and small consumer-owned interfaces. Do not invent a framework or
an interface per struct just to distribute lines.

## Existing projects and gradual cleanup

Existing violations are measured from the actual fixed target-main Git tree.
They are a frozen cleanup backlog, not an editable allowlist. A candidate may
retain or reduce an existing measurement but may not increase it or introduce
a new violation. Edited long lines must be brought within the limit; unchanged
long lines survive line-number shifts, and duplicates consume separate counts.
Renaming a violating file/type does not transfer its exemption.
Malformed Go or invalid text in a frozen baseline can be corrected or deleted.
Candidates must contain valid source; unmeasurable baseline declarations grant
no structural allowance. Inventory bounds and operational failures stay strict.

For new projects there is no inherited debt. For existing projects, Leader
records the affected hotspots and includes bounded cleanup tasks. Coordica's
own staged tasks are in [AGENT.md](AGENT.md).

Changes to these thresholds, protected checks or artifact conventions require a
separate Owner-reviewed policy change. A natural-language request, model
summary, task-local setting or candidate-side config is not such a change.

## Program gates and independent review

1. Initialization and Leader planning include the standing policy and preserve
   existing project instructions. Plans name cohesive modules and acceptance.
2. Developer and independent reviewer receive the same policy for every Run.
   Reviewers request changes for new/worsened debt and semantic god objects.
3. The center reads immutable target-main and candidate blobs without running
   repository code. Structural regressions reject preparation before review.
4. The center rechecks structure immediately before landing, including legacy
   candidates. Missing/invalid/unbounded source inventory fails closed.
5. Local `make quality` and CI use the same implementation. PR CI compares to
   the event's target base; push CI compares to the previous commit. Required
   three-platform verification must succeed before a normal main merge.

Center gates apply to local Git and GitHub publication. A protected-candidate
approval does not skip them. Remote branch protection and mandatory CI must be
retained; privileged manual pushes are not a permitted quality-policy bypass.

## Measurement coverage and exclusions

The built-in policy is versioned as `maintainability-v1`. File/line limits apply
to recognized source extensions, including tests. Go declarations, function
literals and receiver methods use `go/parser` syntax trees. JS/TS, C-family
languages, Rust, Kotlin, Swift, PHP, Ruby, shell, PowerShell and SCSS have
language-specific lexical class/function checks. HTML, Vue and Svelte script
regions use JS/TS checks; inert JSON and markup remain text. Plain CSS has no
authored function/class declarations. Python uses lexical indentation checks.
Lexical checks are not full language parsers or semantic cohesion proofs.
Native language syntax/lint checks and independent structural review remain
required, especially for unsupported syntax, overloads and metaprogramming.
JavaScript/TypeScript signatures use balanced delimiters; expression-bodied
arrows, executable template interpolations, object methods and callable class
fields are included. Function debt also binds to declaration,
binding/call-site and enclosing-scope headers, independently of line numbers.

Fixed artifact conventions exclude `.git`, `node_modules`, `vendor`,
`third_party`, `dist`, `*.generated.ts` and `*.pb.go`. A generated-file comment
does not grant an exemption. Review must reject relocating authored behavior
to these paths to hide it. Repository-supplied ignore rules do not exclude
tracked blobs from center/CI analysis.
Local sparse checkouts read absent skip-worktree content from its pinned index
blob; an actual local deletion remains a deletion. Required Python entrypoints
run in isolated import mode so repository modules cannot replace stdlib imports.

Inventory limits are 20,000 tree entries, 2 MiB per included source file and
64 MiB included source per tree; raw manifests have a separate 96 MiB bound.
Non-UTF-8 source, duplicate inventory paths,
unsupported selected Git objects and malformed Go source are rejected. Large
repositories need an explicit reviewed extension of this bounded reader;
they must not be silently truncated and reported as passed. Immutable blobs
are read in one bounded managed batch per tree, preserving object IDs and
framing without launching a subprocess for each source file.

## Verification commands

```sh
make quality
# Explicit immutable comparison:
go run ./cmd/qualitycheck --base <full-main-sha> --candidate <full-candidate-sha>
```

Keep readability/formatting changes reviewable separately where possible.
Run relevant regressions after extraction. For authorization, recovery,
cancellation or integration changes, cover contention, stale/duplicate
messages, process-stop evidence and fixed-candidate publication.
