package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func init() {
	DisableProcessSharedLock = true
}

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
[
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
			Position: protocol.Position{
				Line:      3,
				Character: 1,
			},
		},
	}

	res, err := handler.TextDocumentCompletion(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := res.(protocol.CompletionList)
	if !ok {
		t.Fatalf("expected protocol.CompletionList, got %T", res)
	}
	items := list.Items

	if len(items) != 2 {
		t.Fatalf("expected 2 completion items, got %d", len(items))
	}

	foundTwo := false
	foundThree := false

	for _, item := range items {
		switch item.Label {
		case "File Two":
			foundTwo = true
			if *item.InsertText != "File Two](file2.md)" {
				t.Errorf("unexpected InsertText for File Two: %s", *item.InsertText)
			}
		case "File Three":
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

func TestTextDocumentCompletionWithPosition(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	// Add a new file that has a half-completed link:
	// Line 0: Click [Fi
	_ = s.ParseAndIndexContent("file:///workspace/file4.md", []byte("Click [Fi"))

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file4.md",
			},
			Position: protocol.Position{
				Line:      0,
				Character: 9, // right after the 'i' in '[Fi'
			},
		},
	}

	res, err := handler.TextDocumentCompletion(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := res.(protocol.CompletionList)
	if !ok {
		t.Fatalf("expected protocol.CompletionList, got %T", res)
	}
	items := list.Items

	// We expect 3 completion items (File One, File Two, File Three)
	if len(items) != 3 {
		t.Fatalf("expected 3 completion items, got %d", len(items))
	}

	foundOne := false
	for _, item := range items {
		if item.Label == "File One" {
			foundOne = true
			if item.TextEdit == nil {
				t.Fatalf("expected TextEdit to be set")
			}
			te, ok := item.TextEdit.(*protocol.TextEdit)
			if !ok {
				t.Fatalf("expected *protocol.TextEdit, got %T", item.TextEdit)
			}
			// Start character should be 7 (right after '[')
			if te.Range.Start.Character != 7 {
				t.Errorf("expected TextEdit start character 7, got %d", te.Range.Start.Character)
			}
			if te.Range.End.Character != 9 {
				t.Errorf("expected TextEdit end character 9, got %d", te.Range.End.Character)
			}
			if te.NewText != "File One](file1.md)" {
				t.Errorf("unexpected NewText: %s", te.NewText)
			}
		}
	}

	if !foundOne {
		t.Error("did not find completion item for File One")
	}
}

func TestTextDocumentCompletionFiltering(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	// Add a new file that has a partially typed link with a space:
	// Line 0: Click [Two
	_ = s.ParseAndIndexContent("file:///workspace/file5.md", []byte("Click [Two "))

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file5.md",
			},
			Position: protocol.Position{
				Line:      0,
				Character: 11, // right after the trailing space in '[Two '
			},
		},
	}

	res, err := handler.TextDocumentCompletion(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := res.(protocol.CompletionList)
	if !ok {
		t.Fatalf("expected protocol.CompletionList, got %T", res)
	}
	items := list.Items

	// We expect ONLY 1 completion item ("File Two") because "Two" filters out File One and File Three
	if len(items) != 1 {
		t.Fatalf("expected 1 completion item, got %d", len(items))
	}

	if items[0].Label != "File Two" {
		t.Errorf("expected completion item 'File Two', got '%s'", items[0].Label)
	}
}

func TestTextDocumentCompletionWithTrailingSpace(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	// Add a new file that has a partially typed link followed by spaces:
	// Line 0: Click [Two
	_ = s.ParseAndIndexContent("file:///workspace/file6.md", []byte("Click [Two   "))

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file6.md",
			},
			Position: protocol.Position{
				Line:      0,
				Character: 13, // right after the last space of '[Two   '
			},
		},
	}

	res, err := handler.TextDocumentCompletion(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := res.(protocol.CompletionList)
	if !ok {
		t.Fatalf("expected protocol.CompletionList, got %T", res)
	}
	items := list.Items

	if len(items) != 1 {
		t.Fatalf("expected 1 completion item, got %d", len(items))
	}

	item := items[0]
	if item.Label != "File Two" {
		t.Errorf("expected completion item 'File Two', got '%s'", item.Label)
	}

	if item.TextEdit == nil {
		t.Fatalf("expected TextEdit to be set")
	}

	te, ok := item.TextEdit.(*protocol.TextEdit)
	if !ok {
		t.Fatalf("expected *protocol.TextEdit, got %T", item.TextEdit)
	}

	// Start character should be 7 (right after '[')
	if te.Range.Start.Character != 7 {
		t.Errorf("expected TextEdit start character 7, got %d", te.Range.Start.Character)
	}
	// End character should be 13 (all the way to the cursor)
	if te.Range.End.Character != 13 {
		t.Errorf("expected TextEdit end character 13, got %d", te.Range.End.Character)
	}
	if te.NewText != "File Two](file2.md)" {
		t.Errorf("unexpected NewText: %s", te.NewText)
	}
}

func TestTextDocumentCompletionFuzzyDirectoryLeaking(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	// file3.md is in /workspace/sub/file3.md.
	// Its relative path from root or source is sub/file3.md.
	// If we search for 'sub', it should match sub/file3.md.
	// If we search for 'sb' (fuzzy for sub), it should match sub/file3.md only if we match full path,
	// but since the query 'sb' has no slash, it should NOT match sub/file3.md unless 'sb' matches 'File Three' (Title) or 'file3.md' (Basename).
	// Neither matches 'sb'. So 'sb' should yield 0 results.

	_ = s.ParseAndIndexContent("file:///workspace/file7.md", []byte("Click [sb"))

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file7.md",
			},
			Position: protocol.Position{
				Line:      0,
				Character: 9, // right after 'b'
			},
		},
	}

	res, err := handler.TextDocumentCompletion(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := res.(protocol.CompletionList)
	if !ok {
		t.Fatalf("expected protocol.CompletionList, got %T", res)
	}
	items := list.Items

	// We expect 0 items because 'sb' matches 'sub' in the path, but 'sb' has no slash,
	// so the path directory prefix is ignored.
	if len(items) != 0 {
		t.Fatalf("expected 0 completion items, got %d", len(items))
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

func TestRenameExactRangeAndTracking(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	params := &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file1.md",
			},
			Position: protocol.Position{
				Line:      2,
				Character: 15, // inside the [File Two](file2.md) link
			},
		},
		NewName: "new_file2.md",
	}

	res, err := handler.TextDocumentRename(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res == nil {
		t.Fatal("expected non-nil WorkspaceEdit")
	}

	// Verify exact range edits
	var textEdit *protocol.TextEdit
	for _, change := range res.DocumentChanges {
		if op, ok := change.(protocol.TextDocumentEdit); ok {
			if op.TextDocument.URI == "file:///workspace/file1.md" {
				if len(op.Edits) != 1 {
					t.Fatalf("expected 1 edit in file1.md, got %d", len(op.Edits))
				}
				te := op.Edits[0].(protocol.TextEdit)
				textEdit = &te
			}
		}
	}

	if textEdit == nil {
		t.Fatal("expected text edit for file1.md")
	}

	// Expect range starting at character 21 and ending at 29 (the path "file2.md" on line 2)
	if textEdit.Range.Start.Line != 2 || textEdit.Range.Start.Character != 21 {
		t.Errorf("expected Start line 2 character 21, got %v", textEdit.Range.Start)
	}
	if textEdit.Range.End.Line != 2 || textEdit.Range.End.Character != 29 {
		t.Errorf("expected End line 2 character 29, got %v", textEdit.Range.End)
	}
	if textEdit.NewText != "new_file2.md" {
		t.Errorf("expected NewText 'new_file2.md', got '%s'", textEdit.NewText)
	}

	// Check that s.ProcessedRenames has the entry
	s.Mu.RLock()
	val, ok := s.ProcessedRenames["/workspace/file2.md"]
	s.Mu.RUnlock()
	if !ok || val != "/workspace/new_file2.md" {
		t.Errorf("expected ProcessedRenames entry '/workspace/file2.md' -> '/workspace/new_file2.md', got '%s'", val)
	}

	// Now simulate the workspaceWillRenameFiles trigger
	willRenameParams := &protocol.RenameFilesParams{
		Files: []protocol.FileRename{
			{
				OldURI: "file:///workspace/file2.md",
				NewURI: "file:///workspace/new_file2.md",
			},
		},
	}

	resWillRename, err := handler.WorkspaceWillRenameFiles(nil, willRenameParams)
	if err != nil {
		t.Fatalf("unexpected error during willRenameFiles: %v", err)
	}

	// Should be empty because it was ignored
	if len(resWillRename.Changes) > 0 {
		t.Errorf("expected no changes because rename was already handled, got: %v", resWillRename.Changes)
	}
}

func TestDuplicateWorkspaceWillRenameFiles(t *testing.T) {
	s := state.NewServerState()
	s.WorkspaceRoot = "/workspace"
	_ = s.ParseAndIndexContent("file:///workspace/ref.md", []byte(`[testing](test.md)`))
	_ = s.ParseAndIndexContent("file:///workspace/test.md", []byte(`# Test`))

	handler := BuildHandler(s)

	params := &protocol.RenameFilesParams{
		Files: []protocol.FileRename{
			{
				OldURI: "file:///workspace/test.md",
				NewURI: "file:///workspace/test-oi.md",
			},
		},
	}

	// First call to WorkspaceWillRenameFiles
	res1, err := handler.WorkspaceWillRenameFiles(nil, params)
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}

	edits1, ok := res1.Changes["file:///workspace/ref.md"]
	if !ok || len(edits1) != 1 {
		t.Fatalf("expected 1 edit in ref.md on first call, got %d", len(edits1))
	}
	if edits1[0].NewText != "test-oi.md" {
		t.Errorf("expected 'test-oi.md', got '%s'", edits1[0].NewText)
	}

	// Second (duplicate/consecutive) call to WorkspaceWillRenameFiles before watched file event
	res2, err := handler.WorkspaceWillRenameFiles(nil, params)
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}

	// Should be empty/ignored because we already registered/processed it
	if len(res2.Changes) > 0 {
		t.Errorf("expected no changes on duplicate/consecutive call, got: %v", res2.Changes)
	}
}

func TestUserReportedRenameBug(t *testing.T) {
	s := state.NewServerState()
	s.WorkspaceRoot = "/workspace"
	_ = s.ParseAndIndexContent("file:///workspace/ref.md", []byte(`[testing](test.md)`))
	_ = s.ParseAndIndexContent("file:///workspace/test.md", []byte(`# Test`))

	handler := BuildHandler(s)

	params := &protocol.RenameFilesParams{
		Files: []protocol.FileRename{
			{
				OldURI: "file:///workspace/test.md",
				NewURI: "file:///workspace/test-hello.md",
			},
		},
	}

	res, err := handler.WorkspaceWillRenameFiles(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edits, ok := res.Changes["file:///workspace/ref.md"]
	if !ok || len(edits) != 1 {
		t.Fatalf("expected 1 edit in ref.md, got %d", len(edits))
	}

	te := edits[0]
	if te.NewText != "test-hello.md" {
		t.Errorf("expected new link path 'test-hello.md', got '%s'", te.NewText)
	}

	// Range checking: should be line 0, characters from 10 to 17
	if te.Range.Start.Line != 0 || te.Range.Start.Character != 10 {
		t.Errorf("expected start at line 0, char 10, got line %d, char %d", te.Range.Start.Line, te.Range.Start.Character)
	}
	if te.Range.End.Line != 0 || te.Range.End.Character != 17 {
		t.Errorf("expected end at line 0, char 17, got line %d, char %d", te.Range.End.Line, te.Range.End.Character)
	}
}

func TestUserReportedRenameBugTextDoc(t *testing.T) {
	s := state.NewServerState()
	s.WorkspaceRoot = "/workspace"
	_ = s.ParseAndIndexContent("file:///workspace/ref.md", []byte(`[testing](test.md)`))
	_ = s.ParseAndIndexContent("file:///workspace/test.md", []byte(`# Test`))

	handler := BuildHandler(s)

	params := &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/ref.md",
			},
			Position: protocol.Position{
				Line:      0,
				Character: 12, // inside "test.md"
			},
		},
		NewName: "test-hello.md",
	}

	res, err := handler.TextDocumentRename(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the text edit is correct
	var textEdit *protocol.TextEdit
	for _, change := range res.DocumentChanges {
		if op, ok := change.(protocol.TextDocumentEdit); ok {
			if op.TextDocument.URI == "file:///workspace/ref.md" {
				if len(op.Edits) != 1 {
					t.Fatalf("expected 1 edit in ref.md, got %d", len(op.Edits))
				}
				te := op.Edits[0].(protocol.TextEdit)
				textEdit = &te
			}
		}
	}

	if textEdit == nil {
		t.Fatal("expected text edit for ref.md")
	}

	if textEdit.NewText != "test-hello.md" {
		t.Errorf("expected new link path 'test-hello.md', got '%s'", textEdit.NewText)
	}

	if textEdit.Range.Start.Line != 0 || textEdit.Range.Start.Character != 10 {
		t.Errorf("expected start at line 0, char 10, got line %d, char %d", textEdit.Range.Start.Line, textEdit.Range.Start.Character)
	}
	if textEdit.Range.End.Line != 0 || textEdit.Range.End.Character != 17 {
		t.Errorf("expected end at line 0, char 17, got line %d, char %d", textEdit.Range.End.Line, textEdit.Range.End.Character)
	}
}

func TestRenameWithAnchors(t *testing.T) {
	s := state.NewServerState()
	s.WorkspaceRoot = "/workspace"
	_ = s.ParseAndIndexContent("file:///workspace/ref.md", []byte(`[testing](test.md#some-section)`))
	_ = s.ParseAndIndexContent("file:///workspace/test.md", []byte(`# Test`))

	handler := BuildHandler(s)

	params := &protocol.RenameFilesParams{
		Files: []protocol.FileRename{
			{
				OldURI: "file:///workspace/test.md",
				NewURI: "file:///workspace/test-hello.md",
			},
		},
	}

	res, err := handler.WorkspaceWillRenameFiles(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edits, ok := res.Changes["file:///workspace/ref.md"]
	if !ok || len(edits) != 1 {
		t.Fatalf("expected 1 edit in ref.md, got %d", len(edits))
	}

	te := edits[0]
	if te.NewText != "test-hello.md#some-section" {
		t.Errorf("expected new link path 'test-hello.md#some-section', got '%s'", te.NewText)
	}

	// Range checking: should be line 0, characters from 10 to 30
	if te.Range.Start.Line != 0 || te.Range.Start.Character != 10 {
		t.Errorf("expected start at line 0, char 10, got line %d, char %d", te.Range.Start.Line, te.Range.Start.Character)
	}
	if te.Range.End.Line != 0 || te.Range.End.Character != 30 {
		t.Errorf("expected end at line 0, char 30, got line %d, char %d", te.Range.End.Line, te.Range.End.Character)
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

func TestNewLinkingStrategy(t *testing.T) {
	s := state.NewServerState()
	s.WorkspaceRoot = "/workspace"

	// 1. Parse and index content for testing
	_ = s.ParseAndIndexContent("file:///workspace/file1.md", []byte(`# File One

Check out [File Two](file2.md).
`))

	_ = s.ParseAndIndexContent("file:///workspace/file2.md", []byte(`# File Two

No links here.
`))

	_ = s.ParseAndIndexContent("file:///workspace/sub/file3.md", []byte(`# File Three

No links here.
`))

	_ = s.ParseAndIndexContent("file:///workspace/sub/file4.md", []byte(`# File Four

Root link to [File Two](/file2.md) and relative link to [File Three](file3.md).
[
`))

	handler := BuildHandler(s)

	// --- Test Definition ---
	// Root-relative definition resolution
	paramsRoot := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/sub/file4.md",
			},
			Position: protocol.Position{
				Line:      2,
				Character: 20, // inside [File Two](/file2.md)
			},
		},
	}
	resRoot, err := handler.TextDocumentDefinition(nil, paramsRoot)
	if err != nil {
		t.Fatalf("definition root link error: %v", err)
	}
	locRoot, ok := resRoot.(protocol.Location)
	if !ok {
		t.Fatalf("expected protocol.Location for root link definition, got %T", resRoot)
	}
	if locRoot.URI != "file:///workspace/file2.md" {
		t.Errorf("expected destination URI 'file:///workspace/file2.md', got '%s'", locRoot.URI)
	}

	// Folder-relative definition resolution
	paramsDir := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/sub/file4.md",
			},
			Position: protocol.Position{
				Line:      2,
				Character: 60, // inside [File Three](file3.md)
			},
		},
	}
	resDir, err := handler.TextDocumentDefinition(nil, paramsDir)
	if err != nil {
		t.Fatalf("definition relative link error: %v", err)
	}
	locDir, ok := resDir.(protocol.Location)
	if !ok {
		t.Fatalf("expected protocol.Location for relative link definition, got %T", resDir)
	}
	if locDir.URI != "file:///workspace/sub/file3.md" {
		t.Errorf("expected destination URI 'file:///workspace/sub/file3.md', got '%s'", locDir.URI)
	}

	// --- Test References ---
	// References for file2.md should find links in file1.md and sub/file4.md
	paramsRefs := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file2.md",
			},
		},
	}
	resRefs, err := handler.TextDocumentReferences(nil, paramsRefs)
	if err != nil {
		t.Fatalf("references error: %v", err)
	}
	if len(resRefs) != 2 {
		t.Fatalf("expected 2 references to file2.md, got %d", len(resRefs))
	}
	found1 := false
	found4 := false
	for _, ref := range resRefs {
		switch ref.URI {
		case "file:///workspace/file1.md":
			found1 = true
		case "file:///workspace/sub/file4.md":
			found4 = true
		}
	}
	if !found1 || !found4 {
		t.Errorf("did not find references in expected files (found1=%v, found4=%v)", found1, found4)
	}

	// --- Test Diagnostics ---
	_ = s.ParseAndIndexContent("file:///workspace/doc_diag.md", []byte(`# Document With Diagnostics

Working link: [File Two](file2.md)
Broken link: [Missing Note](missing.md)
Root working: [File Two](/file2.md)
Root broken: [Missing](/missing.md)
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
	if notifiedParams == nil || len(notifiedParams.Diagnostics) != 2 {
		t.Fatalf("expected exactly 2 diagnostics for broken links, got %d", len(notifiedParams.Diagnostics))
	}
	diagCount := 0
	for _, d := range notifiedParams.Diagnostics {
		if strings.Contains(d.Message, "Broken link") {
			diagCount++
		}
	}
	if diagCount != 2 {
		t.Errorf("expected 2 broken link diagnostics, got %d", diagCount)
	}

	// --- Test Rename ---
	// Rename file2.md -> new_file2.md from sub/file4.md where it is root-relative
	paramsRename := &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/sub/file4.md",
			},
			Position: protocol.Position{
				Line:      2,
				Character: 20, // inside [File Two](/file2.md)
			},
		},
		NewName: "new_file2.md",
	}
	resRename, err := handler.TextDocumentRename(nil, paramsRename)
	if err != nil {
		t.Fatalf("rename error: %v", err)
	}
	if resRename == nil {
		t.Fatal("expected non-nil WorkspaceEdit")
	}

	// It should output text updates in file1.md and sub/file4.md
	textEditFound1 := false
	textEditFound4 := false
	renameOpFound := false

	for _, change := range resRename.DocumentChanges {
		switch op := change.(type) {
		case protocol.TextDocumentEdit:
			switch op.TextDocument.URI {
			case "file:///workspace/file1.md":
				textEditFound1 = true
				te := op.Edits[0].(protocol.TextEdit)
				if !strings.Contains(te.NewText, "new_file2.md") {
					t.Errorf("expected file1.md edit to use 'new_file2.md', got: %s", te.NewText)
				}
			case "file:///workspace/sub/file4.md":
				textEditFound4 = true
				te := op.Edits[0].(protocol.TextEdit)
				if !strings.Contains(te.NewText, "/new_file2.md") {
					t.Errorf("expected sub/file4.md edit to use '/new_file2.md', got: %s", te.NewText)
				}
			}
		case protocol.RenameFile:
			renameOpFound = true
			if op.OldURI != "file:///workspace/file2.md" {
				t.Errorf("expected RenameFile OldURI 'file:///workspace/file2.md', got '%s'", op.OldURI)
			}
			if op.NewURI != "file:///workspace/new_file2.md" {
				t.Errorf("expected RenameFile NewURI 'file:///workspace/new_file2.md', got '%s'", op.NewURI)
			}
		}
	}
	if !textEditFound1 || !textEditFound4 || !renameOpFound {
		t.Errorf("rename changes missing: found1=%v, found4=%v, renameOp=%v", textEditFound1, textEditFound4, renameOpFound)
	}

	// --- Test Completion ---
	// Complete from sub/file4.md should generate folder-relative path '../file1.md' for file1.md
	paramsComp := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/sub/file4.md",
			},
			Position: protocol.Position{
				Line:      3,
				Character: 1,
			},
		},
	}
	resComp, err := handler.TextDocumentCompletion(nil, paramsComp)
	if err != nil {
		t.Fatalf("completion error: %v", err)
	}
	list, ok := resComp.(protocol.CompletionList)
	if !ok {
		t.Fatalf("expected protocol.CompletionList, got %T", resComp)
	}
	items := list.Items
	foundComp1 := false
	for _, item := range items {
		if item.Label == "File One" {
			foundComp1 = true
			if *item.InsertText != "File One](/file1.md)" {
				t.Errorf("expected insert text to be absolute: 'File One](/file1.md)', got '%s'", *item.InsertText)
			}
		}
	}
	if !foundComp1 {
		t.Errorf("did not find completion item for File One")
	}
}

func TestTextDocumentDidSave(t *testing.T) {
	tempDir := t.TempDir()

	filePath := filepath.Join(tempDir, "saved.md")
	content := []byte("# Saved File\nSome content.")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	s := state.NewServerState()
	s.WorkspaceRoot = tempDir
	handler := BuildHandler(s)

	uri := "file://" + filePath
	params := &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: uri,
		},
	}

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

	err := handler.TextDocumentDidSave(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error on save: %v", err)
	}

	s.Mu.RLock()
	docInfo, exists := s.Index[uri]
	s.Mu.RUnlock()

	if !exists {
		t.Fatalf("expected document %s to be indexed", uri)
	}

	if docInfo.Title != "Saved File" {
		t.Errorf("expected Title 'Saved File', got '%s'", docInfo.Title)
	}

	if notifiedMethod != "textDocument/publishDiagnostics" {
		t.Errorf("expected notified method 'textDocument/publishDiagnostics', got '%s'", notifiedMethod)
	}

	if notifiedParams == nil || notifiedParams.URI != uri {
		t.Errorf("expected diagnostics for URI '%s', got '%v'", uri, notifiedParams)
	}
}

func TestWorkspaceDidChangeWatchedFiles(t *testing.T) {
	tempDir := t.TempDir()

	filePath := filepath.Join(tempDir, "watched.md")
	content := []byte("# Watched File\nSome content.")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	s := state.NewServerState()
	s.WorkspaceRoot = tempDir
	handler := BuildHandler(s)

	uri := "file://" + filePath

	// Test Created/Changed event
	params := &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{
				URI:  uri,
				Type: protocol.FileChangeTypeCreated,
			},
		},
	}

	err := handler.WorkspaceDidChangeWatchedFiles(nil, params)
	if err != nil {
		t.Fatalf("unexpected error on change watched files: %v", err)
	}

	s.Mu.RLock()
	docInfo, exists := s.Index[uri]
	s.Mu.RUnlock()

	if !exists {
		t.Fatalf("expected document %s to be indexed", uri)
	}
	if docInfo.Title != "Watched File" {
		t.Errorf("expected Title 'Watched File', got '%s'", docInfo.Title)
	}

	// Test Deleted event
	paramsDeleted := &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{
				URI:  uri,
				Type: protocol.FileChangeTypeDeleted,
			},
		},
	}

	err = handler.WorkspaceDidChangeWatchedFiles(nil, paramsDeleted)
	if err != nil {
		t.Fatalf("unexpected error on delete watched files: %v", err)
	}

	s.Mu.RLock()
	_, existsAfterDelete := s.Index[uri]
	s.Mu.RUnlock()

	if existsAfterDelete {
		t.Errorf("expected document %s to be deleted from index", uri)
	}
}

func TestWorkspaceDidCreateAndDeleteFiles(t *testing.T) {
	tempDir := t.TempDir()

	filePath := filepath.Join(tempDir, "created.md")
	content := []byte("# Created File\nSome content.")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	s := state.NewServerState()
	s.WorkspaceRoot = tempDir
	handler := BuildHandler(s)

	uri := "file://" + filePath

	// Test DidCreate
	paramsCreate := &protocol.CreateFilesParams{
		Files: []protocol.FileCreate{
			{
				URI: uri,
			},
		},
	}

	err := handler.WorkspaceDidCreateFiles(nil, paramsCreate)
	if err != nil {
		t.Fatalf("unexpected error on create files: %v", err)
	}

	s.Mu.RLock()
	docInfo, exists := s.Index[uri]
	s.Mu.RUnlock()

	if !exists {
		t.Fatalf("expected document %s to be indexed", uri)
	}
	if docInfo.Title != "Created File" {
		t.Errorf("expected Title 'Created File', got '%s'", docInfo.Title)
	}

	// Test DidDelete
	paramsDelete := &protocol.DeleteFilesParams{
		Files: []protocol.FileDelete{
			{
				URI: uri,
			},
		},
	}

	err = handler.WorkspaceDidDeleteFiles(nil, paramsDelete)
	if err != nil {
		t.Fatalf("unexpected error on delete files: %v", err)
	}

	s.Mu.RLock()
	_, existsAfterDelete := s.Index[uri]
	s.Mu.RUnlock()

	if existsAfterDelete {
		t.Errorf("expected document %s to be deleted from index", uri)
	}
}

func TestTextDocumentCompletionFuzzyOutOfOrder(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	// We have a file with Title "File Two" in setupTestState().
	// We want to type "Two File" to search for it, and verify it still matches.
	_ = s.ParseAndIndexContent("file:///workspace/file_fuzzy.md", []byte("Click [Two File"))

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file_fuzzy.md",
			},
			Position: protocol.Position{
				Line:      0,
				Character: 15, // right after "Click [Two File"
			},
		},
	}

	res, err := handler.TextDocumentCompletion(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := res.(protocol.CompletionList)
	if !ok {
		t.Fatalf("expected protocol.CompletionList, got %T", res)
	}
	items := list.Items

	// We expect 1 completion item: "File Two", because "Two File" matches both "Two" and "File".
	if len(items) != 1 {
		t.Fatalf("expected 1 completion item, got %d", len(items))
	}

	if items[0].Label != "File Two" {
		t.Errorf("expected completion item 'File Two', got '%s'", items[0].Label)
	}
}

func TestWorkspaceExecuteCommandDumpState(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	var loggedMsg string
	s.DebugLog = func(msg string) {
		loggedMsg = msg
	}

	params := &protocol.ExecuteCommandParams{
		Command: "xsmd.dumpState",
	}

	res, err := handler.WorkspaceExecuteCommand(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resStr, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}

	if resStr != "State dumped to xsmd.log" {
		t.Errorf("expected result 'State dumped to xsmd.log', got '%s'", resStr)
	}

	if !strings.Contains(loggedMsg, "file:///workspace/file1.md") ||
		!strings.Contains(loggedMsg, "file:///workspace/file2.md") ||
		!strings.Contains(loggedMsg, "file:///workspace/sub/file3.md") {
		t.Errorf("logged message did not contain expected file URIs. Got: '%s'", loggedMsg)
	}
}

func TestTextDocumentCompletionIgnoreDirs(t *testing.T) {
	s := setupTestState()
	s.IgnoreDirs = []string{"/sub"}
	handler := BuildHandler(s)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file1.md",
			},
			Position: protocol.Position{
				Line:      3,
				Character: 1, // cursor after the "["
			},
		},
	}

	res, err := handler.TextDocumentCompletion(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := res.(protocol.CompletionList)
	if !ok {
		t.Fatalf("expected protocol.CompletionList, got %T", res)
	}

	// In setupTestState:
	// file1.md itself is skipped (cannot suggest itself).
	// file2.md is at "/workspace/file2.md" (relative: "file2.md"). Not ignored.
	// sub/file3.md is at "/workspace/sub/file3.md" (relative: "sub/file3.md"). Ignored because relative path starts with "sub/".
	// Therefore, we only expect 1 item: "File Two".
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list.Items))
	}

	if list.Items[0].Label != "File Two" {
		t.Errorf("expected item 'File Two', got '%s'", list.Items[0].Label)
	}
}

func TestTextDocumentCompletionParenthesisTrigger(t *testing.T) {
	s := setupTestState()
	handler := BuildHandler(s)

	// Add file1.md with content: "Check [hello](f"
	_ = s.ParseAndIndexContent("file:///workspace/file1.md", []byte("Check [hello](f"))

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///workspace/file1.md",
			},
			Position: protocol.Position{
				Line:      0,
				Character: 15, // right after the "("
			},
		},
	}

	res, err := handler.TextDocumentCompletion(nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := res.(protocol.CompletionList)
	if !ok {
		t.Fatalf("expected protocol.CompletionList, got %T", res)
	}

	if len(list.Items) == 0 {
		t.Fatalf("expected some completion items, got 0")
	}

	var foundFile2 bool
	for _, item := range list.Items {
		if item.Label == "file2.md" {
			foundFile2 = true
			if *item.InsertText != "file2.md" {
				t.Errorf("expected InsertText to be 'file2.md', got '%s'", *item.InsertText)
			}
			if *item.Detail != "File Two" {
				t.Errorf("expected Detail to be 'File Two', got '%s'", *item.Detail)
			}
		}
	}
	if !foundFile2 {
		t.Errorf("expected to find completion item for 'file2.md'")
	}
}
