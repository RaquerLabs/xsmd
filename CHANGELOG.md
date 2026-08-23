# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/2.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- Registers `workspace/didChangeWatchedFiles` with the client to keep the in-memory index in sync with external changes.
- Adds trace logging in `xsmd.log` for file watcher events (create, change, and delete).
- Consolidates the duplicate link lookup algorithm into a centralized `parser.FindLinkAtPosition` helper.
- Refactors the duplicated line-offset-table calculations into a unified `parser.LineOffsetTable` helper.
- Standardizes the path resolution and URI cleaning helpers into a new `internal/state/path.go` file (methods `CleanURIPath` and `ResolveLinkPath`).
- Silences the noisy `log.Printf` calls on stdout/stderr during diagnostics and startup. Errors remain logged. Diagnostic info traces go through `sState.Log` (active only when `debug = true` in `xsmd.toml`).
- Adds `xsmd.log` to `.gitignore`. We do not commit debug trace logs.
- Workspace Crawling: Scans the workspace on startup. It uses `xsmd.toml` to define the root.
- Go to Definition:
  - Resolves workspace-relative paths
  - Resolves folder-relative paths
- Find References: Queries and displays backreferences
- Folding Ranges: Supports structural folding for headers (`# Headings`, `## Subheadings`) and nested lists (`-` or `*`).
- Autocomplete:
  - Triggers on `[`
  - Triggers on `(` inside a link (for example, `[Label](`)
  - Caches the primary `# H1` title of each Markdown note
  - Ignores notes without an H1 header
- Renaming: Moves files and updates all reference links across the workspace.
- Configuration (`xsmd.toml`):
  - `debug`: Setting to toggle verbose logging to `xsmd.log`.
  - `ignore`: Directory paths to ignore during autocomplete.
- **LSP Command**:
  - `xsmd.dumpState`: Custom command that writes the URI of every indexed workspace file to `xsmd.log`.
- **CLI Subcommand**:
  - `list`: Outputs the relative paths of all workspace Markdown files to standard output. It respects the ignore lists.
