# ADR 0009: Confirmed local model startup

Status: superseded by ADR 0012 and ADR 0014

## Decision

Kingdom adds a provider-neutral local runtime manager with Ollama, LM Studio, and MLX adapters. The
local-model TUI displays installation, server, and model state. Setup discovery exposes it as the
first-class `m` action, while `Ctrl+R` remains available from the ready screen. When discovery finds no
model, Enter opens local model setup instead of silently remaining on the same screen. Starting a
runtime or loading a model is an explicitly confirmed action. Once the selected model is ready, the
user can hand it to the existing discovery workflow and advance directly to focused role assignment;
runtime management never writes topology configuration.

The first version manages only models already installed on the machine. It does not download models,
accept arbitrary model paths, bind beyond loopback, unload models, or stop processes. A server started
by Kingdom intentionally survives application exit. These constraints keep storage cost, licensing,
network activity, and lifecycle ownership explicit.

## Provider behavior

Ollama is detected through the `ollama` executable. If its loopback API is unavailable, the user may
confirm `ollama serve`; after readiness, the existing `/api/tags` discovery path supplies installed
models. Ollama loads a selected installed model on its first normal chat request.

LM Studio is detected through `lms`. Inspection runs
`lms ls --llm --json --no-launch`, which is both machine-readable and forbidden from implicitly
launching the application. A confirmed start runs a loopback server and loads the exact inventory
model with the same explicit API identifier. Kingdom then requires that identifier to appear at the
local model endpoint before reporting readiness.

MLX is detected through `mlx_lm.server`. Kingdom scans at most 256 Hugging Face cache repositories and
accepts only snapshots containing a configuration and safetensor weights. Starting requires an exact
model from that inventory. `HF_HUB_OFFLINE=1` and the inspected cache path replace any conflicting
child environment values, preventing an absent model from becoming an implicit download. The server
binds only to `127.0.0.1:8080`.

The command choices follow the provider documentation: [Ollama CLI](https://docs.ollama.com/cli),
[LM Studio CLI inventory](https://lmstudio.ai/docs/cli/local-models/ls),
[LM Studio server startup](https://lmstudio.ai/docs/cli/serve/server-start), and
[MLX LM HTTP server](https://github.com/ml-explore/mlx-lm/blob/main/mlx_lm/SERVER.md).

## Process and application boundaries

Adapters receive an injected system interface for executable lookup, bounded command execution, and
detached startup. Arguments are passed directly to the executable without a shell. Long-running
servers inherit neither terminal input nor terminal output and run in a separate process session.
Readiness polling is cancellation-aware and capped at two minutes. Inspection queries providers in
parallel while returning them in a deterministic Ollama, LM Studio, MLX order.

The Bubble Tea application owns selection, confirmation, cancellation, generation guards, and the
transition back into setup. Runtime commands never run inside `Update`. Late inspection/start messages
are ignored after a newer generation, matching the discovery and memory-browser concurrency pattern.

## Verification

Unit tests cover stable status, missing CLIs, malformed inventories, deterministic model ordering,
exact command arguments, missing-model rejection, MLX cache completeness and offline environment,
readiness, cancellation, confirmation, navigation, stale input isolation, error rendering, and model
focus during role assignment. An integration test combines real HTTP discovery with a machine-readable
LM Studio inventory and verifies installed-versus-loaded model state.
