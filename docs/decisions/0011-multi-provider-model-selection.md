# ADR 0011: Multi-provider model selection

Status: superseded by ADR 0012

## Decision

Provider setup reports availability but does not filter the user's choices. Every ready local endpoint
contributes to a single model catalogue. A model reference is the composite of endpoint ID and model
ID because model names are not globally unique. The user may select one to three references across any
combination of Ollama, LM Studio, MLX, and custom local endpoints.

The catalogue and selected references live in the pure setup draft. Bubble Tea owns only cursor state
and key handling. Rescans reconcile selected references against the new catalogue, and role assignment
can only use the selected pool. The saved topology format is unchanged: each role already stores the
same endpoint/model pair, and persistence retains every endpoint referenced by a role.

When selected models have comparable metadata, Kingdom suggests the largest for King, the smallest for
Worker, and the middle choice for Council. With fewer than three choices, Council uses King. These are
editable defaults. Valid existing assignments are preserved when setup is reopened or revisited.

## Why this shape

Provider checkboxes created two layers of selection and made a mixed-provider setup appear unsupported.
Treating providers as availability and models as the only selection makes the user journey match the
domain: providers supply models, users choose models, and roles consume those choices.

The implementation does not add a general provider-capability framework yet. The current MLX adapter
serves one loaded model from its endpoint, so live discovery already exposes the enforceable choice.
Adding unused capability fields would create abstractions without current behavior. Separate MLX
instances or multiple active models can introduce that contract when their lifecycle is implemented.

## Verification

Pure tests cover composite identity, stable catalogue order, the three-model limit, rescan
reconciliation, role suggestions, and preservation of valid manual assignments. Application tests
select models from Ollama and MLX in the same journey. The cross-provider integration test saves and
reloads configuration with MLX assigned to King and Ollama assigned to Worker, then verifies both
referenced endpoints were persisted.
