# Maintainability review policy

Keep modules cohesive, dependencies clear and changes maintainable. Independent
review uses engineering judgment; numeric measurements are advisory risk signals,
not automatic acceptance or rejection. Owner selected this lightweight policy
on 2026-10-25 instead of expanding a custom multi-language parser.

## Reference signals

| Signal | Reference |
| --- | --- |
| Physical line | 160 Unicode characters |
| Source file | 500 lines |
| Function | 80 lines |
| Class | 200 lines |
| Class / Go receiver | 20 methods |

These numbers prompt a closer look. A cohesive function can reasonably exceed a
reference; a smaller module with unrelated responsibilities can still require
changes. Do not compress statements, rename files or invent forwarding layers
just to satisfy a count. Keep staged cleanup tasks in [AGENT.md](AGENT.md).

## Lightweight implementation

`maintainability-v2-advisory` compares immutable target-main and candidate source
to highlight new or growing signals. All recognized source gets physical file
and line measurements. Go uses the standard library parser for declarations,
closures and package-wide receiver counts. Receiver totals include platform
variants as a rough size signal, not a claim about one build configuration.
Other languages do not receive invented function/class precision. Use existing
project-native lint, type checking, builds, tests and independent code review.

`make quality` and CI print bounded warnings and succeed when only these numeric
signals are present. The center stores a bounded advisory with the fixed
candidate and supplies it to its independent reviewer. A warning cannot approve
a candidate, grant permission, trigger an automatic repair or reject a merge.
Reviewers request changes for material design/maintenance risks with a clear
reason rather than treating reference numbers as universal limits.

## Mandatory integrity and execution checks

Source inventory safety remains strict: immutable object identity, unique paths,
UTF-8 text, 20,000 entries, 2 MiB per source file and 64 MiB total included source.
Go syntax errors remain errors. Malformed baseline source may be fixed/deleted;
unmeasurable declarations grant no inherited measurement. Operational read
failures preserve the candidate for retry. Deterministic source defects retain
fixed repair input and finite cumulative accounting.

These are separate from maintainability judgment. Scope and authorization,
leases/generations, process-stop evidence, fixed candidate/main checks,
protected approvals, independent review and required test/build evidence remain
mandatory. Runtime goal authority is not granted by a quality report.

Fixed artifact conventions exclude `.git`, `node_modules`, `vendor`,
`third_party`, `dist`, `*.generated.ts` and `*.pb.go`. Source ignore comments do
not alter the report. Sparse reads preserve pinned index content for absent
skip-worktree paths. Bounded managed Git reads and isolated Python entrypoints
remain required. No project code or native compiler is executed by the report.

## Verification

```sh
make quality
# Explicit immutable comparison:
go run ./cmd/qualitycheck --base <full-main-sha> --candidate <full-candidate-sha>
```

Also run project-native build/type checks and relevant failure-mode regressions.
An advisory-only exit status does not replace those required checks.
