# ADR 0018: Deterministic Wizard controls and manual override

Status: accepted

## Decision

Keep natural-language interpretation in the local setup model, but keep configuration mutation,
validation, and user-facing change confirmations in Go. Wizard requests use provider-native JSON mode
when available. Explicit requests are not reported as complete until their corresponding bounded tool
has succeeded. Role swaps use one atomic `swap_roles` tool rather than two independent assignments.

After a successful tool call, Kingdom describes the current draft instead of displaying an unverified
model success message. If structured output cannot be interpreted, the Wizard says so and points to the
visible proposal as the source of truth.

Add a model-free Manual setup path from Wizard with `Tab`. It reuses the selected model pool and the
existing role, performance, review, validation, and save flow. `x` swaps King and Worker directly.

## Why

Small local models can omit one part of a request, emit prose instead of the required JSON, or repeat a
generic defaults message. Prompt changes alone cannot make those probabilistic outputs authoritative.
An atomic domain operation reduces the reasoning required for a common swap, while deterministic
confirmations prevent the UI from claiming changes that are absent from the draft.

Manual setup ensures model quality is never a prerequisite for completing configuration. Reusing the
same draft and validation code avoids a second configuration implementation.

## Consequences

The model remains useful for concise natural-language setup, but it is an intent translator rather than
the source of truth. The manual path adds no alternate persistence format or validation rules. Provider
JSON mode is used only for Wizard control responses; normal Kingdom chat remains unrestricted text.

## Verification

Tests cover provider JSON payloads, atomic role swaps, required assignment tools, state-derived
confirmations, honest malformed-response fallback, manual entry and return, manual swapping, performance
changes, validation, and saving. The full suite also runs with the Go race detector.
