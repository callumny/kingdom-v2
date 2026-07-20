# ADR 0010: Royal-minimal onboarding

Status: accepted

## Decision

Kingdom keeps its royal identity through restrained language, a crown mark, and muted parchment,
gold, cyan, green, and red colours. It does not copy the original TUI's animated court, telemetry, or
permanent detail panels. A stable shell provides one header, one focused content area, setup progress,
and a contextual footer. Content is capped at a readable width and centred on wide terminals.

The first-run workflow now begins with explicit Welcome and Providers states. Welcome explains that
models run locally, that up to three may be selected, and that larger models generally trade more RAM
and latency for capability. Discovery continues in the background but cannot skip the explanation.
Providers with models are selected by default; Space toggles an available provider, and only selected
provider results reach subsequent model choices. Provider selection is transient setup state because
the persisted topology should contain assignments, not abandoned onboarding choices.

The setup progress line reserves Providers, Models, Roles, and Review. This increment implements the
first two screens' foundation while the existing role screen temporarily performs individual model
selection. The next increment will insert a dedicated Models state and size-based suggestions without
changing the provider-selection contract.

## Why this shape

The previous renderer styled the complete screen as one title, exposed implementation-oriented
shortcuts, and gave status, navigation, and primary actions equal visual weight. Semantic styles now
express hierarchy without entering application state. A small rendering helper is sufficient; a UI
component framework would add indirection before Kingdom has repeated components that justify it.

Provider selection remains in the Bubble Tea application because it is cursor and session state. The
pure setup workflow owns the ordered Welcome and Providers transitions, while discovery and process
management remain injected asynchronous services. This keeps visual design, interaction state, and
infrastructure responsibilities separate and explainable.

## Verification

Workflow tests cover Welcome/Providers transitions and back navigation. Application tests cover
background discovery, default provider selection, toggling, validation, and downstream filtering.
Rendering tests assert the explanatory copy, progress, provider status, contextual controls, and
terminal width/height bounds rather than comparing brittle ANSI snapshots. The complete interaction
is also exercised against the real local runtime composition.
