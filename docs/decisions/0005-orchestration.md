# ADR 0005: Bounded local orchestration and minimal chat

Status: accepted

The orchestration engine consumes a validated configuration snapshot and a
provider-neutral chat client. Ordinary King prose is a final response; JSON is
reserved for explicit delegation and tool actions. Worker tasks run with configured bounded
concurrency, Council reviews run in deterministic slots, and the King receives
their ordered outcomes for synthesis. Council falls back to the King assignment
when no Council model is configured.

The engine allows at most four King calls and 32 tasks in one delegation. A
response that is not a valid action is returned directly instead of spending a
second generation trying to repair normal prose. Provider transport failures and empty responses remain failures.
HTTP calls are local-only and non-streaming, with bounded responses, disabled
redirects, context cancellation, and one paced retry for transient failures.

The ready screen is a responsive multiline chat backed by an injected
`RunFunc`. The app owns transient run state, cancellation, generation guards,
concise progress rendering, and accumulated per-model throughput; orchestration and HTTP remain in their
respective packages. Each submission receives a value snapshot of the latest
saved config, so setup changes cannot leak into an active run. `Ctrl+Enter`
submits, `Esc` cancels a run, and `Ctrl+C` cancels then exits. `/setup`, `/models`, `/memory`, and
`/skills` are the visible navigation commands. Ordinary letters (including `q`) are prompt text on the
ready screen. In setup, `q` quits ordinary screens but remains text inside the
focused custom-endpoint form.
