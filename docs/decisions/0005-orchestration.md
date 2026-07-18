# ADR 0005: Bounded local orchestration and minimal chat

Status: accepted

The orchestration engine consumes a validated configuration snapshot and a
provider-neutral chat client. The King returns either a final response or a
strict JSON delegation plan. Worker tasks run with configured bounded
concurrency, Council reviews run in deterministic slots, and the King receives
their ordered outcomes for synthesis. Council falls back to the King assignment
when no Council model is configured.

The engine allows at most four King calls and 32 tasks in one delegation. A
malformed action receives one repair attempt; a second malformed response is
shown as a marked raw fallback. Provider transport failures remain failures.
HTTP calls are local-only and non-streaming, with bounded responses, disabled
redirects, context cancellation, and one paced retry for transient failures.

The ready screen is a focused multiline chat backed by an injected
`RunFunc`. The app owns transient run state, cancellation, generation guards,
and concise progress rendering; orchestration and HTTP remain in their
respective packages. Each submission receives a value snapshot of the latest
saved config, so setup changes cannot leak into an active run. Ctrl+Enter
submits, Esc cancels a run, Ctrl+S reopens setup while idle, and Ctrl+C
cancels then exits. Ordinary letters (including `q`) are prompt text on the
ready screen. In setup, `q` quits ordinary screens but remains text inside the
focused custom-endpoint form. Chat history is intentionally in-memory until
the memory stage is introduced.
