# ADR 0012: Provider-first setup

Status: accepted

## Decision

Setup begins at Providers and supports only Ollama and MLX. Ollama is available on macOS and Linux;
MLX is available only on Apple silicon Macs. Windows is deferred. The persisted configuration records
whether each provider is enabled and its base port. Ollama additionally records either `dedicated`
(the default) or `shared` port mode.

Provider enablement, provider health, and model inventory are separate concepts. Enablement is the
user's durable choice. Health and inventory are observations that can change between scans. Therefore
an enabled provider with no installed models can continue to the model-search stage.

Council is also an explicit persisted choice. When disabled, no Council model is required. When
enabled, setup requires a Council assignment. Version-one configuration is migrated in memory: known
providers are enabled, dedicated Ollama ports are selected, and the former implicit King-as-Council
fallback becomes an explicit Council assignment so an existing ready setup remains ready.

## Why this shape

Inferring configuration from a successful scan made startup state depend on timing and prevented a
new provider from reaching model installation. Typed provider settings keep the two-provider product
easy to read and validate without a speculative plugin framework. A small, pure platform value makes
OS/architecture policy testable without embedding runtime checks throughout the TUI.

The council flag belongs in application configuration rather than topology: topology describes model
routes, while the flag decides whether that orchestration stage exists. This prevents an empty
assignment from carrying the hidden meaning "reuse King."

## Verification

Tests cover macOS/Linux Ollama support, Apple-silicon-only MLX support, deferred Windows support,
provider defaults and port validation, optional-Council readiness, version-one migration, and the
Providers-first workflow. The complete repository check runs formatting, vetting, unit and integration
tests, and a production build.
