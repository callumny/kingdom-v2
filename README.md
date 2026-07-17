# Kingdom

Kingdom is a local-only terminal application for configuring a king, council, and workers; coordinating memory, permissioned tools, skills, and topology; and presenting that system in a TUI. The initial foundation is a single Go module and binary with a minimal Bubble Tea v2 interface. v1 topology discovers running endpoints/models and assigns roles; process start/stop management is deferred.

## Development

Requires Go 1.26 or newer.

```sh
make check
go run ./cmd/kingdom
```

Press `q` or `Ctrl+C` to exit.
