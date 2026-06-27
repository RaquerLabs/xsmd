package lsp

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// PublishDiagnostics checks for broken links in the document and publishes diagnostics to the client
func PublishDiagnostics(sState *state.ServerState, context *glsp.Context, uri string) {
	sState.Mu.RLock()
	docInfo, exists := sState.Index[uri]
	workspaceRoot := sState.WorkspaceRoot
	sState.Mu.RUnlock()

	log.Printf("[Diagnostics] Checking URI: %s (exists=%v), WorkspaceRoot: %s", uri, exists, workspaceRoot)

	if !exists {
		return
	}

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

		var targetAbsPath string
		if strings.HasPrefix(filePath, "/") {
			cleanPath := filepath.Clean(filePath)
			cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
			cleanPath = strings.TrimPrefix(cleanPath, "/")
			targetAbsPath = filepath.Join(workspaceRoot, cleanPath)
		} else {
			sourceAbsPath := strings.TrimPrefix(uri, "file://")
			sourceDir := filepath.Dir(sourceAbsPath)
			targetAbsPath = filepath.Clean(filepath.Join(sourceDir, filePath))
		}
		targetURI := "file://" + targetAbsPath

		// First, check in-memory cache
		sState.Mu.RLock()
		_, existsInIndex := sState.Index[targetURI]
		sState.Mu.RUnlock()

		var statErr error
		existsOnDisk := false
		if !existsInIndex {
			if _, err := os.Stat(targetAbsPath); err == nil {
				existsOnDisk = true
			} else {
				statErr = err
			}
		}

		log.Printf("[Diagnostics] Link path: %s -> Abs: %s, URI: %s, InIndex: %v, OnDisk: %v (err: %v)",
			link.Path, targetAbsPath, targetURI, existsInIndex, existsOnDisk, statErr)

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
