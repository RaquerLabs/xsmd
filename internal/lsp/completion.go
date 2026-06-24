package lsp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/RaquerLabs/xsmd/internal/state"
)

// HandleTextDocumentCompletion resolves autocomplete options when typing '[' in Markdown
func HandleTextDocumentCompletion(state *state.ServerState, context *glsp.Context, params *protocol.CompletionParams) (any, error) {
	state.Mu.RLock()
	defer state.Mu.RUnlock()

	var items []protocol.CompletionItem

	for uri, docInfo := range state.Index {
		if uri == params.TextDocument.URI {
			continue
		}

		absPath := strings.TrimPrefix(uri, "file://")
		sourceAbsPath := strings.TrimPrefix(params.TextDocument.URI, "file://")
		sourceDir := filepath.Dir(sourceAbsPath)
		relPath, err := filepath.Rel(sourceDir, absPath)
		if err != nil {
			continue
		}

		if strings.HasPrefix(relPath, "..") {
			relToRoot, err := filepath.Rel(state.WorkspaceRoot, absPath)
			if err != nil {
				continue
			}
			relPath = "/" + filepath.ToSlash(relToRoot)
		} else {
			relPath = filepath.ToSlash(relPath)
		}

		// The text that actually gets inserted after the '[' you typed
		markdownLink := fmt.Sprintf("%s](%s)", docInfo.Title, relPath)

		// THE FIX: Prefix the filter text with '[' so the client's fuzzy finder
		// doesn't immediately filter out the results when you type a bracket.
		filterText := "[" + docInfo.Title

		// Store kinds and descriptions as local vars to pass pointers
		itemKind := protocol.CompletionItemKindFile
		itemDetail := relPath

		items = append(items, protocol.CompletionItem{
			Label:      docInfo.Title, // What shows in the UI dropdown
			FilterText: &filterText,   // What the editor uses to fuzzy-match behind the scenes
			Kind:       &itemKind,
			Detail:     &itemDetail,
			InsertText: &markdownLink, // What gets injected into the buffer
		})
	}

	return items, nil
}
