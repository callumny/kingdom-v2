# Architecture

Kingdom is deliberately small and layered:

* `cmd/kingdom` is the composition root: it loads dependencies and starts the application.
* `internal/app` owns Bubble Tea application state without performing filesystem or network setup.
* `internal/config` validates and atomically persists versioned configuration.
* `internal/topology` defines local endpoints and king, council, and worker assignments.
* `internal/discovery` queries Ollama and OpenAI-compatible endpoints and normalizes their models.
* `internal/modelapi` translates normalized chat messages into provider-specific local HTTP requests.
* `internal/orchestration` coordinates the bounded King, Worker, and Council request lifecycle.
* `internal/tools` validates and executes the six permissioned workspace tools behind a typed approval
  boundary.
* `internal/setup` owns the pure setup workflow, draft configuration, endpoint merging, and
  stale-discovery generation guard.
* `internal/ui` renders presentation without owning domain or infrastructure logic.

Dependencies point inward: the UI receives application state, discovery depends on topology contracts,
and the composition root connects them. Provider-specific HTTP payloads do not escape the discovery
package; callers receive one normalized model type and ordered endpoint results. The setup package
does not import Bubble Tea, perform HTTP requests, or write files. `internal/app` translates key
presses and asynchronous messages into setup transitions, while `cmd/kingdom` injects the concrete
discover, save, and orchestration functions.

The model API supports the two topology endpoint kinds without exposing their wire formats: Ollama
uses `/api/chat`, while OpenAI-compatible runtimes use `/chat/completions`. Requests are non-streaming
in this stage. Endpoints are revalidated as local before every request, response bodies are bounded,
redirects are disabled, and the single retry is limited to transient failures with a cancellation-aware
delay.

The orchestration engine is independent of Bubble Tea. The King can return a final response, a small
JSON delegation plan, or one typed tool call. Worker tasks execute concurrently up to the configured
limit, Council reviews execute in deterministic slots, and the King synthesizes their ordered
outcomes. Runs without tools allow four King calls; tool-enabled runs allow eight because a tool call
and its follow-up consume separate model turns. A delegation remains limited to 32 tasks. Only the
King can produce an interpreted tool action; Worker and Council output is always treated as text.

Tool execution is dependency-injected into orchestration. Read-only tools run automatically, while
write, edit, and command operations send a single-use approval request through the event stream and
wait for the TUI's decision. The app owns no filesystem or process code: it renders the exact request,
resolves it once, and resumes consuming events. Results—including denials—are bounded and returned to
the King as structured context. Repeated call IDs are rejected within a run.

The launch directory is the workspace root. File tools reject traversal, absolute paths outside the
root, and symlink escapes. Reads, searches, directory walks, command duration, and returned model
context all have fixed limits. Writes use same-directory atomic replacement and mode `0600`; edits
require exactly one literal match. Commands run from the workspace through `/bin/sh -c` only after
approval, with a 30-second timeout, bounded combined output, and a small reconstructed environment.
The shell is deliberately an approval boundary rather than an OS sandbox: approving a command grants
that command the permissions of the Kingdom process. This is why commands are never auto-approved.

The setup path is discovery -> role assignment -> performance -> review -> ready. Discovery clears
old results before a rescan and uses monotonically increasing generations so late responses cannot
replace current state. Role identity is the endpoint ID plus model ID, which distinguishes the same
model name served by different local runtimes. Configuration is not written until review. Once the
atomic save command starts, keyboard input is temporarily blocked so the UI cannot claim to cancel a
filesystem operation already in progress.

The product scope includes configurable king, council, and workers; memory; permissioned tools; skills;
and topology. The current implementation has configuration, topology contracts, model discovery, the
complete TUI setup/assignment flow, local model API adapters, King-led orchestration, permissioned
tools, and a minimal chat screen. Skills are the next stage. Starting and stopping model-server
processes is a future milestone. SQLite is planned for the later persistence stage.
