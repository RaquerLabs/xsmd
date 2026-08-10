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
// It returns true if it should be ignored (duplicate), and a cleanup function to release the lock.
func checkAndTrackRenameProcessShared(sState *state.ServerState, oldAbs, newAbs string) (bool, func()) {
	if DisableProcessSharedLock {
		return false, func() {}
	}
	// Generate a unique hash for the rename transaction
	hash := sha256.Sum256([]byte(oldAbs + "->" + newAbs))
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("xsmd-rename-%x.lock", hash))

	// Check if the lock file already exists and is recent (e.g. less than 5 seconds old)
	info, err := os.Stat(lockPath)
	if err == nil {
		if time.Since(info.ModTime()) < 5*time.Second {
			sState.LogNoLock(fmt.Sprintf("[PID:%d] checkAndTrackRenameProcessShared: Lock file %s is recent (age: %v), ignoring", os.Getpid(), lockPath, time.Since(info.ModTime())))
			return true, func() {}
		}
		_ = os.Remove(lockPath)
	}

	// Try to create the lock file atomically
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			// If it was created in a race, double check its age
			info, errStat := os.Stat(lockPath)
			if errStat == nil && time.Since(info.ModTime()) < 5*time.Second {
				sState.LogNoLock(fmt.Sprintf("[PID:%d] checkAndTrackRenameProcessShared: Lock file %s was created by another process, ignoring", os.Getpid(), lockPath))
				return true, func() {}
			}
		}
		// Fallback: if we can't write, don't block the rename, but it's not locked.
		return false, func() {}
	}
	f.Close()

	sState.LogNoLock(fmt.Sprintf("[PID:%d] checkAndTrackRenameProcessShared: Created lock file %s", os.Getpid(), lockPath))

	cleanup := func() {
		sState.LogNoLock(fmt.Sprintf("[PID:%d] checkAndTrackRenameProcessShared: Removing lock file %s", os.Getpid(), lockPath))
		_ = os.Remove(lockPath)
	}

	return false, cleanup
}

// HandleWorkspaceWillRenameFiles resolves files that are about to be renamed on disk and fixes broken links
func HandleWorkspaceWillRenameFiles(sState *state.ServerState, context *glsp.Context, params *protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) {
	sState.Mu.Lock()
	defer sState.Mu.Unlock()

	sState.LogNoLock(fmt.Sprintf("[PID:%d] HandleWorkspaceWillRenameFiles called with %d files", os.Getpid(), len(params.Files)))
	changes := make(map[string][]protocol.TextEdit)

	for _, fileRename := range params.Files {
		sState.LogNoLock(fmt.Sprintf("[PID:%d] File rename entry: OldURI=%s, NewURI=%s", os.Getpid(), fileRename.OldURI, fileRename.NewURI))
		// Clean both URIs to ensure exact matching and avoid redundant/double updates
		oldAbs := state.CleanURIPath(fileRename.OldURI)
		newAbs := state.CleanURIPath(fileRename.NewURI)
		sState.LogNoLock(fmt.Sprintf("[PID:%d] Cleaned paths: oldAbs=%s, newAbs=%s", os.Getpid(), oldAbs, newAbs))

		// Check if we already handled this rename as part of textDocument/rename or a duplicate trigger
		val, exists := sState.ProcessedRenames[oldAbs]
		sState.LogNoLock(fmt.Sprintf("[PID:%d] Lookup in ProcessedRenames: key=%s, exists=%t, val=%s", os.Getpid(), oldAbs, exists, val))
		if exists && val == newAbs {
			sState.LogNoLock(fmt.Sprintf("[PID:%d] Match found in ProcessedRenames, ignoring rename for %s", os.Getpid(), oldAbs))
			delete(sState.ProcessedRenames, oldAbs)
			continue
		}

		// Process-shared duplicate check
		ignore, cleanup := checkAndTrackRenameProcessShared(sState, oldAbs, newAbs)
		if ignore {
			sState.LogNoLock(fmt.Sprintf("[PID:%d] Match found in process-shared lock file, ignoring rename for %s", os.Getpid(), oldAbs))
			continue
		}
		defer cleanup()

		// Track this rename so that subsequent duplicate triggers (e.g. from duplicate clients) are ignored.
		// It will be cleaned up when we receive file watch events (didChangeWatchedFiles / didDeleteFiles) or when matched.
		sState.LogNoLock(fmt.Sprintf("[PID:%d] Tracking rename in ProcessedRenames: %s -> %s", os.Getpid(), oldAbs, newAbs))
		sState.ProcessedRenames[oldAbs] = newAbs
		delete(sState.ProcessedRenames, newAbs) // Clear stale target path history to allow immediate rename back

		oldRel, err1 := filepath.Rel(sState.WorkspaceRoot, oldAbs)
		newRel, err2 := filepath.Rel(sState.WorkspaceRoot, newAbs)

		if err1 != nil || err2 != nil {
			continue // Failsafe if paths escape the workspace
		}

		oldRel = filepath.ToSlash(oldRel)
		newRel = filepath.ToSlash(newRel)

		// Sweep the index to find broken links and patch them
		for uri, docInfo := range sState.Index {
			var edits []protocol.TextEdit

			for _, link := range docInfo.Links {
				linkPath, anchor := state.SplitAnchor(link.Path)

				targetAbsPath := sState.ResolveLinkPath(uri, linkPath)

				if filepath.Clean(targetAbsPath) == filepath.Clean(oldAbs) {
					var newLinkPath string
					if strings.HasPrefix(link.Path, "/") {
						newLinkPath = "/" + newRel
					} else {
						sourceAbsPath := state.CleanURIPath(uri)
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
func HandleTextDocumentRename(sState *state.ServerState, context *glsp.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	sState.Mu.Lock()
	defer sState.Mu.Unlock()

	sState.LogNoLock(fmt.Sprintf("[PID:%d] HandleTextDocumentRename called: URI=%s, line=%d, char=%d, NewName=%s", os.Getpid(), params.TextDocument.URI, params.Position.Line, params.Position.Character, params.NewName))
	uri := params.TextDocument.URI

	docInfo, exists := sState.Index[uri]
	if !exists {
		return nil, nil
	}

	targetLink := parser.FindLinkAtPosition(docInfo.Links, params.Position)
	if targetLink == nil {
		return nil, nil // Not on a link, ignore
	}

	oldAbsPath := sState.ResolveLinkPath(uri, targetLink.Path)

	newNameCleaned := filepath.Clean(params.NewName)
	if !state.IsMarkdownPath(newNameCleaned) {
		newNameCleaned += ".md"
	}

	var newAbsPath string
	if strings.HasPrefix(params.NewName, "/") {
		cleanPath := strings.TrimPrefix(newNameCleaned, string(filepath.Separator))
		cleanPath = strings.TrimPrefix(cleanPath, "/")
		newAbsPath = filepath.Join(sState.WorkspaceRoot, cleanPath)
	} else {
		oldDir := filepath.Dir(oldAbsPath)
		newAbsPath = filepath.Clean(filepath.Join(oldDir, newNameCleaned))
	}

	var docChanges []any

	// 1. Find all documents that link to the old path and queue up text edits
	for indexUri, indexDoc := range sState.Index {
		var edits []any

		for _, link := range indexDoc.Links {
			linkPath, anchor := state.SplitAnchor(link.Path)

			linkAbsPath := sState.ResolveLinkPath(indexUri, linkPath)

			if filepath.Clean(linkAbsPath) == filepath.Clean(oldAbsPath) {
				var newLinkPath string
				if strings.HasPrefix(link.Path, "/") {
					relToRoot, err := filepath.Rel(sState.WorkspaceRoot, newAbsPath)
					if err == nil {
						newLinkPath = "/" + filepath.ToSlash(relToRoot)
					} else {
						newLinkPath = "/" + filepath.ToSlash(newAbsPath)
					}
				} else {
					sourceAbsPath := state.CleanURIPath(indexUri)
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
	sState.LogNoLock(fmt.Sprintf("[PID:%d] TextDocumentRename tracking rename in ProcessedRenames: %s -> %s", os.Getpid(), oldAbsPath, newAbsPath))
	sState.ProcessedRenames[oldAbsPath] = newAbsPath
	delete(sState.ProcessedRenames, newAbsPath) // Clear stale target path history to allow immediate rename back

	// Track this rename process-shared
	_, cleanup := checkAndTrackRenameProcessShared(sState, oldAbsPath, newAbsPath)
	defer cleanup()

	// Ship the combined edits + file move back to Neovim to execute
	return &protocol.WorkspaceEdit{
		DocumentChanges: docChanges,
	}, nil
}

// HandleTextDocumentPrepareRename determines the text range to select and display when starting a rename action
func HandleTextDocumentPrepareRename(sState *state.ServerState, context *glsp.Context, params *protocol.PrepareRenameParams) (any, error) {
	sState.Mu.RLock()
	defer sState.Mu.RUnlock()

	uri := params.TextDocument.URI

	docInfo, exists := sState.Index[uri]
	if !exists {
		return nil, nil
	}

	targetLink := parser.FindLinkAtPosition(docInfo.Links, params.Position)
	if targetLink == nil {
		return nil, nil // Not on a link, cancel the rename action entirely
	}

	// Return targetLink.PathRange directly
	return map[string]any{
		"range":       targetLink.PathRange,
		"placeholder": targetLink.Path,
	}, nil
}
