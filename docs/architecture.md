# Architecture

Kingdom is deliberately small and layered:

* `cmd/kingdom` is the composition root: it loads dependencies and starts the application.
* `internal/app` owns Bubble Tea application state without performing filesystem or network setup.
* `internal/config` validates and atomically persists versioned configuration.
* `internal/topology` defines local endpoints and king, council, and worker assignments.
* `internal/discovery` queries Ollama and OpenAI-compatible endpoints and normalizes their models.
* `internal/modelcatalog` searches remote provider catalogues and merges installed-first results.
* `internal/modelapi` translates normalized chat messages into provider-specific local HTTP requests.
* `internal/localmodels` inspects and starts installed Ollama and MLX runtimes.
* `internal/memory` owns the versioned SQLite schema and bounded conversation persistence.
* `internal/orchestration` coordinates the bounded King, Worker, and Council request lifecycle.
* `internal/tools` validates and executes the six permissioned workspace tools behind a typed approval
  boundary.
* `internal/setup` owns the pure setup workflow, draft configuration, endpoint merging, and
  stale-discovery generation guard.
* `internal/skills` parses, discovers, orders, and bounds reusable Markdown instruction packs.
* `internal/ui` renders presentation without owning domain or infrastructure logic. Its small semantic
  theme maps meaning to colour and its shell owns responsive framing, while application state remains
  unaware of terminal styling.
* `internal/wizard` owns the strict conversational control loop and the
  bounded tools that may mutate only an in-memory setup draft.

Version 2 keeps its configuration, skills, and memory under `~/.kingdom/v2`. This prevents the strict
v2 configuration loader from reading or overwriting files created by the original Kingdom CLI.

Dependencies point inward: the UI receives application state, discovery depends on topology contracts,
and the composition root connects them. Provider-specific HTTP payloads do not escape the discovery
package; callers receive one normalized model type and ordered endpoint results. The setup package
does not import Bubble Tea, perform HTTP requests, or write files. `internal/app` translates key
presses and asynchronous messages into setup transitions, while `cmd/kingdom` injects the concrete
discover, save, runtime-preparation, and orchestration functions.

Runtime topology is derived immediately before a prompt. `internal/config` creates a deep-enough copy
of the saved topology, allocates deterministic consecutive loopback ports for unique managed models,
and rewrites only the copied role assignments. Ollama may use one port per model or a shared port; MLX
always uses one server per model. `cmd/kingdom` asks `internal/localmodels` to ensure those endpoints are
ready, then passes the copied configuration to orchestration. The TUI sees typed progress events and
does not block. Persisted topology remains provider/model oriented and never contains generated process
endpoints.

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

Skills are passive Markdown rather than executable plugins. The library accepts flat Markdown files
and `<name>/SKILL.md` directories, ignores symlinks, bounds individual files and the combined prompt,
and loads valid entries even when another file is malformed. User entries override built-ins by name;
directory skills override flat files deterministically. The TUI owns session activation and passes an
immutable snapshot into each run. Orchestration renders that snapshot only into the King's system
prompt, so Worker and Council role contracts remain narrow. Skill instructions cannot alter the
action schema, permission checks, or safety limits.

Memory is a local, single-file SQLite database accessed through `database/sql`. The infrastructure
package owns schema migration, permissions, queries, and size limits; orchestration depends only on a
two-method recall/save interface, and the TUI depends on a separate browse/delete interface. This
keeps SQL out of both the workflow and presentation layers. One session ID is generated per Kingdom
process. Successful terminal responses are stored as user/King exchange pairs; failed or cancelled
runs are not stored.

Before a run, the engine loads up to six recent exchanges across sessions in chronological order and
injects them only into King calls. The block is explicitly labelled untrusted historical data so it
cannot override system, skill, action-schema, or tool-permission instructions. Retrieval is recency
based rather than semantic in this version: it is deterministic, dependency-light, and easy to
explain, with a 24 KiB prompt ceiling. Database failures produce warning events while orchestration
continues. Store initialization and unsupported schema versions fail at startup because silently
using an unknown database format would risk corrupting user data.

The Ctrl+M browser loads session summaries and selected exchanges asynchronously. Each request carries
a monotonically increasing generation, so a late result cannot replace the user's current selection.
Session deletion cascades to its exchanges and requires an explicit confirmation in the TUI.

Local runtime management extends discovery rather than replacing it. `internal/localmodels` owns a
provider-neutral manager and two adapters. The adapters use an injected system boundary for
executable lookup and argument-vector commands, and reuse `internal/discovery` to decide whether each
loopback endpoint is ready. Inspection runs the providers concurrently but preserves their
stable display order. The TUI sees only normalized runtime/model status and calls one confirmed
start-and-wait operation.

Ollama starts its long-running server and exposes installed models through its local API. Before a
prompt, the manager probes each planned Ollama endpoint, reuses healthy servers, and starts each missing
unique host/port once. It accepts only explicit HTTP loopback addresses and invokes `ollama serve` as
an argument vector with `OLLAMA_HOST`; it never constructs a shell command. MLX scans bounded Hugging
Face cache entries for complete
snapshots and launches only an exact cached repository ID with offline mode and the cache path forced
into the child environment. Long-running processes receive no terminal input/output, enter a detached
process session, and intentionally survive Kingdom. All commands bypass a shell, command output is
bounded, startup is cancellable, and readiness is capped at two minutes.

After readiness, the Ctrl+R local-model screen remains an idle-screen maintenance tool. It refreshes
normalized runtime status and can start an installed runtime after confirmation. Setup does not branch
into this screen: its single path is Providers → Models → Wizard → Ready. No runtime adapter
writes topology configuration directly. Arbitrary model paths, remote binds, unloading, and process
shutdown remain outside the product scope.

Provider enablement is a persisted user choice; installation/running health and discovered models are
transient runtime facts. Keeping these separate allows a selected provider with no installed models to
progress to remote search. A model is identified by endpoint ID plus model ID, so up to three choices
can span providers without collisions. Selection is transient; persisted topology contains only role
assignments and referenced provider endpoints. Discovery and search use generation numbers so late
asynchronous results cannot replace newer state. The Models page combines installed inventory across
enabled providers, aligns Provider/Status/Model columns, and uses one fuzzy query for Ollama and MLX.
Installed fuzzy matches remain first and online results remain visibly marked for download.

Selecting an online result never starts network activity. Models are downloaded only after a separate
confirmation. Ollama uses its configured loopback pull API; MLX uses the managed runtime's Hugging Face
CLI and private cache. Downloads report typed progress events, remain on Models, and must finish before
the Wizard opens. The setup draft marks a model installed only after its provider adapter reports success.

The Wizard opens synchronously with deterministic defaults and selects the suggested Worker—the
smallest selected model—as the likely fastest conversational model. It prepares that model on its final
runtime endpoint and then warms the full proposed topology in the background. Ollama receives an empty
generation request for the proposed King, while MLX loads each model as its server starts. No model
inference runs during entry, so the user can inspect or apply the proposal immediately. A signature of
runtime-relevant settings prevents stale warm state from being reused after a Wizard or manual change.
The first prompt consumes a matching prepared configuration and otherwise falls back to normal runtime
preparation.

`internal/wizard` is intentionally not a general agent framework. It applies deterministic size-based
defaults before the conversation and accepts exactly one JSON message or tool action per model turn.
Its fixed tools change one setup concern at a time: roles, Council, concurrency, provider ports, and
Ollama server mode. Role assignment accepts an exact selected model name and an optional provider. Tools
hold no shell, filesystem, memory, installer, or normal orchestration capability. Apply authorization
is single-use and is granted only by the user's Enter action; successful validation and atomic save
move the app to Ready.

Typing `/wizard` in normal chat rebuilds the transient draft from persisted role assignments and opens
the same bounded Wizard directly. It does not enter normal orchestration or require provider/model
selection again. Esc returns to chat without saving; Apply validates and atomically replaces the
configuration. Ctrl+S remains the separate route for the full Providers and Models journey.

Provider installation is a separate, injected capability and cannot run until the Providers screen
receives a `y` confirmation. Ollama's official script is downloaded to a private temporary file and
executed without interpolating user input; macOS and Linux are allowed. MLX requires macOS/arm64 and
is installed into `~/.kingdom/v2/runtimes/mlx`, avoiding changes to the user's global Python packages.
Configured ports are applied to discovery as well as startup, so the health check observes the same
loopback service that Kingdom launched.

MLX installation prefers Python 3.10 or newer by explicit minor version, recreates a failed managed
environment with `venv --clear`, upgrades pip/setuptools/wheel, and only then resolves MLX-LM. Command
failures retain bounded diagnostic output instead of exposing only an exit status. Installer progress
is streamed back into Bubble Tea as typed events, producing a named step and determinate bar without
allowing infrastructure goroutines to mutate TUI state. Provider intent and readiness remain distinct:
all enabled providers must be ready before setup can enter Models. Ollama readiness means installed
and running; MLX readiness means its managed runtime is installed because its server is model-scoped.

For defaults, setup sorts selected models by normalized parameter metadata, then a parameter hint in
the model ID, and finally local file size. The largest choice is suggested for King, the smallest for
Worker, and a third choice for Council. With fewer than three choices Council starts disabled. Council
enablement is explicit: when disabled orchestration skips that stage; when enabled an assignment is
required. The Wizard exposes these as editable defaults rather than policy.

The product scope includes configurable king, council, and workers; memory; permissioned tools; skills;
and topology. The current implementation has configuration, topology contracts, installed and remote
model discovery, confirmed model downloads, the complete TUI setup/Wizard flow, local model API
adapters, King-led orchestration, permissioned tools, Markdown skills, persistent conversation memory,
local model startup, and a minimal chat screen. Model-server shutdown remains a future milestone.
