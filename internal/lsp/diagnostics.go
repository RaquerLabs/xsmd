package lsp

import (
	"fmt"
	"os"
	"strings"

	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// PublishDiagnostics checks for broken links in the document and publishes diagnostics to the client
func PublishDiagnostics(sState *state.ServerState, context *glsp.Context, uri string) {
	sState.Mu.RLock()
	docInfo, exists := sState.Index[uri]
	if !exists {
		sState.Mu.RUnlock()
		return
	}

	// Snapshot all index keys under the read-lock to avoid acquiring locks repeatedly
	// or holding them during blocking disk I/O operations (os.Stat) inside the loop.
	indexKeys := make(map[string]struct{}, len(sState.Index))
	for k := range sState.Index {
		indexKeys[k] = struct{}{}
	}
	workspaceRoot := sState.WorkspaceRoot
	sState.Mu.RUnlock()

	sState.Log(fmt.Sprintf("[Diagnostics] Checking URI: %s (exists=%v), WorkspaceRoot: %s", uri, exists, workspaceRoot))

	diagnostics := []protocol.Diagnostic{}

	for _, link := range docInfo.Links {
		if isExternalLink(link.Path) {
			continue
		}

		filePath, _ := state.SplitAnchor(link.Path)
		if filePath == "" {
			continue
		}

		targetAbsPath := sState.ResolveLinkPath(uri, filePath)
		targetURI := "file://" + targetAbsPath

		// The map value is struct{}; presence is carried by the bool.
		_, existsInIndex := indexKeys[targetURI]
		existsOnDisk := true
		var statErr error
		if !existsInIndex {
			_, statErr = os.Stat(targetAbsPath)
			existsOnDisk = statErr == nil
		}

		sState.Log(fmt.Sprintf("[Diagnostics] Link path: %s -> Abs: %s, URI: %s, InIndex: %v, OnDisk: %v (err: %v)",
			link.Path, targetAbsPath, targetURI, existsInIndex, existsOnDisk, statErr))

		if !existsInIndex && !existsOnDisk {
			severity := protocol.DiagnosticSeverityError
			source := "xsmd-lsp"
			message := "Broken link: file does not exist"

			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range:    link.PathRange,
				Severity: &severity,
				Source:   &source,
				Message:  message,
			})
		}
	}

	params := &protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diagnostics,
	}

	if context != nil {
		context.Notify("textDocument/publishDiagnostics", params)
	}
}

func isExternalLink(path string) bool {
	// mailto: is the only scheme without :// in the URI.
	return strings.Contains(path, "://") || strings.HasPrefix(path, "mailto:")
}
