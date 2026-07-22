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
are marked Download. Select up to three. Highlight an installed model and press `d` to uninstall it
after an explicit confirmation; Kingdom removes it from the current selection and refreshes the combined
inventory. Downloads require confirmation and finish on the Models page before setup continues.

Kingdom opens the Wizard immediately and uses the smallest selected model as the likely fastest setup
model. It starts that model on its final planned endpoint, then prepares the complete proposed runtime
in the background while setup remains interactive. Kingdom preloads every active Ollama model without
generating text; MLX models load as their servers start. The Wizard first applies deterministic
defaults: larger for King, smaller for Worker, Council disabled unless three models were selected, and
conservative concurrency. Ask for a specific change in plain language or press Enter to Apply & launch
immediately. The first prompt waits for and reuses any matching background preparation instead of
starting the topology again.

The Wizard can only call small setup tools: inspect or preview the draft, assign an exact selected model
to a role, swap two role models, enable Council, set Council size, set concurrent workers, choose
shared or separate Ollama servers, set provider base ports, and apply after explicit user confirmation.
It requests JSON output from the provider, while Kingdom reports successful changes from the resulting
Go configuration rather than trusting model-written claims. It cannot access the shell, files, memory,
provider installation, or Kingdom's normal workspace tools. Configuration is validated and atomically
saved only by Apply & launch.

Press `Tab` from the Wizard for a model-free Manual setup path. Assign selected models directly, press
`x` to swap King and Worker, adjust Council members and concurrent workers, review the complete proposal,
and save it through the same validation boundary.

When configuration is ready, the chat accepts multiline prompts (32 KiB max). Use `Ctrl+Enter` to
submit and `Esc` to cancel a running orchestration. Four visible prompt commands keep navigation
small: `/setup` reopens the Wizard, `/models` opens the combined model library, `/sessions` browses
and resumes saved conversations, and `/skills` manages session skills. `/new` starts a clean session;
`/compact` summarizes older context while retaining the most recent two turns verbatim. `/wizard` and
`/memory` remain compatibility aliases. `Ctrl+C` cancels and quits.

The main screen keeps the conversation on the left and deduplicated model activity on the right,
stacking them on narrower terminals. Each model shows its assigned roles, current activity, and the
weighted tokens-per-second observed from real responses in this session. Kingdom does not benchmark
models before chat, so an unused model displays `— tok/s` until it generates text.

Before each prompt, Kingdom derives an in-memory runtime topology from the saved model assignments.
In separate Ollama mode, each unique active Ollama model is routed to its own consecutive loopback port;
shared mode uses one base port. Each unique active MLX model always receives its own consecutive
loopback port. Roles sharing a model share its server. Kingdom reuses healthy servers and starts missing
ones. If an MLX port belongs to another process, Kingdom selects the next free loopback port and uses
that endpoint for the current process. Generated runtime endpoints are never written to the
configuration file.
During setup, the same preparation runs speculatively against the proposed configuration. A Wizard or
manual change cancels stale work and prepares the new proposal; failures remain non-blocking and are
retried by the first prompt.

Completed exchanges are stored locally in `~/.kingdom/v2/memory.db`. Each run receives only the active
session's compacted summary and uncompacted exchanges, clearly labelled as untrusted historical data.
Enter `/sessions` while idle to see a one-line preview, turn count, cumulative model tokens, and an
estimated share of a conservative 32k context window. Use `j`/`k` to move, `Enter` to resume, `n` to
start fresh, `c` to compact, `r` to reload, and `Esc` to return. Press `d` and then `y` to permanently
delete the selected session (`n` cancels). Raw exchanges remain stored after compaction. Historical
sessions created before token accounting are marked with `~` estimates. Memory read/write failures are
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

Enter `/skills` while idle to browse skills. Use `j`/`k` or the arrow keys to move, `Enter` to toggle
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
