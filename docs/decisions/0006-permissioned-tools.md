# ADR 0006: Permissioned local tools

Status: accepted

## Decision

Kingdom exposes six tools: `list_files`, `read_file`, literal `search`, `write_file`, exact-match
`edit_file`, and `run_command`. Only a King action is interpreted as a tool request, and one action can
contain only one call. Read-only tools are automatic. Every write, edit, and command requires a fresh
approval; approvals are neither persisted nor shared between calls. A missing approver is a denial.

The workspace is the process launch directory. File requests are strict JSON and must resolve within
that root without traversing a symlink. Invalid requests are rejected before approval, which means the
user is never asked to approve an operation Kingdom already knows it cannot safely execute. Writes use
a same-directory temporary file, restrictive permissions, and atomic rename. Edits proceed only when
the old literal occurs exactly once.

File reads are capped at 64 KiB, search at 100 matches, directory listings at 1,000 entries, and model
tool context at 24 KiB. Commands have a 30-second timeout and 24 KiB combined output limit. They run
with the workspace as the working directory and a reconstructed environment containing only basic
execution and locale variables. `/bin/sh -c` is used only after the user has seen and approved the
exact command. This approval is a deliberate trust boundary, not a filesystem sandbox; an approved
shell command has the operating-system permissions of Kingdom.

The orchestration-to-TUI approval is a typed, single-use request. The engine emits it and waits on the
run context; the app resolves it once with `y` or `n`. `Esc` cancels the whole run and `Ctrl+C` cancels
then exits. Denial becomes a structured tool result returned to the King, while cancellation ends the
run. Tool IDs cannot be replayed within one run, and enabling tools raises the King-call ceiling from
four to eight so the model has room to request a tool and reason over its result.

## Why this shape

The runner contains validation and side effects, orchestration owns policy and model feedback, and the
Bubble Tea app owns interaction. This separation makes the dangerous boundary testable without a
terminal or model server, keeps UI state deterministic, and allows a different approval surface or
tool implementation to be injected later. A narrow fixed allowlist is easier to explain and secure
than arbitrary plugin execution; skills remain a separate stage.

## Verification

Unit tests cover strict decoding, approval ordering, traversal and symlink rejection, deterministic
limits, cancellation, timeouts, environment reconstruction, atomic permissions, and exact-match edits.
Orchestration tests cover the approval handshake, denial feedback, duplicate IDs, bounded results, and
cancellation. An integration test runs the real tool runner through the engine and verifies an approved
file write on disk.
