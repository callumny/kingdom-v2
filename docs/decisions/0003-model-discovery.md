# ADR 0003: Model discovery adapters

Discovery uses small provider adapters for Ollama (`/api/tags`) and OpenAI-compatible (`/models`, preserving any nested base prefix). Endpoint validation happens before requests; unsupported kinds produce an endpoint-local error and no request. Payloads normalize IDs (trimmed, blank ignored, exact duplicate first-wins) and metadata, then sort deterministically case-insensitively with an exact-ID tie-break.

Each input endpoint produces a `Result` in input order. Provider failures remain attached to that endpoint so partial availability is preserved (including the same model ID from multiple endpoints). A parent cancellation returns ordered partial results and the parent context error; in-flight requests inherit cancellation and waiting work is not detached.

Requests use a fixed worker pool bounded by `MaxConcurrency`, per-request timeout, and a max response-body limit. Any 2xx status is accepted; non-2xx errors include status and a bounded, valid-UTF-8, single-line sanitized snippet without leaking bodies. Bodies are always closed.

Discovery never follows HTTP redirects. Only the configured and validated local URL may be contacted;
allowing a local service to redirect would bypass the locality boundary. Error status labels are built
from the numeric status code and Go's trusted status table rather than an untrusted HTTP reason phrase.

The local test suite uses race-enabled `httptest` servers and custom transports to cover paths, methods,
normalization, partial failures, cancellation, limits, malformed or oversized responses, redirect
rejection, and body closure. Provider installations are intentionally excluded from automated tests.
