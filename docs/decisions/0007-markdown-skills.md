# ADR 0007: Session-scoped Markdown skills

Status: accepted

## Decision

Skills are passive Markdown instruction packs. Kingdom provides a small built-in skill and loads user
skills from `~/.kingdom/skills` as either flat `.md`/`.markdown` files or directories containing a
`SKILL.md`. Optional frontmatter contains `name` and `description`; the Markdown body is the instruction
text. Skills do not bundle or execute scripts in this stage.

The ready-screen TUI opens the library with `Ctrl+K`. The user can browse, reload, and toggle skills for
the current process session. Activation is unavailable during setup or an active run. A submission
receives an immutable copy of the active list, so changing the TUI later cannot change an in-flight
request. Activation is deliberately not added to configuration: skills describe how to approach a
task, while topology configuration describes which local models perform roles.

Only the King receives active skill instructions. Workers receive their explicit delegated prompt and
Council members receive their review prompt. This preserves a single policy authority and avoids
silently multiplying prompt tokens across concurrent calls. Skills are appended to the King system
prompt beneath an explicit statement that they cannot override the action schema, tool permissions,
or safety limits.

## Bounds and precedence

Each skill file is limited to 64 KiB, at most 256 candidate entries are inspected, and the combined
active prompt is limited to 32 KiB on a UTF-8 boundary. Hidden files, non-Markdown files, symlinks, and
directories without `SKILL.md` are ignored. A malformed file produces a visible load warning without
hiding valid skills.

Names are normalized to lowercase slugs and results are sorted deterministically. A user skill can
override a built-in with the same case-insensitive name. A directory skill takes precedence over a
flat user file, which makes the common `SKILL.md` layout unambiguous.

## Why this shape

Passive instructions keep skills separate from permissioned tools: reading a skill cannot create a
side effect, while every mutation still travels through the Step 5 approval boundary. A small library
interface keeps filesystem discovery out of Bubble Tea, and an orchestration option keeps the engine
independent of the concrete library. This leaves clear future seams for persisted activation,
automatic selection, or separately permissioned executable skills without committing the first
version to those complexities.

## Verification

Unit tests cover parsing, malformed and oversized files, symlink rejection, deterministic precedence,
partial errors, bounded UTF-8 rendering, session toggling, input isolation, and King-only injection.
An integration test loads a real Markdown file, activates it through TUI messages, calls a local
`httptest` Ollama endpoint, and verifies that the resulting King system prompt contains the skill.
