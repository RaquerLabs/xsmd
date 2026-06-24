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

	// 1. Fetch the current line from state index using params.TextDocument.URI
	var lines []string
	var hasLine bool
	var currentLine string
	var characterPos int

	doc, ok := state.Index[params.TextDocument.URI]
	if ok {
		lines = strings.Split(string(doc.Content), "\n")
		if int(params.Position.Line) < len(lines) {
			currentLine = lines[params.Position.Line]
			characterPos = int(params.Position.Character)
			if characterPos > len(currentLine) {
				characterPos = len(currentLine)
			}
			hasLine = true
		}
	}

	// 2. Look backward from the cursor to find the opening '['
	startChar := -1
	var query string
	if hasLine {
		for i := characterPos - 1; i >= 0; i-- {
			// If we see a closing bracket before an opening one, it means the
			// link is already closed (e.g., "[]"), so don't trigger anything.
			if currentLine[i] == ']' {
				break
			}

			if currentLine[i] == '[' {
				startChar = i
				break
			}
		}
		if startChar != -1 {
			query = strings.TrimSpace(currentLine[startChar+1 : characterPos])
		}
	}

	// If no valid open '[' is found behind the cursor, do not offer completions
	if startChar == -1 {
		return nil, nil
	}

	var items []protocol.CompletionItem
	queryLower := strings.ToLower(query)

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

		var relPathSlash string
		if strings.HasPrefix(relPath, "..") {
			relToRoot, err := filepath.Rel(state.WorkspaceRoot, absPath)
			if err != nil {
				continue
			}
			relPathSlash = "/" + filepath.ToSlash(relToRoot)
		} else {
			relPathSlash = filepath.ToSlash(relPath)
		}

		// Apply server-side filtering if query is not empty
		if queryLower != "" {
			titleLower := strings.ToLower(docInfo.Title)
			relPathLower := strings.ToLower(relPathSlash)
			if !strings.Contains(titleLower, queryLower) && !strings.Contains(relPathLower, queryLower) {
				continue
			}
		}

		// The text that actually gets inserted (fallback/InsertText)
		var markdownLink string
		if startChar != -1 {
			markdownLink = fmt.Sprintf("%s](%s)", docInfo.Title, relPathSlash)
		} else {
			markdownLink = fmt.Sprintf("[%s](%s)", docInfo.Title, relPathSlash)
		}

		// The client matches against the doc title inside the editRange
		filterText := docInfo.Title

		// Store kinds and descriptions as local vars to pass pointers safely
		itemKind := protocol.CompletionItemKindFile
		itemDetail := relPathSlash

		item := protocol.CompletionItem{
			Label:      docInfo.Title, // What shows in the UI dropdown
			FilterText: &filterText,   // What the editor uses to fuzzy-match behind the scenes
			Kind:       &itemKind,
			Detail:     &itemDetail,
			InsertText: &markdownLink, // What gets injected into the buffer
		}

		// If we found a valid open '[', we specify a TextEdit starting after the '['.
		if startChar != -1 {
			editRange := protocol.Range{
				Start: protocol.Position{Line: params.Position.Line, Character: uint32(startChar + 1)},
				End:   params.Position,
			}
			item.TextEdit = &protocol.TextEdit{
				Range:   editRange,
				NewText: markdownLink,
			}
		}

		items = append(items, item)
	}

	return protocol.CompletionList{
		IsIncomplete: true,
		Items:        items,
	}, nil
}
