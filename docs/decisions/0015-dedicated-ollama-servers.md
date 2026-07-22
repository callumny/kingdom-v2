# ADR 0015: Ephemeral dedicated Ollama server topology

Status: accepted; setup control moved by ADR 0016

## Decision

Kingdom offers an Ollama server-mode control through the setup Wizard when at least one active role
uses the managed Ollama provider. `dedicated` remains the default; `shared` is the alternative.

Dedicated mode assigns one server to each unique selected Ollama model. Ports are consecutive from the
configured Ollama base port, and ordering is deterministic: King, Worker, then Council, with duplicate
models removed. Roles sharing a model share its server. Shared mode routes all managed Ollama models to
the base port. The Wizard's proposed setup shows the mode and base port. The setting is omitted when
no selected model uses managed Ollama.

The generated topology is runtime state, not configuration. Immediately before every prompt,
`internal/config` copies the persisted topology, adds deterministic runtime endpoints, and rewrites the
copied role assignments. The composition root passes the required endpoints to `internal/localmodels`,
which reuses ready servers and starts missing ones before handing the copied config to orchestration.
Generated endpoint IDs and ports are never saved.

Only explicit HTTP loopback endpoints may be started. Processes use the exact `ollama serve` argument
vector and an `OLLAMA_HOST` environment value. Kingdom does not use a shell, does not bind remote
interfaces, and does not stop detached provider processes.

## Why this shape

Users make one understandable performance choice without learning endpoint topology. A server per
model can reduce cross-model request contention when workers and reviewers run concurrently, while
shared mode may consume less memory. This is a trade-off, not a throughput guarantee, so the UI says
what changes rather than promising a speed increase.

Keeping generated routes ephemeral preserves a small, explainable configuration: users save provider,
model, role, and policy choices; Kingdom derives process details. The preparation function also keeps
process startup outside Bubble Tea and outside the orchestration engine. Both layers depend only on
typed functions and event streams.

## Verification

Tests were written first for deterministic routing, shared-mode collapse, port overflow, immutable
saved configuration, conditional TUI controls, exact review output, asynchronous preparation, failure
propagation, loopback validation, readiness reuse, endpoint deduplication, and exact process arguments.
The command-level integration test verifies that planned endpoints reach the server manager and the
rewritten configuration reaches orchestration.
