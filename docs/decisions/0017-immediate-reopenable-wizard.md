# ADR 0017: Immediate and reopenable setup Wizard

Status: accepted

## Decision

Remove the selected-model benchmark from onboarding. After downloads finish, Kingdom applies its
deterministic size-based defaults, chooses the suggested Worker (the smallest selected model) as the
likely fastest setup model, and opens Wizard immediately. It prepares only that runtime in the
background and makes no inference request during entry.

The proposed configuration is ready before the conversational model is ready. The user may apply it
immediately. If runtime preparation fails, Kingdom reports that conversation is unavailable but does
not discard or block the deterministic proposal.

Add `/wizard` as a built-in main-prompt command. It reconstructs the transient setup draft from saved
role assignments, reuses the configured Worker as the setup model, and opens the bounded Wizard
without running normal King orchestration. Esc returns to chat without saving; Apply validates and
atomically saves. Ctrl+S continues to reopen the full Providers and Models flow.

## Why

Sequential cold-model benchmarks made first-run time proportional to every selected model and could
block a valid setup because one optional model returned an empty capability response. Tokens per
second measured during cold onboarding is also noisy and does not justify delaying the user. Model
size is already available and is a sufficient heuristic for choosing a responsive setup assistant.

The deterministic proposal and granular tools remain the reliability boundary for weak models. The
model explains or requests one validated mutation at a time; it is not responsible for constructing
the initial configuration.

## Consequences

Wizard entry is immediate and has constant inference cost: zero model calls. An MLX model may still
need time to load before it can answer questions, but the UI and Apply action remain usable while that
happens. The previous benchmark implementation and screen are deleted.

## Verification

Tests assert that opening Wizard makes no model call, selects the smallest model, prepares only that
runtime, keeps Apply available after preparation failure, intercepts `/wizard` before orchestration,
reconstructs configured roles, and returns to main chat on Esc. Full formatting, vet, race, build, and
fresh-start TUI checks cover the repository.
