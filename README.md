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
from its official source. Provider setup shows named steps and a progress bar, and Models remains
locked until every enabled provider is ready.

Models combines installed Ollama and MLX models in aligned Provider, Status, and Model columns. Press
`/` for one fuzzy search across every enabled provider; installed matches rank first and missing models
are marked Download. Select up to three. Downloads require confirmation and finish on the Models page
before setup continues.

Kingdom opens the Wizard immediately and uses the smallest selected model as the likely fastest setup
model. Only that runtime is prepared, in the background; setup never waits for benchmark calls across
every selection. The Wizard first applies deterministic defaults: larger for King, smaller for Worker,
Council disabled unless three models were selected, and conservative concurrency. Ask for a specific
change in plain language or press Enter to Apply & launch immediately, even if the conversational model
is still starting.

The Wizard can only call small setup tools: inspect or preview the draft, assign a numbered selected
model to a role, swap two role models, enable Council, set Council size, set concurrent workers, choose
shared or separate Ollama servers, set provider base ports, and apply after explicit user confirmation.
It requests JSON output from the provider, while Kingdom reports successful changes from the resulting
Go configuration rather than trusting model-written claims. It cannot access the shell, files, memory,
provider installation, or Kingdom's normal workspace tools. Configuration is validated and atomically
saved only by Apply & launch.

Press `Tab` from the Wizard for a model-free Manual setup path. Assign selected models directly, press
`x` to swap King and Worker, adjust Council members and concurrent workers, review the complete proposal,
and save it through the same validation boundary.

When configuration is ready, the chat accepts multiline prompts (32 KiB max).
Use Ctrl+Enter to submit, Esc to cancel a running orchestration, Ctrl+M to
browse memory, Ctrl+S to reopen full setup, and Ctrl+C to cancel and quit. Enter `/wizard` as a prompt
to reopen the conversational setup directly with the saved configuration. Progress
and the final King response are shown in the current chat history.

Before each prompt, Kingdom derives an in-memory runtime topology from the saved model assignments.
In separate Ollama mode, each unique active Ollama model is routed to its own consecutive loopback port;
shared mode uses one base port. Each unique active MLX model always receives its own consecutive
loopback port. Roles sharing a model share its server. Kingdom reuses healthy servers and starts missing
ones. Generated runtime endpoints are never written to the configuration file.

Completed exchanges are stored locally in `~/.kingdom/v2/memory.db`. Before each run, the King receives
up to six recent exchanges as clearly labelled, untrusted historical context. Press `Ctrl+M` while
idle to browse sessions, use `j`/`k` to move, `r` to reload, and `Esc` to return to chat. Press `d`
and then `y` to permanently delete the selected session (`n` cancels). Memory read/write failures are
reported without discarding an otherwise valid King response.

Press `Ctrl+R` while idle to inspect or start local runtimes as a maintenance tool. Setup itself keeps
the main journey linear: Providers → Models → Wizard → Ready. Ollama downloads stream
progress from its loopback API. MLX downloads use Kingdom's managed Hugging Face tooling and private cache. Kingdom
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
