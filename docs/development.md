# Development

## Getting Started

### Prerequisites

- Go: Version `1.24.2` or later.
- Mise (Optional): A task runner used to orchestrate build pipelines.
  - If you don't want to use it, check the `mise.toml` for development commands.

## Tasks Reference

### Format Code

Clean imports, formatting, and standard layout settings:

```bash
mise run format
# Or manually:
# goimports -w . && go fmt ./...
```

### Tidy Modules

Downloads missing modules and prunes unused imports:

```bash
mise run tidy
# Or manually:
# go mod tidy
```

### Run Tests

Runs the complete unit testing suite covering parsing, stores, renames, completions, folding, and diagnostics:

```bash
mise run test
# Or manually:
# go test -v ./...
```

### Build Binary

Compiles the executable locally:

```bash
mise run build
```

### Install Globally

Compiles and moves the binary to `~/go/bin/`.

```bash
mise run install
```

### Clean Up

Deletes compiled local binaries and release distribution directories:

```bash
mise run clean
```

## Inspecting

Add this to your init.lua:

```lua
vim.lsp.set_log_level("debug")
-- This opens the log file in a split window
vim.cmd("edit " .. vim.lsp.get_log_path())
```

If you have the nvim plugin installed,
you can use this to print the state index content:

```plaintext
:XsmdDump
```
