# Architecture

Kingdom is deliberately small and layered:

* `cmd/kingdom` is the executable entry point.
* `internal/app` owns application orchestration and Bubble Tea model state.
* `internal/ui` renders presentation and owns UI styling.

The product scope includes configurable king, council, and workers; memory; permissioned tools; skills; and topology. In v1, topology discovers running endpoints/models and assigns roles. Starting and stopping topology processes is a future milestone. SQLite is planned for a later persistence layer, but is not implemented in this foundation.
