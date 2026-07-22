# ADR 0021: Resumable and compactable sessions

Status: accepted

## Decision

Promote the opaque persistence session ID into explicit application state. `/new` generates a fresh ID
and clears the visible transcript. `/sessions` lists saved conversations by their first user prompt,
loads the selected transcript asynchronously, and resumes it with `Enter`. `/memory` remains an
unadvertised compatibility alias.

Persist normalized prompt and completion usage with every successful exchange. The Sessions list shows
cumulative model tokens and an approximate current-context percentage. Because Ollama and MLX do not
provide one context-limit contract through discovery, use a clearly approximate 32k window until model
capabilities become a separate provider concern.

`/compact` summarizes all but the most recent two uncompacted exchanges with the configured King. The
summary preserves decisions, constraints, unresolved tasks, facts, and preferences. SQLite stores the
summary and an exchange-ID boundary; raw exchanges remain available for the transcript and deletion.
Future King prompts receive the summary plus exchanges after that boundary.

## Why

A session browser that cannot resume a conversation is only an archive. Making the ID part of app state
aligns the UI, persistence, and orchestration boundaries: the user chooses the conversation, while the
engine sees an immutable session ID for one run.

Compaction must reduce future context without destroying history. A stored boundary makes that rule
simple to query, test, and explain. Keeping the latest two turns verbatim preserves immediate nuance,
while semantic summarization retains older decisions more effectively than truncation.

## Consequences

Existing version-one databases migrate in place and retain every exchange. Their token totals use a
visible text estimate because past provider usage cannot be reconstructed. New totals include King,
Worker, Council, and compaction calls.

Compaction can take as long as one local King generation and can be cancelled with `Esc`. It is
available from the main command and the Sessions screen. Empty or two-turn sessions are not compacted.

## Verification

Tests cover schema migration, provider usage normalization, session-scoped recall, summary boundaries,
raw-transcript retention, new and resumed session IDs, active-session deletion, stale async results,
wide and narrow rendering, compaction prompts, measured compaction usage, and the complete persistence
integration path.
