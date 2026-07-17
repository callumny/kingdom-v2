# Architecture

Kingdom is deliberately small and layered:

* `cmd/kingdom` is the composition root: it loads dependencies and starts the application.
* `internal/app` owns Bubble Tea application state without performing filesystem or network setup.
* `internal/config` validates and atomically persists versioned configuration.
* `internal/topology` defines local endpoints and king, council, and worker assignments.
* `internal/discovery` queries Ollama and OpenAI-compatible endpoints and normalizes their models.
* `internal/ui` renders presentation without owning domain or infrastructure logic.

Dependencies point inward: the UI receives application state, discovery depends on topology contracts,
and the composition root connects them. Provider-specific HTTP payloads do not escape the discovery
package; callers receive one normalized model type and ordered endpoint results.

The product scope includes configurable king, council, and workers; memory; permissioned tools; skills;
and topology. The current implementation has configuration, topology contracts, and model discovery.
The next stage connects them to the TUI setup flow. Starting and stopping model-server processes is a
future milestone. SQLite is planned for the later persistence stage.
