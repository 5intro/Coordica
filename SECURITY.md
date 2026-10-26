# Execution trust boundary

Coordica runs authorized development on the Owner's trusted private computers.
It is not a service for executing hostile repositories or arbitrary contributors'
code on a credentialed host. Treat repository content and model output as
untrusted proposals; only the existing Owner-authorized project/task flows can
grant work. External issues, PRs and tool capability reports grant no authority.

## What Coordica enforces

- Center APIs authenticate the caller and check persisted role, project/node
  scope, versions and the current authorization inside the application service.
- Runs use separate Git snapshots, fixed inputs, finite execution policies,
  leases/generations and actual process-stop receipts. Unknown old execution
  blocks replacement; late results cannot authorize a newer Run.
- Scope, protected paths, tests, independent review and fixed candidate/main
  checks determine which code may be integrated. A tool's completion text is not
  permission to integrate, publish, expand scope or change project decisions.
- Interaction MCP profiles do not receive Owner or publication credentials.
  Secrets are excluded from prompts, public artifacts and normal state exports.

## What Coordica does not isolate

Git snapshots, a working directory, prompts, permission modes, process groups and
Windows Jobs are not an operating-system security sandbox. Post-execution Git
checks can reject a candidate; they cannot undo reads, writes or network requests
that already happened in the private execution environment.

Installed harnesses and project checks run with their operating-system user's
access. Their own sandbox and permission capabilities vary by tool, version,
platform and local settings. `read_only` reports a registered review mode, not
proof that the entire process is OS-isolated. In particular, an allowlisted Bash
tool is still command execution, and Claude's `dontAsk` is not a sandbox.

Do not assume filesystem scope prevents a model-driven command from reading
Runner registration, Owner tokens, SSH keys, model credentials or other files
accessible to that same OS user. This limit also applies to project test scripts,
hooks and other child processes, not just one harness. Pairing, selecting a model
or passing tests does not establish isolation. The Owner must trust the host,
installed executables and admitted project execution before enabling automation.

## When stronger isolation is required

Run the harness and project checks in a separately provisioned restricted OS
account, container, VM or dedicated Runner machine. Keep center Owner/publication
credentials outside that boundary, minimize the execution identity's privileges
and verify denied-file, denied-network and process-tree behavior in that actual
environment before admitting untrusted code. Coordica does not currently
provision or attest such an environment, and must not describe an unverified
deployment as sandboxed. Never disable a real isolation boundary as a fallback.

Retain the existing authorization and integration gates inside isolated
deployments too; isolation does not approve work or replace code review.

## Reporting a vulnerability

Do not include credentials or raw private execution logs in public issues. Report
the affected version, deployment boundary, minimal reproduction and expected
authorization/identity behavior to the repository owner privately. A defect in
the documented application boundary is distinct from the explicitly unsupported
same-user hostile-code isolation guarantee above.
