# ADR 0014: Unified installed and remote model library

Status: accepted; post-selection flow amended by ADR 0016

## Decision

The Models screen presents one provider-neutral catalogue for every enabled provider. Provider health,
installed inventory, remote search results, selected models, and download progress remain separate
pieces of state:

- runtime inspection supplies installed Ollama and cached MLX models;
- one fuzzy query searches Ollama and MLX concurrently;
- installed matches sort before online matches and each provider has its own remote result limit;
- a model's identity is its endpoint ID plus model ID;
- selected identities survive search-query and result changes; and
- online models require a separate confirmation before any download begins; and
- installed models require a separate confirmation before provider-specific removal.

Provider-specific behavior stays behind injected interfaces. Ollama downloads through its configured
loopback `/api/pull` endpoint. MLX downloads through the `hf` executable in Kingdom's managed Python
environment into Kingdom's Hugging Face cache. Both emit the same typed progress value to the
application. Downloads remain on Models and must complete before the Wizard opens. A failed
download is visible and blocks progression.

The same Models screen owns uninstalling. `d` acts only on an installed row and opens a confirmation
before any files are removed. Ollama removal uses its configured loopback `/api/delete` endpoint. MLX
removal uses the exact `model/<repository>` cache ID with Kingdom's managed `hf cache rm` command.
Success triggers a fresh inventory scan and reconciles the transient selection; failure preserves the
visible model and reports the provider error.

Setup has one path: Providers → Models → Wizard → Ready. The old `m` and setup-time `Ctrl+R` detour is
removed. `Ctrl+R` remains available from the idle chat screen as a maintenance view for installed
runtime startup and inspection.

## Why this shape

Users choose models, not transport adapters. Combining inventory makes mixed Ollama/MLX configurations
obvious, while retaining endpoint identity prevents same-name collisions. Keeping health, inventory,
search, selection, and download state separate avoids a provider's temporary availability silently
changing durable intent. Explicit confirmation makes storage and network cost visible.

The application coordinates asynchronous work but does not know provider commands or HTTP payloads.
The setup package owns the pure catalogue and selection rules, the model-catalog package owns search
normalization and ordering, local-model adapters own downloads, and the UI only renders normalized
state. Generation IDs and cancellation prevent stale searches or progress events from replacing newer
state.

## Verification

Tests were written before each behavior. They cover combined installed inventory, mixed-provider fuzzy
search, installed-first ordering, stale-search rejection, preserved selection, explicit confirmation,
provider-specific download and removal requests, progress, cancellation, success/failure readiness
gates, removal confirmation and selection reconciliation, removal of legacy setup shortcuts, and the
bounded cursor-following model list. `make check` exercises formatting, vetting, the full unit/integration
suite, and the production build; race tests cover the application, setup, and UI packages.
