# ADR 0004: Always-TUI setup workflow

Status: accepted

Setup is represented by a pure workflow in `internal/setup`; Bubble Tea owns
messages and commands while `internal/ui` only renders. Discovery candidates
are merged deterministically (configured IDs override defaults), and each scan
is guarded by a generation/cancellation gate so stale results cannot replace a
newer rescan. Configuration is written only after review via the injected
atomic save function. Setup navigation can be cancelled before save begins;
once the atomic save starts, all keyboard input is blocked until it completes.
Cancelled or failed setup leaves the original config untouched.
