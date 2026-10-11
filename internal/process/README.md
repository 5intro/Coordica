# Managed commands

Native executables use argv directly. Windows resolves installed `.cmd` / `.bat`
entry points (including npm shims) through the system directory's `cmd.exe`,
queried with `GetSystemDirectoryW`. It never selects the interpreter from
`COMSPEC`, `PATH`, or the command's environment.

The shim invocation uses `/d /s /v:off /c`: AutoRun and delayed expansion are
disabled, and the complete command line uses explicit outer and inner quotes.
The interpreter starts suspended and joins the same Job as native commands before
it can execute. Descendants remain subject to that Job's termination policy.
Installed shim code itself must be trusted, like any installed executable.

This is deliberately limited shim support, not a shell command API:

- Only absolute local drive paths ending in `.cmd` / `.bat` are wrapped.
- Paths and arguments reject quotes, `%`, `!`, `^`, `&`, `|`, `<`, `>`, `(`, `)`,
  invalid UTF-8, and control characters. Arguments ending in `\` are rejected to
  prevent ambiguous forwarding through npm's `%*` to a native child.
- Spaces, Unicode names, and empty arguments are supported with fixed quotes.
- The constructed command line cannot exceed 8191 UTF-16 code units. Batch
  scripts have their own expansion limits as well.
- Unsupported values return `ErrUnsafeBatchCommand`. Move the tool to a supported
  path or select its native `.exe`; automatic fallback to a shell is unavailable.
  For example, `Program Files (x86)` and user paths containing `&` are rejected.
- Model prompts and protocol messages travel over stdin, never the batch command
  line, so shell characters and large prompts do not require command-line encoding.

Pure construction/rejection tests run on every development OS. Windows-only
tests exercise actual batch startup, npm-style `%*` forwarding, large stdin,
exit codes, cancellation of descendants, and suspended Job configuration.
Cross-compilation checks buildability; it does not certify Windows execution.

Primary references:
[Go os/exec Windows command quoting](https://pkg.go.dev/os/exec#Command),
[Microsoft batch startup](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-createprocessa),
[cmd flags and quoting](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/cmd),
[system directory lookup](https://learn.microsoft.com/en-us/windows/win32/api/sysinfoapi/nf-sysinfoapi-getsystemdirectoryw),
[cmd length limit](https://learn.microsoft.com/en-us/troubleshoot/windows-client/shell-experience/command-line-string-limitation).
