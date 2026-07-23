# Kingdom

Kingdom is a terminal application for running a small team of local AI models. A **King**
coordinates the response, optional **Council** models review it, and **Workers** handle tasks
concurrently. Model inference, conversations, skills, and configuration stay on your machine.

Kingdom supports:

- **Ollama** on macOS and Linux.
- **MLX** on Apple silicon Macs.
- Mixed-provider teams, such as an MLX King with Ollama Workers.
- Resumable sessions, compacted context, Markdown skills, and permissioned workspace tools.

Windows support is not currently available.

## Requirements

- macOS or Linux.
- [Go 1.26](https://go.dev/doc/install) or newer.
- Git, if installing from the repository.
- An internet connection when installing a provider or downloading a model.

You do not need to configure Ollama or MLX before starting. Kingdom can install and configure either
provider from inside the TUI after asking for confirmation.

## Install

Clone the repository and install the `kingdom` command:

```sh
git clone https://github.com/callumny/kingdom-v2.git
cd kingdom-v2
go install ./cmd/kingdom
```

Make sure Go's binary directory is on your `PATH`. You can also run the installed binary directly:

```sh
"$(go env GOPATH)/bin/kingdom"
```

To run Kingdom without installing it:

```sh
go run ./cmd/kingdom
```

Kingdom treats the directory it is launched from as its workspace. Start it inside the project you
want the King to read or modify:

```sh
cd /path/to/your/project
kingdom
```

Configuration and local application data are stored under `~/.kingdom/v2`.

## First-time setup

Setup follows one short path: **Providers → Models → Wizard → Ready**.

### 1. Choose providers

Kingdom checks which local providers are available. Enable Ollama, MLX, or both. If a selected
provider is missing, press `i` and confirm the installation. You cannot continue until every enabled
provider is ready.

<!-- Screenshot: docs/images/setup-providers.png -->

### 2. Choose models

Installed Ollama and MLX models appear together and are prioritised in the list. Press `/` to search
both enabled providers, then select up to three models with `Space`. Missing models are downloaded
only after confirmation.

During a download, Kingdom shows the active model, queue position, downloaded and total size,
transfer speed, progress, and estimated time remaining. When every model is ready, Kingdom opens the
Wizard.

<!-- Screenshot: docs/images/setup-models.png -->

### 3. Finish with the Wizard

The Wizard uses the smallest selected model for a fast setup conversation and proposes sensible
defaults:

- **King** — plans the work, may use tools, and produces the final response. A larger model is
  usually the best choice.
- **Council** — optionally reviews Worker output before the King answers.
- **Workers** — run delegated tasks concurrently. Smaller, faster models work well here.

Ask the Wizard to change a role assignment, enable or disable the Council, adjust Council size or
concurrent Workers, change ports, or choose shared versus separate Ollama servers. Press `Enter` to
apply the proposal and launch Kingdom. Press `Tab` at any time to use the model-free manual setup.

<!-- Screenshot: docs/images/setup-wizard.png -->

## Main prompt

The main screen keeps the conversation on the left and live model activity on the right. Each model
shows its assigned roles, current state, and measured tokens per second after it has generated text.
Write a prompt and press `Ctrl+Enter` to send it; press `Esc` to cancel a running response.

<!-- Screenshot: docs/images/main-prompt.png -->

### Commands

| Command | Purpose |
| --- | --- |
| `/setup` | Reopen the Wizard and change the current configuration. |
| `/models` | Return to model selection. |
| `/sessions` | Browse and resume saved conversations. |
| `/new` | Start a new conversation. |
| `/compact` | Summarise older context while preserving the latest turns. |
| `/skills` | Enable or disable Markdown skills for this session. |

Sessions are stored in `~/.kingdom/v2/memory.db`. User skills are loaded from
`~/.kingdom/v2/skills`; Kingdom also includes a small built-in `careful-coder` example.

### Workspace tools

Only the King can request tools. File tools are restricted to the directory from which Kingdom was
launched.

| Tool | Behaviour |
| --- | --- |
| `list_files` | Lists files inside the workspace automatically. |
| `read_file` | Reads a workspace file automatically. |
| `search` | Searches workspace text automatically. |
| `write_file` | Pauses and asks for approval before writing. |
| `edit_file` | Pauses and asks for approval before an exact-match edit. |
| `run_command` | Shows the complete command and pauses for approval. |

For an approval request, press `y` to allow it, `n` to deny it, or `Esc` to cancel the complete run.
An approved shell command has the same operating-system permissions as the Kingdom process.

## Local data and networking

Model inference and conversation history remain local. Kingdom uses the network only when it needs
to search for models, install a provider, or download a model. Provider servers bind to loopback
addresses, and generated runtime endpoints are not written into the saved configuration.

## Development

Run the complete formatting, vetting, test, and build checks:

```sh
make check
go test -race ./...
```

The architectural overview is in [docs/architecture.md](docs/architecture.md), with individual
decisions recorded under [docs/decisions](docs/decisions).

## License

Kingdom is available under the [MIT License](LICENSE).
