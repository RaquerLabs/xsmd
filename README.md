# Extra Small Markdown LSP (xs-md)

A small LSP server for Markdown for note-taking.

It balances speed and low memory usage, integrating directly with Neovim's modern built-in LSP client to provide seamless cross-note navigation and structural tools.

## Features Implemented

- **Workspace Crawling:**
  Scans your vault on boot, locating the project root via the anchor file `xsmd.toml`.
- **Go to Definition (`gd`):**
  Jumps instantly to the resolved absolute path of the target Markdown file under your cursor:
  - Links starting with `/` (e.g., `[Link](/docs/file.md)`) are resolved relative to the workspace root.
  - Links not starting with `/` (e.g., `[Link](../file.md)`) are resolved relative to the current file's folder.
- **Find References (`gD` / `gr`):**
  Inverts your links in memory to instantly gather every backlink across your workspace pointing to your active file.
- **Smart Folding (`za` / `zM`):**
  Walk the Goldmark AST to fold text cleanly under `# Headings`, `## Subheadings`, and nested lists (`-` or `*`).
- **Human-Readable Autocomplete (`[`):**
  Caches the primary `# H1 Title` of every note in the directory. Filtering out unformatted files, typing `[` pops open a floating menu of note names, automatically injecting a valid folder-relative `[Title Text](../path/to/note.md)` snippet.
- **Inline Link Renaming (LSP Rename):**
  Triggering your LSP rename shortcut on any Markdown link pre-fills the exact file path. Submitting the new name physically moves the file on disk and automatically patches every backlink across your entire vault, maintaining correct root-relative or folder-relative paths.
- **Workspace File Operations:**
  Move or rename files natively in your Neovim file tree (like `oil.nvim` or `neo-tree`). The server intercepts the filesystem event to automatically fix any broken links pointing to the old path, correctly adjusting formatting based on the linking strategy.

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

Compile:

```bash
mise run build
```

Launch:

```bash
./dist/xsmd
```

To install it globally to your path (`~/go/bin/xsmd`) so Neovim can launch it:

```bash
mise run install
```

### 2. Run Tests

Execute the complete test suite:

```bash
mise run test
```

### 3. How to Contribute

1. Fork the repo and make your adjustments in the Go code.
2. Format your files using `mise run format`.
3. Assert that all unit tests pass with `mise run test`.
4. Send a PR
