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
Providers report readiness and every ready provider contributes to the model catalogue. Selection is
performed once, at model level, so choices can span multiple providers without redundant provider
checkboxes. Model selection is transient setup state because the persisted topology should contain
assignments, not abandoned onboarding choices.

The setup progress line contains Providers, Models, Roles, and Review. The dedicated Models state uses
the composite endpoint/model identity already required by topology and limits the selection to three.

## Why this shape

The previous renderer styled the complete screen as one title, exposed implementation-oriented
shortcuts, and gave status, navigation, and primary actions equal visual weight. Semantic styles now
express hierarchy without entering application state. A small rendering helper is sufficient; a UI
component framework would add indirection before Kingdom has repeated components that justify it.

The model cursor remains in the Bubble Tea application, while the selected model references live in
the pure setup draft where limits and rescan reconciliation can be tested without a terminal. Discovery
and process management remain injected asynchronous services. This keeps visual design, interaction
state, and infrastructure responsibilities separate and explainable.

## Verification

Workflow tests cover Welcome/Providers transitions and back navigation. Application tests cover
background discovery, default provider selection, toggling, validation, and downstream filtering.
Rendering tests assert the explanatory copy, progress, provider status, contextual controls, and
terminal width/height bounds rather than comparing brittle ANSI snapshots. The complete interaction
is also exercised against the real local runtime composition.
