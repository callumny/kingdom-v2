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
Use Ctrl+Enter to submit, Esc to cancel a running orchestration, Ctrl+M to
browse memory, Ctrl+S to reopen setup, and Ctrl+C to cancel and quit. Progress
and the final King response are shown in the current chat history.

Completed exchanges are stored locally in `~/.kingdom/memory.db`. Before each run, the King receives
up to six recent exchanges as clearly labelled, untrusted historical context. Press `Ctrl+M` while
idle to browse sessions, use `j`/`k` to move, `r` to reload, and `Esc` to return to chat. Press `d`
and then `y` to permanently delete the selected session (`n` cancels). Memory read/write failures are
reported without discarding an otherwise valid King response.

Press `Ctrl+R` while idle—or from the discovery step—to manage local models. Use `h`/`l` to choose
Ollama, LM Studio, or MLX and `j`/`k` to choose an installed model. Press `s`, review the action, and
press `y` to start it (`n` cancels). Kingdom waits for the loopback endpoint to become ready, then
refreshes its model status. Press `Enter` on a ready model to reopen setup, rescan endpoints, and focus
that model in role assignment.

This version never downloads models, binds a server beyond loopback, or stops a process. Ollama can be
started first and then refreshed to expose its installed models. LM Studio models come from
`lms ls --llm --json --no-launch`; MLX models come only from complete snapshots already in the local
Hugging Face cache and are started with offline mode enforced. Processes intentionally continue after
Kingdom exits.

The King may also request one tool at a time. `list_files`, `read_file`, and literal `search` run
automatically inside the directory from which Kingdom was launched. `write_file`, exact-match
`edit_file`, and `run_command` pause for an explicit decision every time: press `y` to approve,
`n` to deny, or `Esc` to cancel the run. The prompt shows the tool, target or command, risk category,
and complete JSON arguments before anything with side effects runs. Tool requests and results remain
in the in-memory chat transcript.

Press `Ctrl+K` while idle to browse skills. Use `j`/`k` or the arrow keys to move, `Enter` to toggle
a skill for the current session, `r` to reload the library, and `Esc` to return to chat. Kingdom ships
with a small `careful-coder` example and loads user skills from `~/.kingdom/skills`. A skill may be a
flat Markdown file or a directory containing `SKILL.md`:

```markdown
---
name: concise
description: Keep answers brief.
---

Answer in no more than two sentences unless the user asks for detail.
```

Active skills are bounded instruction text supplied only to the King. They do not execute scripts,
bypass tool approval, or change persisted model configuration.
