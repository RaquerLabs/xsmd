# LSP Execution Flows

This document details the step-by-step transaction sequences of the server during boot and runtime editing.

---

## 1. Booting & Crawling the Workspace

When Neovim launches and initializes the client connection, the server discovers the vault root and indexes Markdown content asynchronously:

```mermaid
sequenceDiagram
    autonumber
    participant Editor as Neovim Client
    participant Main as Main Entrypoint
    participant State as State Index
    participant Crawler as Workspace Crawler
    participant Parser as Markdown Parser

    Editor->>Main: Request: initialize (Root URI)
    Main->>State: Create empty index & root lookup
    Main->>Crawler: Start asynchronous crawl
    Main-->>Editor: Response: initialize (Success)
    
    loop Crawling files
        Crawler->>Parser: ParseMarkdown(file)
        Parser-->>Crawler: Return AST, Title, and Links
        Crawler->>State: Store document details
    end
```

---

## 2. Real-Time Diagnostics (Link Validation)

As you switch buffers or edit notes, the server validates links in the background:

```mermaid
sequenceDiagram
    autonumber
    actor User as User Types Link
    participant Editor as Neovim Client
    participant Server as LSP Handlers
    participant State as State Index
    participant Parser as Markdown Parser

    User->>Editor: Type [Broken Link](missing.md)
    Editor->>Server: Notification: textDocument/didChange (Content)
    Server->>Parser: ParseMarkdown(URI, Content)
    Parser-->>Server: Return AST & exact character ranges
    Server->>State: Update document in-memory
    Server->>Server: Validate link list
    Server->>State: Query: Does missing.md exist in cache?
    State-->>Server: No
    Server->>Server: Query Disk: Does missing.md exist?
    Server->>Editor: Notification: publishDiagnostics (Error on exact columns)
    Editor->>User: Highlight the broken link range in red
```

---

## 3. Precise Link Character Positioning

To prevent diagnostics or rename actions from bleeding into neighboring text, the parser calculates the **exact byte offsets** of links in the source document rather than mapping bounds to the parent line:

1. **AST Node Lookup**: Detects a link node (`ast.KindLink`) during traversal.
2. **Sequential Search**: Matches pattern `](destination)` starting from the last matched offset.
3. **Offset Resolution**: Scans backwards for the corresponding `[` bracket to frame the absolute start.
4. **Column Calculation**: Computes exact start/end line coordinates and characters relative to the row's newline bytes.
