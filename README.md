# Kingdom

Kingdom is a local-only terminal application for configuring a king, council, and workers; coordinating memory, permissioned tools, skills, and topology; and presenting that system in a TUI. It discovers already-running Ollama, LM Studio, and MLX-compatible endpoints, lets the user assign models to roles, and atomically saves the reviewed configuration. Starting and stopping model-server processes is deferred to a later milestone.

## Development

Requires Go 1.26 or newer.

```sh
make check
go run ./cmd/kingdom
```

On first run, Kingdom opens setup and scans the default local endpoints. Use arrows or `j`/`k` to
navigate, `Enter` to assign, `n` to continue from role assignment, `r` to rescan, and `a` to add a
custom local endpoint. Press `q` on ordinary setup screens or `Ctrl+C` to exit; inside the custom
endpoint form, `q` is normal text. While the reviewed configuration is being saved, keyboard input is
briefly blocked until the atomic write succeeds or fails.

When configuration is ready, the chat accepts multiline prompts (32 KiB max).
Use Ctrl+Enter to submit, Esc to cancel a running orchestration, Ctrl+S to
reopen setup, and Ctrl+C to cancel and quit. Progress and the final King
response are shown in in-memory chat history.

The King may also request one tool at a time. `list_files`, `read_file`, and literal `search` run
automatically inside the directory from which Kingdom was launched. `write_file`, exact-match
`edit_file`, and `run_command` pause for an explicit decision every time: press `y` to approve,
`n` to deny, or `Esc` to cancel the run. The prompt shows the tool, target or command, risk category,
and complete JSON arguments before anything with side effects runs. Tool requests and results remain
in the in-memory chat transcript.
