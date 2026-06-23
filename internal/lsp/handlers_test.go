package lsp

import (
	"strings"
	"testing"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/yourusername/gcgb-md/internal/state"
)

func setupTestState() *state.ServerState {
	s := state.NewServerState()
	s.WorkspaceRoot = "/workspace"

	// Add file1.md
	// Line numbers are 0-indexed:
	// Line 0: # File One
	// Line 1:
	// Line 2: Check out [File Two](file2.md) or [sub/file3.md](sub/file3.md).
	_ = s.ParseAndIndexContent("file:///workspace/file1.md", []byte(`# File One

Check out [File Two](file2.md) or [sub/file3.md](sub/file3.md).
`))

	// Add file2.md
	_ = s.ParseAndIndexContent("file:///workspace/file2.md", []byte(`# File Two

No links here.
`))

	// Add sub/file3.md
	_ = s.ParseAndIndexContent("file:///workspace/sub/file3.md", []byte(`# File Three

Backlink to [File One](../file1.md).
`))

	return s
}

func TestTextDocumentDefinition(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	params := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file1.md",
			},
			Position: protocol.Position{
				Line:      2,
				Character: 15, // inside the [File Two](file2.md) link line
			},
		},
	}

	res, err := handler.TextDocumentDefinition(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loc, ok := res.(protocol.Location)
	if !ok {
		t.Fatalf("expected protocol.Location, got %T", res)
	}

	expectedURI := "file:///workspace/file2.md"
	if loc.URI != expectedURI {
		t.Errorf("expected destination URI '%s', got '%s'", expectedURI, loc.URI)
	}
}

func TestTextDocumentReferences(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file2.md",
			},
		},
	}

	res, err := handler.TextDocumentReferences(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res) != 1 {
		t.Fatalf("expected 1 reference, got %d", len(res))
	}

	loc := res[0]
	expectedURI := "file:///workspace/file1.md"
	if loc.URI != expectedURI {
		t.Errorf("expected reference URI '%s', got '%s'", expectedURI, loc.URI)
	}
}

func TestTextDocumentCompletion(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file1.md",
			},
		},
	}

	res, err := handler.TextDocumentCompletion(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	items, ok := res.([]protocol.CompletionItem)
	if !ok {
		t.Fatalf("expected []protocol.CompletionItem, got %T", res)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 completion items, got %d", len(items))
	}

	foundTwo := false
	foundThree := false

	for _, item := range items {
		if item.Label == "File Two" {
			foundTwo = true
			if *item.InsertText != "File Two](file2.md)" {
				t.Errorf("unexpected InsertText for File Two: %s", *item.InsertText)
			}
		} else if item.Label == "File Three" {
			foundThree = true
			if *item.InsertText != "File Three](sub/file3.md)" {
				t.Errorf("unexpected InsertText for File Three: %s", *item.InsertText)
			}
		}
	}

	if !foundTwo || !foundThree {
		t.Errorf("did not find expected completion items (foundTwo=%v, foundThree=%v)", foundTwo, foundThree)
	}
}

func TestRenameLinkPrepare(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	params := &protocol.PrepareRenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file1.md",
			},
			Position: protocol.Position{
				Line:      2,
				Character: 15,
			},
		},
	}

	res, err := handler.TextDocumentPrepareRename(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", res)
	}

	placeholder, ok := m["placeholder"].(string)
	if !ok || placeholder != "file2.md" {
		t.Errorf("expected placeholder 'file2.md', got '%v'", m["placeholder"])
	}
}

func TestTextDocumentRename(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	params := &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file1.md",
			},
			Position: protocol.Position{
				Line:      2,
				Character: 15,
			},
		},
		NewName: "new_file2.md",
	}

	res, err := handler.TextDocumentRename(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edit := res
	if edit == nil {
		t.Fatal("expected non-nil WorkspaceEdit")
	}

	if len(edit.DocumentChanges) != 2 {
		t.Fatalf("expected 2 document changes, got %d", len(edit.DocumentChanges))
	}

	textEditFound := false
	renameOpFound := false

	for _, change := range edit.DocumentChanges {
		switch op := change.(type) {
		case protocol.TextDocumentEdit:
			if op.TextDocument.URI == "file:///workspace/file1.md" {
				textEditFound = true
				if len(op.Edits) != 1 {
					t.Fatalf("expected 1 edit in file1.md, got %d", len(op.Edits))
				}
				te := op.Edits[0].(protocol.TextEdit)
				if !strings.Contains(te.NewText, "new_file2.md") {
					t.Errorf("expected text edit to contain 'new_file2.md', got '%s'", te.NewText)
				}
			}
		case protocol.RenameFile:
			renameOpFound = true
			if op.OldURI != "file:///workspace/file2.md" {
				t.Errorf("expected OldURI 'file:///workspace/file2.md', got '%s'", op.OldURI)
			}
			if op.NewURI != "file:///workspace/new_file2.md" {
				t.Errorf("expected NewURI 'file:///workspace/new_file2.md', got '%s'", op.NewURI)
			}
		}
	}

	if !textEditFound || !renameOpFound {
		t.Errorf("did not find both expected text edit and rename operations (textEditFound=%v, renameOpFound=%v)", textEditFound, renameOpFound)
	}
}

func TestWorkspaceWillRenameFiles(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	params := &protocol.RenameFilesParams{
		Files: []protocol.FileRename{
			{
				OldURI: "file:///workspace/file2.md",
				NewURI: "file:///workspace/new_file2.md",
			},
		},
	}

	res, err := handler.WorkspaceWillRenameFiles(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edit := res
	if edit == nil {
		t.Fatal("expected non-nil WorkspaceEdit")
	}

	edits, ok := edit.Changes["file:///workspace/file1.md"]
	if !ok {
		t.Fatal("expected edits for file1.md")
	}

	if len(edits) != 1 {
		t.Fatalf("expected 1 edit in file1.md, got %d", len(edits))
	}

	te := edits[0]
	if !strings.Contains(te.NewText, "new_file2.md") {
		t.Errorf("expected updated text to contain 'new_file2.md', got '%s'", te.NewText)
	}
}

func TestTextDocumentFoldingRange(t *testing.T) {
	s := state.NewServerState()
	s.WorkspaceRoot = "/workspace"

	_ = s.ParseAndIndexContent("file:///workspace/folding.md", []byte(`# Heading 1
Some content.

## Heading 2
Some other content under heading 2.

- List item 1
  - Nested list item 1.1
  - Nested list item 1.2
`))

	handler := BuildHandler(s)

	params := &protocol.FoldingRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///workspace/folding.md",
		},
	}

	res, err := handler.TextDocumentFoldingRange(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res) < 2 {
		t.Fatalf("expected folding ranges, got %d", len(res))
	}
}

func TestPublishDiagnostics(t *testing.T) {
	s := setupTestState()

	// Create a document with broken and working links.
	// file2.md exists in our test state.
	// missing.md does not exist.
	_ = s.ParseAndIndexContent("file:///workspace/doc_diag.md", []byte(`# Document With Diagnostics

Working link: [File Two](file2.md)
Broken link: [Missing Note](missing.md)
External link: [Google](https://google.com)
Anchor link: [Section](#section)
`))

	var notifiedMethod string
	var notifiedParams *protocol.PublishDiagnosticsParams

	ctx := &glsp.Context{
		Notify: func(method string, params any) {
			notifiedMethod = method
			if p, ok := params.(*protocol.PublishDiagnosticsParams); ok {
				notifiedParams = p
			}
		},
	}

	PublishDiagnostics(s, ctx, "file:///workspace/doc_diag.md")

	if notifiedMethod != "textDocument/publishDiagnostics" {
		t.Fatalf("expected notified method 'textDocument/publishDiagnostics', got '%s'", notifiedMethod)
	}

	if notifiedParams == nil {
		t.Fatal("expected non-nil notified params")
	}

	if notifiedParams.URI != "file:///workspace/doc_diag.md" {
		t.Errorf("expected URI 'file:///workspace/doc_diag.md', got '%s'", notifiedParams.URI)
	}

	// Only 1 broken link ("missing.md") should produce a diagnostic
	if len(notifiedParams.Diagnostics) != 1 {
		t.Fatalf("expected exactly 1 diagnostic, got %d", len(notifiedParams.Diagnostics))
	}

	diag := notifiedParams.Diagnostics[0]
	if !strings.Contains(diag.Message, "Broken link") {
		t.Errorf("expected diagnostic message to mention 'Broken link', got '%s'", diag.Message)
	}
}
