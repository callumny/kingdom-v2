# ADR 0008: Local SQLite conversation memory

Status: accepted

## Decision

Kingdom stores completed user/King exchanges in one local SQLite database at
`~/.kingdom/v2/memory.db`. The database contains versioned `sessions` and `exchanges` tables, with a
foreign key that cascades session deletion. A cryptographically random opaque session ID is created
for each new conversation and may be replaced or resumed without restarting Kingdom. The database file is mode `0600`, newly created parent directories are
mode `0700`, and an unsupported schema version stops startup rather than attempting an unsafe change.

Retrieval is scoped to the active session. Before orchestration starts, Kingdom supplies its compacted
summary followed by uncompacted exchanges in chronological order. User and reply fields are bounded at
write time, and the rendered context block is capped at 96 KiB on a valid UTF-8 boundary, reserving
roughly one quarter of the conservative 32k-token display window for instructions and a response.
Recalled text is labelled untrusted historical data and is supplied only to the King, not to Worker or
Council calls.

Provider prompt and completion counts are normalized by `internal/modelapi` and accumulated across
King, Worker, Council, and compaction calls. Each saved session reports cumulative usage. Version-one
exchanges lack provider counts, so their text receives a visibly approximate fallback estimate.
Context percentage is always marked approximate because the current provider discovery contract does
not expose one reliable context limit across Ollama and MLX.

Only successful terminal results are persisted. Invalid prompts, configuration failures, model
failures, and cancelled runs are not saved. Recall and save failures emit warnings but do not turn a
valid model answer into a failed run. Database opening and migration are stricter because continuing
against an unreadable or unknown schema could damage local data.

The ready-screen TUI opens Sessions with `/sessions`. Session summaries and details are loaded in
commands rather than inside `Update`; generation numbers reject stale asynchronous results. `Enter`
resumes the selected transcript, `n` starts fresh, and `c` compacts all but the most recent two
uncompacted exchanges. Compaction uses the configured King, preserves decisions and unresolved work,
stores a summary boundary, and never deletes the raw transcript. Deleting a session requires `d`
followed by `y`, and uses the database cascade to remove its exchanges.

## Why SQLite

SQLite gives this single-user local product transactions, constraints, deterministic queries, and
future migrations without a server process. Kingdom uses the pure-Go `modernc.org/sqlite` driver so
the application does not introduce a C compiler requirement. The standard `database/sql` boundary
keeps the driver choice inside the memory package.

Semantic embeddings and a vector store are deferred. They would add model selection, indexing,
re-indexing, relevance tuning, and substantially more interview surface before the product has shown
that semantic retrieval is necessary. Recent exchanges provide predictable conversational continuity
with a much smaller operational and conceptual footprint.

## Verification

Unit tests cover private files, version-one migration/version refusal, reopen persistence, ordering,
bounds, UTF-8 safety, cancellation, concurrent saves, previews, usage/context summaries, compaction,
and cascading deletion.
Orchestration tests cover King-only recall, successful final persistence, and graceful memory
failures. TUI tests cover asynchronous loading, responsive rendering, navigation, resume/new flows,
stale-result rejection, input isolation, compaction, confirmation, deletion, and error rendering. An integration test submits two prompts through the TUI
to a local test HTTP model, verifies recall in the second request, and browses both persisted exchanges.
