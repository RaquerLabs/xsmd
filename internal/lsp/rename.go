package lsp

import (
	"path/filepath"
	"strings"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/RaquerLabs/xsmd/internal/parser"
	"github.com/RaquerLabs/xsmd/internal/state"
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
				var targetAbsPath string
				if strings.HasPrefix(link.Path, "/") {
					cleanPath := filepath.Clean(link.Path)
					cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
					cleanPath = strings.TrimPrefix(cleanPath, "/")
					targetAbsPath = filepath.Join(state.WorkspaceRoot, cleanPath)
				} else {
					sourceAbsPath := strings.TrimPrefix(uri, "file://")
					sourceDir := filepath.Dir(sourceAbsPath)
					targetAbsPath = filepath.Clean(filepath.Join(sourceDir, link.Path))
				}

				if filepath.Clean(targetAbsPath) == filepath.Clean(oldAbs) {
					lineIdx := link.Range.Start.Line
					if int(lineIdx) < len(lines) {
						oldLineText := lines[lineIdx]

						var newLinkPath string
						if strings.HasPrefix(link.Path, "/") {
							newLinkPath = "/" + newRel
						} else {
							sourceAbsPath := strings.TrimPrefix(uri, "file://")
							sourceDir := filepath.Dir(sourceAbsPath)
							relPath, err := filepath.Rel(sourceDir, newAbs)
							if err == nil {
								newLinkPath = filepath.ToSlash(relPath)
							} else {
								newLinkPath = filepath.ToSlash(newRel) // Fallback
							}
						}

						newLineText := strings.Replace(oldLineText, "("+link.Path+")", "("+newLinkPath+")", 1)

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
			if link.Range.Start.Line == link.Range.End.Line {
				if params.Position.Character >= link.Range.Start.Character && params.Position.Character <= link.Range.End.Character {
					targetLink = link
					break
				}
			} else {
				onStartLine := cursorLine == link.Range.Start.Line
				onEndLine := cursorLine == link.Range.End.Line
				if (!onStartLine || params.Position.Character >= link.Range.Start.Character) &&
					(!onEndLine || params.Position.Character <= link.Range.End.Character) {
					targetLink = link
					break
				}
			}
		}
	}

	if targetLink == nil {
		for i := range docInfo.Links {
			link := &docInfo.Links[i]
			if cursorLine >= link.Range.Start.Line && cursorLine <= link.Range.End.Line {
				targetLink = link
				break
			}
		}
	}

	if targetLink == nil {
		return nil, nil // Not on a link, ignore
	}

	var oldAbsPath string
	if strings.HasPrefix(targetLink.Path, "/") {
		cleanPath := filepath.Clean(targetLink.Path)
		cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
		cleanPath = strings.TrimPrefix(cleanPath, "/")
		oldAbsPath = filepath.Join(state.WorkspaceRoot, cleanPath)
	} else {
		sourceAbsPath := strings.TrimPrefix(uri, "file://")
		sourceDir := filepath.Dir(sourceAbsPath)
		oldAbsPath = filepath.Clean(filepath.Join(sourceDir, targetLink.Path))
	}

	newNameCleaned := filepath.Clean(params.NewName)
	if !strings.HasSuffix(newNameCleaned, ".md") && !strings.HasSuffix(newNameCleaned, ".markdown") {
		newNameCleaned += ".md"
	}

	var newAbsPath string
	if strings.HasPrefix(params.NewName, "/") {
		cleanPath := strings.TrimPrefix(newNameCleaned, string(filepath.Separator))
		cleanPath = strings.TrimPrefix(cleanPath, "/")
		newAbsPath = filepath.Join(state.WorkspaceRoot, cleanPath)
	} else {
		oldDir := filepath.Dir(oldAbsPath)
		newAbsPath = filepath.Clean(filepath.Join(oldDir, newNameCleaned))
	}

	var docChanges []any

	// 1. Find all documents that link to the old path and queue up text edits
	for indexUri, indexDoc := range state.Index {
		var edits []any
		lines := strings.Split(string(indexDoc.Content), "\n")

		for _, link := range indexDoc.Links {
			var linkAbsPath string
			if strings.HasPrefix(link.Path, "/") {
				cleanPath := filepath.Clean(link.Path)
				cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
				cleanPath = strings.TrimPrefix(cleanPath, "/")
				linkAbsPath = filepath.Join(state.WorkspaceRoot, cleanPath)
			} else {
				sourceAbsPath := strings.TrimPrefix(indexUri, "file://")
				sourceDir := filepath.Dir(sourceAbsPath)
				linkAbsPath = filepath.Clean(filepath.Join(sourceDir, link.Path))
			}

			if filepath.Clean(linkAbsPath) == filepath.Clean(oldAbsPath) {
				lineIdx := link.Range.Start.Line
				if int(lineIdx) < len(lines) {
					oldLineText := lines[lineIdx]

					var newLinkPath string
					if strings.HasPrefix(link.Path, "/") {
						relToRoot, err := filepath.Rel(state.WorkspaceRoot, newAbsPath)
						if err == nil {
							newLinkPath = "/" + filepath.ToSlash(relToRoot)
						} else {
							newLinkPath = "/" + filepath.ToSlash(newAbsPath)
						}
					} else {
						sourceAbsPath := strings.TrimPrefix(indexUri, "file://")
						sourceDir := filepath.Dir(sourceAbsPath)
						relToDoc, err := filepath.Rel(sourceDir, newAbsPath)
						if err == nil {
							newLinkPath = filepath.ToSlash(relToDoc)
						} else {
							newLinkPath = filepath.ToSlash(newAbsPath)
						}
					}

					// Strictly replace the path within the parentheses to avoid false positives
					newLineText := strings.Replace(oldLineText, "("+link.Path+")", "("+newLinkPath+")", 1)

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
			if link.Range.Start.Line == link.Range.End.Line {
				if params.Position.Character >= link.Range.Start.Character && params.Position.Character <= link.Range.End.Character {
					targetLink = link
					break
				}
			} else {
				onStartLine := cursorLine == link.Range.Start.Line
				onEndLine := cursorLine == link.Range.End.Line
				if (!onStartLine || params.Position.Character >= link.Range.Start.Character) &&
					(!onEndLine || params.Position.Character <= link.Range.End.Character) {
					targetLink = link
					break
				}
			}
		}
	}

	if targetLink == nil {
		for i := range docInfo.Links {
			link := &docInfo.Links[i]
			if cursorLine >= link.Range.Start.Line && cursorLine <= link.Range.End.Line {
				targetLink = link
				break
			}
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
