# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/2.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Todo

### Added

- Workspace Crawling: Scans workspace on startup, uses `xsmd.toml` to define the root.
- Go to Definition:
  - Resolves workspace-relative paths
  - Resolves folder-relative paths
- Find References: Queries and displays backreferences
- Folding Ranges:
  Supports structural folding for headers (`# Headings`, `## Subheadings`) and nested lists (`-` or `*`).
- Autocompletion:
  - Triggers on `[`
  - Triggers on `(` inside a link (e.g., `[Label](`)
  - Automatically caches the primary `# H1` title of each Markdown
  - Ignores notes without an H1 header.
- Renaming: moves files and automatically updates all reference links across the workspace.
- Configuration (`xsmd.toml`):
  - `debug`: Setting to toggle verbose logging to `xsmd.log`.
  - `ignore`: Directory paths to ignore during autocompletion.
- **LSP Command**:
  - `xsmd.dumpState`: Custom command that outputs a list of all currently indexed workspace file URIs directly to `xsmd.log`.
- **CLI Subcommand**:
  - `list`: Outputs all workspace Markdown file relative paths (respecting ignore lists) directly to standard output.
