# Architecture

Kingdom is deliberately small and layered:

* `cmd/kingdom` is the composition root: it loads dependencies and starts the application.
* `internal/app` owns Bubble Tea application state without performing filesystem or network setup.
* `internal/config` validates and atomically persists versioned configuration.
* `internal/topology` defines local endpoints and king, council, and worker assignments.
* `internal/discovery` queries Ollama and OpenAI-compatible endpoints and normalizes their models.
* `internal/setup` owns the pure setup workflow, draft configuration, endpoint merging, and
  stale-discovery generation guard.
* `internal/ui` renders presentation without owning domain or infrastructure logic.

Dependencies point inward: the UI receives application state, discovery depends on topology contracts,
and the composition root connects them. Provider-specific HTTP payloads do not escape the discovery
package; callers receive one normalized model type and ordered endpoint results. The setup package
does not import Bubble Tea, perform HTTP requests, or write files. `internal/app` translates key
presses and asynchronous messages into setup transitions, while `cmd/kingdom` injects the concrete
discover and save functions.

The setup path is discovery -> role assignment -> performance -> review -> ready. Discovery clears
old results before a rescan and uses monotonically increasing generations so late responses cannot
replace current state. Role identity is the endpoint ID plus model ID, which distinguishes the same
model name served by different local runtimes. Configuration is not written until review. Once the
atomic save command starts, keyboard input is temporarily blocked so the UI cannot claim to cancel a
filesystem operation already in progress.

The product scope includes configurable king, council, and workers; memory; permissioned tools; skills;
and topology. The current implementation has configuration, topology contracts, model discovery, and
the complete TUI setup/assignment flow. The next stage will orchestrate requests through the assigned
roles. Starting and stopping model-server processes is a future milestone. SQLite is planned for the
later persistence stage.
