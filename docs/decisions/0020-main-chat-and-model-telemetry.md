# ADR 0020: Focused main chat and observed model telemetry

Status: accepted

## Decision

Keep the ready screen centred on conversation and expose four prompt commands: `/setup`, `/models`,
`/memory`, and `/skills`. The wide layout places deduplicated model activity beside the conversation;
the narrow layout stacks it below. Royal styling identifies the product but does not compete with the
user's prompt and response.

The model API returns normalized completion tokens and generation duration. Orchestration emits a
typed activity event after each successful King, Worker, or Council generation. The app aggregates
tokens and duration by provider kind and model, then the presentation layer renders the resulting
weighted tokens per second. Models used by several roles appear once with all roles listed.

Ordinary King text completes immediately. JSON remains available for delegation and tools, but normal
answers do not enter a repair loop merely because they are prose.

## Why

The main screen should make the primary task—asking local models—obvious. Visible slash commands are
easier to discover and explain than a large shortcut bar. Measurements from real work avoid a startup
benchmark and make the statistic honest: it describes this session on this machine.

Accepting prose also removes an unnecessary second inference from the common path. It improves latency
and compatibility with smaller local models without weakening validation of actions that Kingdom does
interpret.

## Consequences

An unused model has no speed yet and displays `— tok/s`. OpenAI-compatible providers may supply token
counts while Kingdom measures elapsed generation time; Ollama supplies both values directly. The
metric is session-only and is not a hardware benchmark.

Keyboard shortcuts retained internally are not advertised on the main screen. `/models` returns to
chat on Esc, while `/setup` opens the existing bounded Wizard rather than creating another setup path.

## Verification

Tests cover command routing, direct model-library return, responsive activity rendering, role
deduplication, a one-call prose response, normalized throughput events, accumulated sidebar speed, and
the existing structured delegation and tool paths. The repository formatting, vet, unit, integration,
race, and build checks run before release.
