# Extra Small Markdown LSP (xs-md)

Another LSP server for Markdown note-taking.

## Installation

If you're on MacOS or Linux:

```sh
curl -sSfL https://raw.githubusercontent.com/RaquerLabs/xsmd/main/install.sh | sh
```

If you're on windows:

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

### Commands

The server provides a list of commands for debug:

- `xsmd.dumpState`: Outputs a list of all current indexed document keys to `xsmd.log`.
  In Neovim, you can run this with:
  ```plaintext
  :XsmdDump
  ```

## Features Implemented

- Workspace Crawling:
  Scans your vault on boot,
  locates the project root via the anchor file `xsmd.toml`.
- Go to Definition:
  - Links starting with `/` (e.g., `[Link](/docs/file.md)`) are resolved relative to the workspace root.
  - Links not starting with `/` (e.g., `[Link](../file.md)`) are resolved relative to the current file's folder.
- Find References
- Folding:
  - `# Headings`, `## Subheadings`
  - nested lists (`-` or `*`)
- Autocomplete:
  - Caches the primary `# H1 Title` of every note in the directory.
    Filtering out files that don't have `# H1 Title` headers.
  - Typing `[` autocompletes with note names, adding the folder-relative `[Title Text](../path/to/note.md)` snippet.
  - Typing `(` inside a link (e.g., `[Label](`) autocompletes with paths, also adds the folder-relative snippet.
- Rename

## How It Works Under the Hood

The server communicates with Neovim using standard input/output (`stdin`/`stdout`) over JSON-RPC.

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
  - thread-safety concurrency locks
- [Execution Flows](docs/flows.md)
  - boot-time crawl indexing loops
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

List indexed workspace files (ignoring configured directories):

```bash
./dist/xsmd list
```

To install it globally:

```bash
mise run install
```

### Run Tests

```bash
mise run test
```

### How to Contribute

1. Fork the repo and make your adjustments in the Go code.
2. Format your files using `mise run format`.
3. Assert that all unit tests pass with `mise run test`.
4. Send a PR
