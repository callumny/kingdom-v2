# Kingdom

Kingdom is a local-only terminal application for configuring a king, council, and workers; coordinating memory, permissioned tools, skills, and topology; and presenting that system in a TUI. It discovers already-running Ollama, LM Studio, and MLX-compatible endpoints, lets the user assign models to roles, and atomically saves the reviewed configuration. Starting and stopping model-server processes is deferred to a later milestone.

## Development

Requires Go 1.26 or newer.

```sh
make check
go run ./cmd/kingdom
```

On first run, Kingdom opens setup and scans the default local endpoints. Use arrows or `j`/`k` to
navigate, `Enter` to assign, `n` to continue from role assignment, `r` to rescan, and `a` to add a
custom local endpoint. Press `q` or `Ctrl+C` to exit. While the reviewed configuration is being saved,
keyboard input is briefly blocked until the atomic write succeeds or fails.
