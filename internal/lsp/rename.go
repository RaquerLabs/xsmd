package lsp

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RaquerLabs/xsmd/internal/parser"
	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

var DisableProcessSharedLock = false

// checkAndTrackRenameProcessShared checks if this rename was recently handled by any process.
// It returns true if it should be ignored (duplicate).
func checkAndTrackRenameProcessShared(state *state.ServerState, oldAbs, newAbs string) bool {
	if DisableProcessSharedLock {
		return false
	}
	// Generate a unique hash for the rename transaction
	hash := sha256.Sum256([]byte(oldAbs + "->" + newAbs))
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("xsmd-rename-%x.lock", hash))

	// Check if the lock file already exists and is recent (e.g. less than 5 seconds old)
	info, err := os.Stat(lockPath)
	if err == nil {
		if time.Since(info.ModTime()) < 5*time.Second {
			state.LogNoLock(fmt.Sprintf("[PID:%d] checkAndTrackRenameProcessShared: Lock file %s is recent (age: %v), ignoring", os.Getpid(), lockPath, time.Since(info.ModTime())))
			return true
		}
		// Clean up old stale lock file
		_ = os.Remove(lockPath)
	}

	// Try to create the lock file atomically
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			// If it was created in a race, double check its age
			info, errStat := os.Stat(lockPath)
			if errStat == nil && time.Since(info.ModTime()) < 5*time.Second {
				state.LogNoLock(fmt.Sprintf("[PID:%d] checkAndTrackRenameProcessShared: Lock file %s was created by another process, ignoring", os.Getpid(), lockPath))
				return true
			}
		}
		// Fallback: if we can't write, don't block the rename, but it's not locked.
		return false
	}
	f.Close()

	state.LogNoLock(fmt.Sprintf("[PID:%d] checkAndTrackRenameProcessShared: Created lock file %s", os.Getpid(), lockPath))

	// Schedule cleanup in our own process too (best effort)
	time.AfterFunc(5*time.Second, func() {
		_ = os.Remove(lockPath)
	})

	return false
}

// cleanURIPath converts a URI (which may have double or triple slashes, e.g. file:// or file:///)
// to a standardized absolute filesystem path.
// public for use within lsp package.
func cleanURIPath(uri string) string {
	p := uri
	prefixes := []string{"file://localhost", "file:///", "file://", "file:/", "file:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(p, prefix) {
			p = strings.TrimPrefix(p, prefix)
			break
		}
	}
	// On Windows, a URI might look like /C:/path. Trim the leading slash if followed by drive letter.
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' && ((p[1] >= 'a' && p[1] <= 'z') || (p[1] >= 'A' && p[1] <= 'Z')) {
		p = p[1:]
	}
	if !strings.HasPrefix(p, "/") && !filepath.IsAbs(p) {
		p = "/" + p
	}
	return filepath.Clean(p)
}

// HandleWorkspaceWillRenameFiles resolves files that are about to be renamed on disk and fixes broken links
func HandleWorkspaceWillRenameFiles(state *state.ServerState, context *glsp.Context, params *protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) {
	state.Mu.Lock()
	defer state.Mu.Unlock()

	state.LogNoLock(fmt.Sprintf("[PID:%d] HandleWorkspaceWillRenameFiles called with %d files", os.Getpid(), len(params.Files)))
	changes := make(map[string][]protocol.TextEdit)

	for _, fileRename := range params.Files {
		state.LogNoLock(fmt.Sprintf("[PID:%d] File rename entry: OldURI=%s, NewURI=%s", os.Getpid(), fileRename.OldURI, fileRename.NewURI))
		// Clean both URIs to ensure exact matching and avoid redundant/double updates
		oldAbs := cleanURIPath(fileRename.OldURI)
		newAbs := cleanURIPath(fileRename.NewURI)
		state.LogNoLock(fmt.Sprintf("[PID:%d] Cleaned paths: oldAbs=%s, newAbs=%s", os.Getpid(), oldAbs, newAbs))

		// Check if we already handled this rename as part of textDocument/rename or a duplicate trigger
		val, exists := state.ProcessedRenames[oldAbs]
		state.LogNoLock(fmt.Sprintf("[PID:%d] Lookup in ProcessedRenames: key=%s, exists=%t, val=%s", os.Getpid(), oldAbs, exists, val))
		if exists && val == newAbs {
			state.LogNoLock(fmt.Sprintf("[PID:%d] Match found in ProcessedRenames, ignoring rename for %s", os.Getpid(), oldAbs))
			continue
		}

		// Process-shared duplicate check
		if checkAndTrackRenameProcessShared(state, oldAbs, newAbs) {
			state.LogNoLock(fmt.Sprintf("[PID:%d] Match found in process-shared lock file, ignoring rename for %s", os.Getpid(), oldAbs))
			continue
		}

		// Track this rename so that subsequent duplicate triggers (e.g. from duplicate clients) are ignored.
		// It will be cleaned up when we receive file watch events (didChangeWatchedFiles / didDeleteFiles).
		state.LogNoLock(fmt.Sprintf("[PID:%d] Tracking rename in ProcessedRenames: %s -> %s", os.Getpid(), oldAbs, newAbs))
		state.ProcessedRenames[oldAbs] = newAbs

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

			for _, link := range docInfo.Links {
				linkPath := link.Path
				var anchor string
				if idx := strings.Index(linkPath, "#"); idx != -1 {
					anchor = linkPath[idx:]
					linkPath = linkPath[:idx]
				}

				var targetAbsPath string
				if strings.HasPrefix(linkPath, "/") {
					cleanPath := filepath.Clean(linkPath)
					cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
					cleanPath = strings.TrimPrefix(cleanPath, "/")
					targetAbsPath = filepath.Join(state.WorkspaceRoot, cleanPath)
				} else {
					sourceAbsPath := cleanURIPath(uri)
					sourceDir := filepath.Dir(sourceAbsPath)
					targetAbsPath = filepath.Clean(filepath.Join(sourceDir, linkPath))
				}

				if filepath.Clean(targetAbsPath) == filepath.Clean(oldAbs) {
					var newLinkPath string
					if strings.HasPrefix(link.Path, "/") {
						newLinkPath = "/" + newRel
					} else {
						sourceAbsPath := cleanURIPath(uri)
						sourceDir := filepath.Dir(sourceAbsPath)
						relPath, err := filepath.Rel(sourceDir, newAbs)
						if err == nil {
							newLinkPath = filepath.ToSlash(relPath)
						} else {
							newLinkPath = filepath.ToSlash(newRel) // Fallback
						}
					}

					newLinkPath = newLinkPath + anchor

					edits = append(edits, protocol.TextEdit{
						Range:   link.PathRange,
						NewText: newLinkPath,
					})
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
	state.Mu.Lock()
	defer state.Mu.Unlock()

	state.LogNoLock(fmt.Sprintf("[PID:%d] HandleTextDocumentRename called: URI=%s, line=%d, char=%d, NewName=%s", os.Getpid(), params.TextDocument.URI, params.Position.Line, params.Position.Character, params.NewName))
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

	targetPath := targetLink.Path
	if idx := strings.Index(targetPath, "#"); idx != -1 {
		targetPath = targetPath[:idx]
	}

	var oldAbsPath string
	if strings.HasPrefix(targetPath, "/") {
		cleanPath := filepath.Clean(targetPath)
		cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
		cleanPath = strings.TrimPrefix(cleanPath, "/")
		oldAbsPath = filepath.Join(state.WorkspaceRoot, cleanPath)
	} else {
		sourceAbsPath := cleanURIPath(uri)
		sourceDir := filepath.Dir(sourceAbsPath)
		oldAbsPath = filepath.Clean(filepath.Join(sourceDir, targetPath))
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

		for _, link := range indexDoc.Links {
			linkPath := link.Path
			var anchor string
			if idx := strings.Index(linkPath, "#"); idx != -1 {
				anchor = linkPath[idx:]
				linkPath = linkPath[:idx]
			}

			var linkAbsPath string
			if strings.HasPrefix(linkPath, "/") {
				cleanPath := filepath.Clean(linkPath)
				cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
				cleanPath = strings.TrimPrefix(cleanPath, "/")
				linkAbsPath = filepath.Join(state.WorkspaceRoot, cleanPath)
			} else {
				sourceAbsPath := cleanURIPath(indexUri)
				sourceDir := filepath.Dir(sourceAbsPath)
				linkAbsPath = filepath.Clean(filepath.Join(sourceDir, linkPath))
			}

			if filepath.Clean(linkAbsPath) == filepath.Clean(oldAbsPath) {
				var newLinkPath string
				if strings.HasPrefix(link.Path, "/") {
					relToRoot, err := filepath.Rel(state.WorkspaceRoot, newAbsPath)
					if err == nil {
						newLinkPath = "/" + filepath.ToSlash(relToRoot)
					} else {
						newLinkPath = "/" + filepath.ToSlash(newAbsPath)
					}
				} else {
					sourceAbsPath := cleanURIPath(indexUri)
					sourceDir := filepath.Dir(sourceAbsPath)
					relToDoc, err := filepath.Rel(sourceDir, newAbsPath)
					if err == nil {
						newLinkPath = filepath.ToSlash(relToDoc)
					} else {
						newLinkPath = filepath.ToSlash(newAbsPath)
					}
				}

				newLinkPath = newLinkPath + anchor

				edits = append(edits, protocol.TextEdit{
					Range:   link.PathRange,
					NewText: newLinkPath,
				})
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

	// Track this rename so that workspace/willRenameFiles is ignored for it
	state.LogNoLock(fmt.Sprintf("[PID:%d] TextDocumentRename tracking rename in ProcessedRenames: %s -> %s", os.Getpid(), oldAbsPath, newAbsPath))
	state.ProcessedRenames[oldAbsPath] = newAbsPath

	// Track this rename process-shared
	_ = checkAndTrackRenameProcessShared(state, oldAbsPath, newAbsPath)

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

	// Return targetLink.PathRange directly
	return map[string]any{
		"range":       targetLink.PathRange,
		"placeholder": targetLink.Path,
	}, nil
}
