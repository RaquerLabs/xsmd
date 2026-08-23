# Extra Small Markdown LSP (xs-md)

Another LSP server for Markdown note-taking.

## Installation

On macOS or Linux, run this command:

```sh
curl -sSfL https://raw.githubusercontent.com/RaquerLabs/xsmd/main/install.sh | sh
```

On Windows, run this command:

```powershell
iwr https://raw.githubusercontent.com/RaquerLabs/xsmd/main/install.ps1 | iex
```

## Configuration

The LSP server looks for an `xsmd.toml` file at the root of your project.
You can configure the following options in it:

```toml
# Enable verbose debug logs printed to xsmd.log
debug = false

# Folders to ignore during autocomplete. Paths must start from the project root directory.
# For example, "/journal" will ignore everything in "/journal/*"
ignore = []
```

You can provide the same settings per editor through the LSP `initializationOptions` payload.
These settings override `xsmd.toml`:

```json
{ "debug": true, "ignore": ["/journal"] }
```

In Neovim, you pass them with `init_options`:

```lua
local lsp_config = {
  name = "xsmd",
  cmd = { "xsmd" },
  filetypes = { "markdown" },
  init_options = { debug = false, ignore = {} },
}
vim.lsp.start(lsp_config)
```

### Neovim Setup

Make sure that Neovim launches a single `xsmd` process and shares it across all open Markdown buffers.
Configure the server with a dynamic `root_dir`. Specify `name = "xsmd"`. Disable `single_file_support`:

```lua
local lsp_config = {
  name = "xsmd",
  cmd = { "xsmd" },
  filetypes = { "markdown" },
  -- Dynamically resolve the workspace root per-buffer
  root_dir = function(filepath)
    return vim.fs.root(filepath, { "xsmd.toml", ".git" })
  end,
  -- Prevent spawning a process per file/buffer if no root is detected
  single_file_support = false,
  -- ... on_attach, capabilities, settings
}
vim.lsp.start(lsp_config)
```

### Commands

The server provides a list of commands for debug:

- `xsmd.dumpState`: Outputs a list of all current indexed document keys to `xsmd.log`.
  In Neovim, you can run this command with:
  ```plaintext
  :XsmdDump
  ```

## Features Implemented

- Workspace crawling: Scans your vault on boot and locates the project root with the anchor file `xsmd.toml`.
- Workspace file watching: Registers filesystem watchers for Markdown files (`**/*.md`, `**/*.markdown`). The watchers keep the in-memory database in sync when files change externally.
- Go to definition:
  - The server resolves links that start with `/` (for example, `[Link](/docs/file.md)`) relative to the workspace root.
  - The server resolves links that do not start with `/` (for example, `[Link](../file.md)`) relative to the folder of the current file.
- Find references
- Folding:
  - `# Headings`, `## Subheadings`
  - Nested lists (`-` or `*`)
- Autocomplete:
  - Caches the primary `# H1 Title` of every note in the directory. It excludes the notes that do not have a `# H1 Title` header.
  - Typing `[` autocompletes with note names. It adds the folder-relative `[Title Text](../path/to/note.md)` snippet.
  - Typing `(` inside a link (for example, `[Label](`) autocompletes with paths. It also adds the folder-relative snippet.
- Rename: Moves files and updates all reference links across the workspace.

## Todo

- [ ] Anchor completion: Complete `#heading` anchors. The anchors are in-file headings for `[](#` and target-file headings for links like `[x](file.md#`. The feature uses a per-document heading index.

## How It Works

The server communicates with Neovim over JSON-RPC. The transport is standard input/output (`stdin`/`stdout`).

```text
       ┌───────────┐      JSON-RPC (stdio)      ┌─────────────┐
       │    IDE    │ ────────────────────────>  │   Go Core   │
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

- [Architecture Guide](docs/architecture.md)
  - visual dependencies
  - modules map
  - concurrency locks
- [Execution Flows](docs/flows.md)
  - boot-time crawl loops
  - real-time diagnostics triggers
  - character coordinate parsing
- [Development & Contributing](docs/development.md)
  - compiling locally
  - formatting files
  - running tests
  - git contributions

## Developer Quick Start

### Build and Run

Compile:

```bash
mise run build
```

Launch the LSP server:

```bash
./dist/xsmd
```

List the indexed workspace files, except the configured directories:

```bash
./dist/xsmd list
```

List the workspace files as JSON (`path`, `title`, `has_h1`), sorted by path:

```bash
./dist/xsmd list --json
```

Print the version:

```bash
./dist/xsmd --version
```

Install globally:

```bash
mise run install
```

### Run Tests

```bash
mise run test
```

### How to Contribute

1. Fork the repo. Make your changes in the Go code.
2. Format your files with `mise run format`.
3. Run the tests with `mise run test`. Make sure that all unit tests pass.
4. Send a PR.
