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
