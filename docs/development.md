# Development & Contributing Guide

This document describes how to set up, build, test, and contribute to the `gcgb-md-lsp` codebase.

## Getting Started

### Prerequisites

- **Go**: Version `1.24.2` or later.
- **Mise** (Optional): A task runner used to orchestrate build pipelines.

## Tasks Reference

We use a `mise.toml` task configuration. Below are the available development commands:

### 1. Format Code

Clean imports, formatting, and standard layout settings:

```bash
mise run format
# Or manually:
# goimports -w . && go fmt ./...
```

### 2. Tidy Modules

Downloads missing modules and prunes unused imports:

```bash
mise run tidy
# Or manually:
# go mod tidy
```

### 3. Run Tests

Runs the complete unit testing suite covering parsing, stores, renames, completions, folding, and diagnostics:

```bash
mise run test
# Or manually:
# go test -v ./...
```

### 4. Build Binary

Compiles the executable locally:

```bash
mise run build
# Or manually:
# go build -o gcgb-md ./cmd/gcgb-md-lsp
```

### 5. Install Globally

Compiles and moves the binary to `~/go/bin/gcgb-md`, making it immediately accessible to Neovim configurations in your path:

```bash
mise run install
# Or manually:
# go build -o ~/go/bin/gcgb-md ./cmd/gcgb-md-lsp
```

### 6. Clean Up

Deletes compiled local binaries and release distribution directories:

```bash
mise run clean
# Or manually:
# rm -f gcgb-md && rm -rf dist/
```

## Contributing Workflow

1.  **Fork & Clone**: Pull the repository down onto your workstation.
2.  **Make Changes**: Write modular code under `internal/` or entry points in `cmd/`.
3.  **Write Tests**: Always supplement new functions with corresponding unit tests inside `_test.go` files (e.g. `handlers_test.go`).
4.  **Format & Test**: Run formatting and testing suites locally to confirm code hygiene:
    ```bash
    mise run format && mise run test
    ```
5.  **Submit a PR**: Open a Pull Request on GitHub detailing your adjustments.
