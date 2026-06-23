package lsp

import (
	"path/filepath"
	"strings"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/yourusername/gcgb-md/internal/parser"
	"github.com/yourusername/gcgb-md/internal/state"
)

// HandleWorkspaceWillRenameFiles resolves files that are about to be renamed on disk and fixes broken links
func HandleWorkspaceWillRenameFiles(state *state.ServerState, context *glsp.Context, params *protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) {
	state.Mu.RLock()
	defer state.Mu.RUnlock()

	changes := make(map[string][]protocol.TextEdit)

	for _, fileRename := range params.Files {
		oldAbs := strings.TrimPrefix(fileRename.OldURI, "file://")
		newAbs := strings.TrimPrefix(fileRename.NewURI, "file://")

		oldRel, err1 := filepath.Rel(state.WorkspaceRoot, oldAbs)
		newRel, err2 := filepath.Rel(state.WorkspaceRoot, newAbs)

		if err1 != nil || err2 != nil {
			continue // Failsafe if paths escape the workspace
		}

		oldRel = filepath.ToSlash(oldRel)
		newRel = filepath.ToSlash(newRel)

		// Sweep the index to find broken links and patch them
		for uri, docInfo := range state.Index {
			var edits []protocol.TextEdit
			lines := strings.Split(string(docInfo.Content), "\n")

			for _, link := range docInfo.Links {
				if filepath.Clean(link.Path) == filepath.Clean(oldRel) {
					lineIdx := link.Range.Start.Line
					if int(lineIdx) < len(lines) {
						oldLineText := lines[lineIdx]
						newLineText := strings.Replace(oldLineText, "("+link.Path+")", "("+newRel+")", 1)

						if oldLineText != newLineText {
							edits = append(edits, protocol.TextEdit{
								Range: protocol.Range{
									Start: protocol.Position{Line: lineIdx, Character: 0},
									End:   protocol.Position{Line: lineIdx, Character: uint32(len(oldLineText))},
								},
								NewText: newLineText,
							})
						}
					}
				}
			}

			if len(edits) > 0 {
				changes[uri] = append(changes[uri], edits...)
			}
		}
	}

	return &protocol.WorkspaceEdit{
		Changes: changes,
	}, nil
}

// HandleTextDocumentRename handles renaming a markdown file/link directly within the editor
func HandleTextDocumentRename(state *state.ServerState, context *glsp.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	state.Mu.RLock()
	defer state.Mu.RUnlock()

	uri := params.TextDocument.URI
	cursorLine := params.Position.Line

	docInfo, exists := state.Index[uri]
	if !exists {
		return nil, nil
	}

	// Figure out which link we are trying to rename
	var targetLink *parser.ExtractedLink
	for i := range docInfo.Links {
		link := &docInfo.Links[i]
		if cursorLine >= link.Range.Start.Line && cursorLine <= link.Range.End.Line {
			targetLink = link
			break
		}
	}

	if targetLink == nil {
		return nil, nil // Not on a link, ignore
	}

	oldRelPath := filepath.Clean(targetLink.Path)
	newRelPath := filepath.ToSlash(filepath.Clean(params.NewName))

	// Ensure the new name retains a markdown extension
	if !strings.HasSuffix(newRelPath, ".md") && !strings.HasSuffix(newRelPath, ".markdown") {
		newRelPath += ".md"
	}

	oldAbsPath := filepath.Join(state.WorkspaceRoot, oldRelPath)
	newAbsPath := filepath.Join(state.WorkspaceRoot, newRelPath)

	var docChanges []any

	// 1. Find all documents that link to the old path and queue up text edits
	for indexUri, indexDoc := range state.Index {
		var edits []any
		lines := strings.Split(string(indexDoc.Content), "\n")

		for _, link := range indexDoc.Links {
			if filepath.Clean(link.Path) == oldRelPath {
				lineIdx := link.Range.Start.Line
				if int(lineIdx) < len(lines) {
					oldLineText := lines[lineIdx]
					// Strictly replace the path within the parentheses to avoid false positives
					newLineText := strings.Replace(oldLineText, "("+link.Path+")", "("+newRelPath+")", 1)

					if oldLineText != newLineText {
						edits = append(edits, protocol.TextEdit{
							Range: protocol.Range{
								Start: protocol.Position{Line: lineIdx, Character: 0},
								End:   protocol.Position{Line: lineIdx, Character: uint32(len(oldLineText))},
							},
							NewText: newLineText,
						})
					}
				}
			}
		}

		if len(edits) > 0 {
			docChanges = append(docChanges, protocol.TextDocumentEdit{
				TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: indexUri},
				},
				Edits: edits,
			})
		}
	}

	// 2. Queue the physical file rename operation
	renameOp := protocol.RenameFile{
		Kind:   "rename",
		OldURI: "file://" + oldAbsPath,
		NewURI: "file://" + newAbsPath,
	}
	docChanges = append(docChanges, renameOp)

	// Ship the combined edits + file move back to Neovim to execute
	return &protocol.WorkspaceEdit{
		DocumentChanges: docChanges,
	}, nil
}

// HandleTextDocumentPrepareRename determines the text range to select and display when starting a rename action
func HandleTextDocumentPrepareRename(state *state.ServerState, context *glsp.Context, params *protocol.PrepareRenameParams) (any, error) {
	state.Mu.RLock()
	defer state.Mu.RUnlock()

	uri := params.TextDocument.URI
	cursorLine := params.Position.Line

	docInfo, exists := state.Index[uri]
	if !exists {
		return nil, nil
	}

	// 1. Find the link under the cursor
	var targetLink *parser.ExtractedLink
	for i := range docInfo.Links {
		link := &docInfo.Links[i]
		if cursorLine >= link.Range.Start.Line && cursorLine <= link.Range.End.Line {
			targetLink = link
			break
		}
	}

	if targetLink == nil {
		return nil, nil // Not on a link, cancel the rename action entirely
	}

	// 2. Find the exact character columns of the path inside the line text
	lines := strings.Split(string(docInfo.Content), "\n")
	lineIdx := targetLink.Range.Start.Line

	if int(lineIdx) < len(lines) {
		lineText := lines[lineIdx]

		// Search for the exact string, e.g., "(docs/test.md)"
		searchStr := "(" + targetLink.Path + ")"
		idx := strings.Index(lineText, searchStr)

		if idx != -1 {
			// We found it! Calculate the exact start and end columns
			startChar := uint32(idx + 1) // +1 to skip the opening parenthesis '('
			endChar := startChar + uint32(len(targetLink.Path))

			exactRange := protocol.Range{
				Start: protocol.Position{Line: lineIdx, Character: startChar},
				End:   protocol.Position{Line: lineIdx, Character: endChar},
			}

			// Return a map containing both the range AND the explicit placeholder
			return map[string]any{
				"range":       exactRange,
				"placeholder": targetLink.Path, // Forces "docs/test.md" into the prompt
			}, nil
		}
	}

	// Fallback (if exact text wasn't found, still enforce the placeholder)
	return map[string]any{
		"range":       targetLink.Range,
		"placeholder": targetLink.Path,
	}, nil
}
