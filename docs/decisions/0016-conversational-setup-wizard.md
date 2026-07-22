# ADR 0016: Bounded conversational setup Wizard

Status: superseded by ADR 0017

## Decision

The first-run path is Providers → Models → Benchmark → Wizard → Ready. The manual Roles,
Performance, and Review screens are removed from the product path; their setup-flow behavior is
superseded by this decision.

Models displays one installed-first library across enabled Ollama and MLX providers using stable
Provider, Status, and Model columns. A selected online model is downloaded only after confirmation,
and every download must finish before benchmarking begins.

Kingdom benchmarks selected models sequentially with one warm-up and one short structured-action
sample. It uses provider-reported token counts and generation duration when available. The fastest
model that returns the exact requested control action becomes the Wizard. Transport or runtime
failures block progression; if available models answer but none produces the exact action, Kingdom
uses the deterministic Worker suggestion as a fallback. Benchmark results are session-only.

The Wizard applies deterministic defaults before its first response: the largest selected model for
King, the smallest for Worker, and Council disabled unless a distinct third model is available. The
user can accept immediately or request concise changes in plain language.

The model cannot edit configuration directly. It receives a fixed set of strict-JSON, single-purpose
tools:

- inspect and preview the setup;
- enable or disable Council;
- assign one exact selected model name to King, Worker, or Council;
- set Council size and concurrent workers;
- choose separate or shared Ollama servers;
- set an enabled provider's base loopback port; and
- apply the setup after explicit user confirmation.

Apply authorization is single-use and granted by the TUI's Enter action, not by model output. Wizard
tools receive only the in-memory setup draft and an injected save callback. They have no shell,
filesystem, memory, provider installer, skill library, or normal Kingdom tools. A malformed control
response gets one repair attempt, a tool loop is capped at ten calls, and deterministic defaults remain
available when a small model cannot follow the conversational protocol reliably.

MLX uses one server per model. Final runtime endpoints use consecutive ports from the configured MLX
base port. Benchmark MLX servers use isolated temporary loopback ports so a later role or Council
change cannot make an already-running benchmark process occupy a final runtime port. Ollama keeps its
existing separate-by-default or shared-server choice.

## Why this shape

Most users understand a short setup conversation more readily than three configuration forms, but
letting a local model write arbitrary JSON would make weak-model mistakes difficult to constrain and
explain. Small tools turn each decision into a narrow validated operation. Visible model numbers avoid
asking the model to reproduce long identifiers. Deterministic defaults keep setup quick and provide a
safe fallback; the benchmark avoids choosing a fast model that cannot use the control protocol.

The Wizard remains a package beside setup and orchestration rather than becoming another orchestration
role. Setup has different permissions, a short lifetime, stricter output grammar, and a separate trust
boundary. This separation keeps the normal King/Worker/Council engine and its workspace tools unchanged.

## Consequences

Setup now starts selected runtimes before configuration is saved, and detached provider processes may
remain after setup exits. Process shutdown is still deferred. Benchmarking adds a short startup cost,
but it is bounded to two small requests per selected model and gives the user visible progress.

The historical manual setup renderers remain temporarily covered as isolated components but are not
reachable from the current workflow. They can be deleted in a later cleanup once no migration or test
coverage depends on them.

## Verification

Tests cover aligned model columns, download-before-benchmark ordering, runtime preparation, exact
capability checks, fastest-reliable selection, unavailable-model blocking, prepared-endpoint fallback,
strict tool schemas, one-setting mutations, invalid arguments, role defaults, malformed-response
repair, tool-loop bounds, explicit Apply authorization, atomic save, dedicated MLX routing, and the
transition into normal chat. The full repository check includes formatting, vetting, tests, and a
production build.
