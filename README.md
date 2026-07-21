# Kingdom

Kingdom is a local-only terminal application for configuring a king, optional council, and workers; coordinating memory, permissioned tools, skills, and topology; and presenting that system in a TUI. Its supported model providers are Ollama on macOS or Linux and MLX on Apple silicon Macs. Windows support is deferred.

## Development

Requires Go 1.26 or newer.

```sh
make check
go run ./cmd/kingdom
```

On first run, Kingdom starts at Providers and scans in the background. Use arrows and `Space` to enable
Ollama or MLX. Press `i` on an uninstalled provider and explicitly confirm before Kingdom installs it
from its official source. Ollama is then started on its configured loopback port; MLX is installed in
Kingdom's private Python environment and starts after a model is selected. Provider setup shows named
steps and a progress bar. Kingdom does not allow the Models screen to open until every enabled provider
is ready. Continue to Models to see installed Ollama and MLX models in one list. Press `/` to fuzzy
search both enabled providers; installed matches stay first and online matches are marked for download.
Select up to three choices. Kingdom asks for confirmation before downloading anything, then lets role
assignment continue while progress is shown. The role screen suggests the largest selected model for
King and the smallest for Worker. With three choices it suggests the middle model for Council; with
fewer choices Council starts disabled. Every suggestion can be changed before the reviewed
configuration is atomically saved.

When configuration is ready, the chat accepts multiline prompts (32 KiB max).
Use Ctrl+Enter to submit, Esc to cancel a running orchestration, Ctrl+M to
browse memory, Ctrl+S to reopen setup, and Ctrl+C to cancel and quit. Progress
and the final King response are shown in the current chat history.

Completed exchanges are stored locally in `~/.kingdom/v2/memory.db`. Before each run, the King receives
up to six recent exchanges as clearly labelled, untrusted historical context. Press `Ctrl+M` while
idle to browse sessions, use `j`/`k` to move, `r` to reload, and `Esc` to return to chat. Press `d`
and then `y` to permanently delete the selected session (`n` cancels). Memory read/write failures are
reported without discarding an otherwise valid King response.

Press `Ctrl+R` while idle to inspect or start local runtimes as a maintenance tool. Setup itself keeps
the main journey linear: Providers → Models → Roles → Review. Ollama downloads stream progress from
its loopback API. MLX downloads use Kingdom's managed Hugging Face tooling and private cache. Kingdom
does not bind a server beyond loopback or stop provider processes; processes started by Kingdom
intentionally continue after it exits.

The King may also request one tool at a time. `list_files`, `read_file`, and literal `search` run
automatically inside the directory from which Kingdom was launched. `write_file`, exact-match
`edit_file`, and `run_command` pause for an explicit decision every time: press `y` to approve,
`n` to deny, or `Esc` to cancel the run. The prompt shows the tool, target or command, risk category,
and complete JSON arguments before anything with side effects runs. Tool requests and results remain
in the in-memory chat transcript.

Press `Ctrl+K` while idle to browse skills. Use `j`/`k` or the arrow keys to move, `Enter` to toggle
a skill for the current session, `r` to reload the library, and `Esc` to return to chat. Kingdom ships
with a small `careful-coder` example and loads user skills from `~/.kingdom/v2/skills`. A skill may be a
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
