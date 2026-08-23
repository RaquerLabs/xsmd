# Development

## Getting Started

### Prerequisites

- Go: Version `1.24.2` or later.
- Mise (optional): A task runner that runs the build pipelines.
  - If you do not want to use it, read the `mise.toml` file for the development commands.

## Tasks Reference

### Format Code

Cleans the imports, the formatting, and the standard layout settings:

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

Runs the complete unit test suite. The suite covers parsing, stores, renames, completions, folding, and diagnostics:

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

If you have the nvim plugin installed, you can use this command to print the state index content:

```plaintext
:XsmdDump
```
