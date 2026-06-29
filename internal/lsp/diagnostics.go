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

		filePath := link.Path
		if idx := strings.Index(filePath, "#"); idx != -1 {
			filePath = filePath[:idx]
		}

		if filePath == "" {
			continue
		}

		targetAbsPath := sState.ResolveLinkPath(uri, filePath)
		targetURI := "file://" + targetAbsPath

		_, existsInIndex := indexKeys[targetURI]

		var statErr error
		existsOnDisk := false
		if !existsInIndex {
			if _, err := os.Stat(targetAbsPath); err == nil {
				existsOnDisk = true
			} else {
				statErr = err
			}
		}

		sState.Log(fmt.Sprintf("[Diagnostics] Link path: %s -> Abs: %s, URI: %s, InIndex: %v, OnDisk: %v (err: %v)",
			link.Path, targetAbsPath, targetURI, existsInIndex, existsOnDisk, statErr))

		if !existsInIndex && !existsOnDisk {
			severity := protocol.DiagnosticSeverityError
			source := "xsmd-lsp"
			message := "Broken link: file does not exist"

			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range:    link.Range,
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
	return strings.HasPrefix(path, "http://") ||
		strings.HasPrefix(path, "https://") ||
		strings.HasPrefix(path, "mailto:") ||
		strings.HasPrefix(path, "ftp://") ||
		strings.Contains(path, "://")
}
