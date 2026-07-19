# ADR 0008: Local SQLite conversation memory

Status: accepted

## Decision

Kingdom stores completed user/King exchanges in one local SQLite database at
`~/.kingdom/memory.db`. The database contains versioned `sessions` and `exchanges` tables, with a
foreign key that cascades session deletion. A cryptographically random opaque session ID is created
for each application process. The database file is mode `0600`, newly created parent directories are
mode `0700`, and an unsupported schema version stops startup rather than attempting an unsafe change.

The first retrieval strategy is deliberately recency based. Before orchestration starts, up to six
recent exchanges are selected newest-first and returned to the King in chronological order. User and
reply fields are bounded at write time, and the rendered recall block is capped at 24 KiB on a valid
UTF-8 boundary. Recalled text is labelled untrusted historical data and is supplied only to the King,
not to Worker or Council calls.

Only successful terminal results are persisted. Invalid prompts, configuration failures, model
failures, and cancelled runs are not saved. Recall and save failures emit warnings but do not turn a
valid model answer into a failed run. Database opening and migration are stricter because continuing
against an unreadable or unknown schema could damage local data.

The ready-screen TUI opens a memory browser with `Ctrl+M`. Session summaries and details are loaded in
commands rather than inside `Update`; generation numbers reject stale asynchronous results. Deleting a
session requires `d` followed by `y`, and uses the database cascade to remove its exchanges.

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

Unit tests cover private files, migration/version refusal, reopen persistence, ordering, bounds,
UTF-8 safety, cancellation, concurrent saves, session summaries, and cascading deletion.
Orchestration tests cover King-only recall, successful final persistence, and graceful memory
failures. TUI tests cover asynchronous loading, navigation, stale-result rejection, input isolation,
confirmation, deletion, and error rendering. An integration test submits two prompts through the TUI
to a local test HTTP model, verifies recall in the second request, and browses both persisted exchanges.
