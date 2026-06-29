package lsp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// HandleTextDocumentCompletion resolves autocomplete options when typing '[' in Markdown
func HandleTextDocumentCompletion(state *state.ServerState, context *glsp.Context, params *protocol.CompletionParams) (any, error) {
	state.Log(fmt.Sprintf("HandleTextDocumentCompletion: URI=%s, Line=%d, Char=%d", params.TextDocument.URI, params.Position.Line, params.Position.Character))

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
			query = currentLine[startChar+1 : characterPos]
		}
	}

	// If no valid open '[' is found behind the cursor, do not offer completions
	if startChar == -1 {
		state.LogNoLock("HandleTextDocumentCompletion: No open '[' found before cursor. Returning nil.")
		return nil, nil
	}

	items := []protocol.CompletionItem{}
	var queryFiltered bool
	var queryCleaned string
	if strings.TrimSpace(query) != "" {
		queryCleaned = strings.TrimSpace(query)
		queryFiltered = true
	}

	state.LogNoLock(fmt.Sprintf("HandleTextDocumentCompletion: Query found: '%s' (cleaned: '%s', filtered: %v)", query, queryCleaned, queryFiltered))

	for uri, docInfo := range state.Index {
		if uri == params.TextDocument.URI {
			continue
		}

		absPath := strings.TrimPrefix(uri, "file://")
		relToRoot, err := filepath.Rel(state.WorkspaceRoot, absPath)
		if err == nil && state.IsIgnored(relToRoot) {
			state.LogNoLock(fmt.Sprintf("HandleTextDocumentCompletion: Skipping ignored document '%s'", uri))
			continue
		}

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

		// Apply fuzzy filtering if a query is present
		if queryFiltered {
			basename := filepath.Base(relPathSlash)
			if strings.Contains(queryCleaned, "/") {
				// If query has a slash, match against Title or full relative path
				if !fuzzyMatch(docInfo.Title, queryCleaned) && !fuzzyMatch(relPathSlash, queryCleaned) {
					continue
				}
			} else {
				// Otherwise, match against Title or Basename only to avoid folder-prefix false positives
				if !fuzzyMatch(docInfo.Title, queryCleaned) && !fuzzyMatch(basename, queryCleaned) {
					continue
				}
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

	state.LogNoLock(fmt.Sprintf("HandleTextDocumentCompletion: Returning %d items", len(items)))
	return protocol.CompletionList{
		IsIncomplete: len(items) > 0,
		Items:        items,
	}, nil
}

// fuzzyMatch checks if each space-separated term in the query matches the target string as a subsequence case-insensitively.
func fuzzyMatch(target, query string) bool {
	words := strings.Fields(query)
	if len(words) == 0 {
		return true
	}

	for _, word := range words {
		if !subsequenceMatch(target, word) {
			return false
		}
	}
	return true
}

// subsequenceMatch checks if the word subsequence matches the target string case-insensitively.
func subsequenceMatch(target, word string) bool {
	target = strings.ToLower(target)
	word = strings.ToLower(word)

	tIdx := 0
	for wIdx := 0; wIdx < len(word); wIdx++ {
		wChar := word[wIdx]
		found := false
		for ; tIdx < len(target); tIdx++ {
			if target[tIdx] == wChar {
				found = true
				tIdx++
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
