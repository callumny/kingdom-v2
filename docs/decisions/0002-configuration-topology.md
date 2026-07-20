# ADR 0002: Configuration and topology

Status: accepted

Configuration schema version 1 keeps endpoint definitions separate from role assignments. Validation is structural (including a locality policy allowing loopback, private/link-local IPs and named `.local` hosts); readiness is a separate state and never probes connectivity, so an offline machine can still be configured. King and Worker assignments are required for readiness, while Council falls back to King when omitted. Defaults (council size 3, worker concurrency 4) remain setup-incomplete until assignments are supplied.

The application composition root loads configuration and injects the resulting value into the pure `app.New(config.Config)` constructor. This keeps UI construction deterministic and separates wiring from domain validation.

Config files use strict JSON and atomic replacement (same-directory temporary file, sync, close, rename) with restrictive permissions. Validation occurs before any write, preserving a previously valid file when a save is rejected. Missing files return the in-memory defaults; malformed files return actionable errors and are never rewritten.

Version 2 stores its state under `~/.kingdom/v2`, separate from the original CLI's incompatible
`~/.kingdom/config.json`. Isolation preserves the original data and avoids hiding schema mistakes
behind permissive legacy parsing.

Newly created configuration directories are mode `0700` and files are mode `0600`; existing directories are not chmodded. Same-directory rename is atomic on POSIX filesystems. Platforms that do not permit replacing an existing destination with rename may return an error; the prior file is preserved and no post-rename chmod is attempted.
