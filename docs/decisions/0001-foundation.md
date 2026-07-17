# Decision 0001: Foundation

## Status

Accepted

## Decisions

* Keep Kingdom local-only and TUI-only.
* Use one Go module and one binary.
* Separate the executable, application orchestration, and UI into layers.
* Support the full product shape: configurable king, council, and workers; memory; permissioned tools; skills; and topology.
* In v1 topology, discover running endpoints/models and assign roles; defer topology process start/stop management to a future milestone.
* Plan for SQLite persistence, but do not implement it yet.

These constraints keep the first increment easy to run and evolve while leaving clear seams for later capabilities.
