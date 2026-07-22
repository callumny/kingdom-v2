# ADR 0019: Background runtime warm-up

Status: accepted

## Decision

After the setup model starts, prepare the complete proposed runtime topology in the background. Start
the setup model on its final planned endpoint so MLX is not loaded twice and dedicated Ollama does not
start a redundant server. If the proposed King uses Ollama, send an empty generation request with a
ten-minute keep-alive hint. MLX needs no separate preload because starting its model-scoped server loads
the model.

Store the ephemeral runtime configuration behind a signature containing only settings that affect a
run. The first prompt reuses a matching result, including preparation still in progress. A Wizard or
manual configuration change cancels stale work and starts a new warm-up. Warm-up failure never blocks
setup; the first prompt retries the normal preparation path.

## Why

Starting providers is not the same as loading their model weights. Deferring the full topology until
the first prompt made a successful setup appear to hang at “Starting local model servers”. Preparing
while the user reviews the proposal overlaps unavoidable local startup work with useful interaction.

Reusing the normal runtime planner keeps ports and role routing identical to a real prompt. Keeping the
result ephemeral preserves the boundary between durable provider/model choices and generated runtime
endpoints.

## Consequences

Applying setup remains immediate, but a prompt submitted before warm-up completes waits on the same work
instead of launching duplicate processes. Large models can still have unavoidable load and inference
latency. Ollama Kings are kept resident for ten minutes after preload; provider memory policy remains
external to Kingdom.

## Verification

Tests cover non-blocking Wizard entry, restart after a draft change, manual-review warm-up, signature
stability across unused discovery endpoints, first-prompt reuse, planned Wizard ports, Ollama preload
payloads, MLX startup without duplicate preload, cancellation, and fallback preparation. Formatting,
vet, unit, integration, race, and build checks run across the repository.
