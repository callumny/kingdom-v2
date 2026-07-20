# ADR 0013: Confirmed provider installation

Status: accepted

## Decision

Provider installation is available only from the Providers screen and requires a separate `i`, then
`y`, interaction. Installation is injected into the Bubble Tea application; the installer itself has
no concept of implicit confirmation.

Ollama uses its official installer script on macOS and Linux. Kingdom downloads the script to a
private temporary path, executes that exact file with `sh`, removes it afterward, and starts Ollama on
the configured loopback port. MLX is supported only on Apple silicon and is installed with
`python3 -m venv` under Kingdom's runtime directory followed by `python -m pip install --upgrade mlx-lm`.
Kingdom's MLX adapter checks that managed executable before searching `PATH`.

## Why this shape

The visible confirmation keeps network downloads and package installation at an explicit trust
boundary. A managed virtual environment prevents dependency conflicts and makes Kingdom's MLX setup
repeatable. Installation, process startup, and provider inspection remain separate interfaces so each
can be tested without a terminal, network, or real model runtime.

## Verification

Tests first failed against the absent lifecycle. They now verify the confirmation gate and
cancellation, exact argument-vector commands, official Ollama URL, temporary script execution,
managed MLX environment, platform rejection, loopback port configuration, and application state after
installation. `make check` covers the full repository and production build.
