# gcgb-md-lsp

A lightning-fast, workspace-aware Language Server Protocol (LSP) server designed specifically for Markdown documentation vaults and personal wikis, written natively in Go.

It balances speed and low memory usage, integrating directly with Neovim's modern built-in LSP client to provide seamless cross-note navigation and structural tools.

## Features Implemented

- **Workspace Crawling:**
  Scans your entire vault concurrently on boot, locating the project root via the anchor file `gcgb-md.toml`.
- **Go to Definition (`gd`):**
  Jumps instantly to the absolute file path of any root-relative standard Markdown link under your cursor.
- **Find References (`gD` / `gr`):**
  Inverts your links in memory to instantly gather every backlink across your workspace pointing to your active file.
- **Smart Folding (`za` / `zM`):**
  Walk the Goldmark AST to fold text cleanly under `# Headings`, `## Subheadings`, and nested lists (`-` or `*`).
- **Human-Readable Autocomplete (`[`):**
  Caches the primary `# H1 Title` of every note in the directory. Filtering out unformatted files, typing `[` pops open a floating menu of note names, automatically injecting a valid `[Title Text](path/to/note.md)` snippet.
- **Inline Link Renaming (LSP Rename):**
  Triggering your LSP rename shortcut on any Markdown link pre-fills the exact file path. Submitting the new name physically moves the file on disk and automatically patches every backlink across your entire vault.
- **Workspace File Operations:**
  Move or rename files natively in your Neovim file tree (like `oil.nvim` or `neo-tree`). The server intercepts the filesystem event to automatically fix any broken links pointing to the old path.

## How It Works Under the Hood

The server communicates with Neovim using standard input/output (`stdin`/`stdout`) over JSON-RPC.

```text
       ┌───────────┐      JSON-RPC (stdio)      ┌─────────────┐
       │  Neovim   │ ────────────────────────>  │   Go Core   │
       │ (Buffers) │ <────────────────────────  │ (LSP Server)│
       └───────────┘                            └─────────────┘
                                                       │
                                        ┌──────────────┴──────────────┐
                                        ▼                             ▼
                                ┌──────────────┐              ┌──────────────┐
                                │ In-Memory    │              │   Goldmark   │
                                │ State Index  │              │  AST Parser  │
                                └──────────────┘              └──────────────┘
```


## Documentation

To learn more about the code structure and logic of the LSP server, check out these guides:

- [Architecture Guide](docs/architecture.md) — Visual dependencies, modules map, and thread-safety concurrency locks.
- [Execution Flows](docs/flows.md) — Boot-time crawl indexing loops, real-time diagnostics triggers, and character coordinate parsing.
- [Development & Contributing](docs/development.md) — Compiling locally, formatting files, running tests, and git contributions.


## Developer Quick Start

### 1. Build and Run

Compile the server binary locally:

```bash
go build -o gcgb-md ./cmd/gcgb-md-lsp
```

Launch the compiled binary directly (it will listen on standard inputs/outputs):

```bash
./gcgb-md
```

To install it globally to your path (`~/go/bin/gcgb-md`) so Neovim can launch it:

```bash
go build -o ~/go/bin/gcgb-md ./cmd/gcgb-md-lsp
# Or if using mise:
mise run install
```

### 2. Run Tests

Execute the complete test suite (includes diagnostics, folding, renames, completions, definitions, and references):

```bash
go test -v ./...
# Or if using mise:
mise run test
```

### 3. How to Contribute

1. Fork the repo and make your adjustments in the Go code.
2. Format your files using `go fmt ./...` and `goimports` (or `mise run format`).
3. Assert that all unit tests pass with `go test ./...` (or `mise run test`).
4. Submit a Pull Request describing your changes.
