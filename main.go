package main

import (
	"fmt" // <-- Added missing import
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/tliron/glsp/server"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type DocumentInfo struct {
	URI     string
	Content []byte
	AST     ast.Node
	Links   []ExtractedLink
	Title   string // Caches the file's primary # Title (or fallback)
	HasH1   bool   // <-- ADDED: Strictly tracks if an H1 exists
}

// ExtractedLink stores information about the links found in documents
type ExtractedLink struct {
	Path  string
	Range protocol.Range
}

// ServerState manages your global workspace memory
type ServerState struct {
	Mu            sync.RWMutex
	WorkspaceRoot string
	Index         map[string]*DocumentInfo
}

func main() {
	state := &ServerState{
		Index: make(map[string]*DocumentInfo),
	}

	prepareProvider := true
	capabilities := protocol.ServerCapabilities{
		TextDocumentSync:     protocol.TextDocumentSyncKindFull,
		DefinitionProvider:   true,
		ReferencesProvider:   true,
		FoldingRangeProvider: true,
		CompletionProvider: &protocol.CompletionOptions{
			TriggerCharacters: []string{"["},
		},
		RenameProvider: &protocol.RenameOptions{
			PrepareProvider: &prepareProvider,
		},
		Workspace: &protocol.ServerCapabilitiesWorkspace{
			FileOperations: &protocol.ServerCapabilitiesWorkspaceFileOperations{
				WillRename: &protocol.FileOperationRegistrationOptions{
					Filters: []protocol.FileOperationFilter{
						{Pattern: protocol.FileOperationPattern{Glob: "**/*.md"}},
						{Pattern: protocol.FileOperationPattern{Glob: "**/*.markdown"}},
					},
				},
			},
		},
	}

	// Define the LSP event handlers
	handler := protocol.Handler{
		Initialize: func(context *glsp.Context, params *protocol.InitializeParams) (any, error) {
			if params.RootURI != nil {
				startPath := strings.TrimPrefix(*params.RootURI, "file://")
				root, err := FindProjectRoot(startPath)
				if err == nil {
					state.WorkspaceRoot = root
				} else {
					state.WorkspaceRoot = startPath // Fallback
				}
			}

			// Kick off the workspace crawl asynchronously
			go func() {
				err := state.CrawlWorkspace()
				if err != nil {
					log.Printf("Error crawling workspace: %v", err)
				} else {
					log.Printf("Workspace successfully indexed! Found %d files.", len(state.Index))
				}
			}()

			return protocol.InitializeResult{Capabilities: capabilities}, nil
		},

		// Absorbs Neovim's post-handshake notification
		Initialized: func(context *glsp.Context, params *protocol.InitializedParams) error {
			log.Println("LSP client handshake completed successfully!")
			return nil
		},

		// Triggered when you open a file in Neovim
		TextDocumentDidOpen: func(context *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
			state.Mu.Lock()
			defer state.Mu.Unlock()

			uri := params.TextDocument.URI
			return state.ParseAndIndexContent(uri, []byte(params.TextDocument.Text))
		},

		// Triggered by Neovim to fetch collapsible regions (Headers AND Lists)

		// Triggered by Neovim to fetch collapsible regions (Headers AND Lists)
		TextDocumentFoldingRange: func(context *glsp.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
			state.Mu.RLock()
			defer state.Mu.RUnlock()

			uri := params.TextDocument.URI
			docInfo, exists := state.Index[uri]
			if !exists {
				return nil, nil
			}

			var lineOffsets []int
			lineOffsets = append(lineOffsets, 0)
			for i, b := range docInfo.Content {
				if b == '\n' {
					lineOffsets = append(lineOffsets, i+1)
				}
			}

			getLineFromOffset := func(offset int) uint32 {
				for lineNum, startOffset := range lineOffsets {
					if offset >= startOffset && (lineNum == len(lineOffsets)-1 || offset < lineOffsets[lineNum+1]) {
						return uint32(lineNum)
					}
				}
				return 0
			}

			var folds []protocol.FoldingRange
			totalLines := uint32(len(lineOffsets) - 1)

			type HeaderRange struct {
				Level     int
				StartLine uint32
			}
			var headers []HeaderRange

			// Walk the AST to gather information
			ast.Walk(docInfo.AST, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				if !entering {
					return ast.WalkContinue, nil
				}

				// --- 1. COLLECT HEADERS FOR CALCULATION ---
				if n.Kind() == ast.KindHeading {
					heading := n.(*ast.Heading)
					if heading.Lines().Len() > 0 {
						startByte := heading.Lines().At(0).Start
						headers = append(headers, HeaderRange{
							Level:     heading.Level,
							StartLine: getLineFromOffset(startByte),
						})
					}
				}

				// --- 2. FIXED: ONLY FOLD LIST ITEMS ---
				// By ignoring the overall list container, we prevent parent folds
				// from swallowing sibling bullets (like "Tasks for tomorrow").
				if n.Kind() == ast.KindListItem {
					var startByte, stopByte int = -1, -1

					// Walk the list item's descendants to find the true text boundaries
					ast.Walk(n, func(child ast.Node, childEntering bool) (ast.WalkStatus, error) {
						// Guardrail: Ensure the node is a Block type before calling .Lines()
						if child.Type() == ast.TypeBlock && child.Lines().Len() > 0 {
							first := child.Lines().At(0).Start
							last := child.Lines().At(child.Lines().Len() - 1).Stop

							if startByte == -1 || first < startByte {
								startByte = first
							}
							if last > stopByte {
								stopByte = last
							}
						}
						return ast.WalkContinue, nil
					})

					if startByte != -1 && stopByte != -1 {
						firstLine := getLineFromOffset(startByte)
						lastLine := getLineFromOffset(stopByte)

						// Only fold if the list item actually has nested children (spans multiple lines)
						if lastLine > firstLine {
							foldingKind := string(protocol.FoldingRangeKindRegion)
							folds = append(folds, protocol.FoldingRange{
								StartLine: firstLine,
								EndLine:   lastLine,
								Kind:      &foldingKind,
							})
						}
					}
				}

				return ast.WalkContinue, nil
			})

			// --- 3. COMPUTE HEADER BOUNDARIES (Existing Logic) ---
			for i, currentHeader := range headers {
				endLine := totalLines

				for j := i + 1; j < len(headers); j++ {
					nextHeader := headers[j]
					if nextHeader.Level <= currentHeader.Level {
						endLine = nextHeader.StartLine - 1
						break
					}
				}

				if endLine > currentHeader.StartLine {
					foldingKind := string(protocol.FoldingRangeKindRegion)
					folds = append(folds, protocol.FoldingRange{
						StartLine: currentHeader.StartLine,
						EndLine:   endLine,
						Kind:      &foldingKind,
					})
				}
			}

			return folds, nil
		},

		// Triggered on every keystroke/modification in Neovim
		TextDocumentDidChange: func(context *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
			state.Mu.Lock()
			defer state.Mu.Unlock()

			uri := params.TextDocument.URI

			if len(params.ContentChanges) > 0 {
				change := params.ContentChanges[0].(protocol.TextDocumentContentChangeEventWhole)
				return state.ParseAndIndexContent(uri, []byte(change.Text))
			}
			return nil
		},

		// Triggered when you hit 'gd' on a link in Neovim
		TextDocumentDefinition: func(context *glsp.Context, params *protocol.DefinitionParams) (any, error) {
			state.Mu.RLock()
			defer state.Mu.RUnlock()

			uri := params.TextDocument.URI
			cursorLine := params.Position.Line

			docInfo, exists := state.Index[uri]
			if !exists {
				return nil, nil
			}

			var targetLink *ExtractedLink
			for _, link := range docInfo.Links {
				if cursorLine >= link.Range.Start.Line && cursorLine <= link.Range.End.Line {
					targetLink = &link
					break
				}
			}

			if targetLink == nil {
				return nil, nil
			}

			cleanPath := filepath.Clean(targetLink.Path)
			cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
			cleanPath = strings.TrimPrefix(cleanPath, "/")

			targetAbsPath := filepath.Join(state.WorkspaceRoot, cleanPath)
			targetURI := "file://" + targetAbsPath

			return protocol.Location{
				URI: targetURI,
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 0},
				},
			}, nil
		},

		// Triggered when you hit 'gD' in Neovim
		TextDocumentReferences: func(context *glsp.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
			state.Mu.RLock()
			defer state.Mu.RUnlock()

			currentURI := params.TextDocument.URI

			currentAbsPath := strings.TrimPrefix(currentURI, "file://")
			currentRelPath, err := filepath.Rel(state.WorkspaceRoot, currentAbsPath)
			if err != nil {
				return nil, nil
			}

			locations := []protocol.Location{}

			for _, docInfo := range state.Index {
				for _, link := range docInfo.Links {

					cleanLinkPath := filepath.Clean(link.Path)
					cleanLinkPath = strings.TrimPrefix(cleanLinkPath, string(filepath.Separator))
					cleanLinkPath = strings.TrimPrefix(cleanLinkPath, "/")

					if cleanLinkPath == currentRelPath {
						locations = append(locations, protocol.Location{
							URI:   docInfo.URI,
							Range: link.Range,
						})
					}
				}
			}

			return locations, nil
		},

		// Triggered by Neovim whenever you demand auto-completions

		TextDocumentCompletion: func(context *glsp.Context, params *protocol.CompletionParams) (any, error) {
			state.Mu.RLock()
			defer state.Mu.RUnlock()

			var items []protocol.CompletionItem

			for uri, docInfo := range state.Index {
				if uri == params.TextDocument.URI {
					continue
				}

				absPath := strings.TrimPrefix(uri, "file://")
				relPath, err := filepath.Rel(state.WorkspaceRoot, absPath)
				if err != nil {
					continue
				}

				markdownLink := fmt.Sprintf("%s](%s)", docInfo.Title, relPath)

				// FIX: Store kinds and descriptions as local vars to pass pointers
				itemKind := protocol.CompletionItemKindFile
				itemDetail := relPath

				items = append(items, protocol.CompletionItem{
					Label:      docInfo.Title,
					Kind:       &itemKind,   // Pointer assignment
					Detail:     &itemDetail, // Pointer assignment
					InsertText: &markdownLink,
				})
			}

			return items, nil
		},

		// Triggered automatically when you rename a file in your file explorer
		// Triggered automatically when you rename a file in your file explorer
		WorkspaceWillRenameFiles: func(context *glsp.Context, params *protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) { // <-- CHANGED 'any' to '*protocol.WorkspaceEdit'
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

			// <-- CHANGED: Returning a pointer (&) to the struct
			return &protocol.WorkspaceEdit{
				Changes: changes,
			}, nil
		},

		// Triggered when you press your rename shortcut on a markdown link
		TextDocumentRename: func(context *glsp.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) { // <-- CHANGED 'any' to '*protocol.WorkspaceEdit'
			state.Mu.RLock()
			defer state.Mu.RUnlock()

			uri := params.TextDocument.URI
			cursorLine := params.Position.Line

			docInfo, exists := state.Index[uri]
			if !exists {
				return nil, nil
			}

			// Figure out which link we are trying to rename
			var targetLink *ExtractedLink
			for _, link := range docInfo.Links {
				if cursorLine >= link.Range.Start.Line && cursorLine <= link.Range.End.Line {
					targetLink = &link
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
			return &protocol.WorkspaceEdit{ // <-- CHANGED: Added pointer (&)
				DocumentChanges: docChanges,
			}, nil
		},

		// Triggered BEFORE the rename prompt appears to determine exactly what text to pre-fill
		TextDocumentPrepareRename: func(context *glsp.Context, params *protocol.PrepareRenameParams) (any, error) {
			state.Mu.RLock()
			defer state.Mu.RUnlock()

			uri := params.TextDocument.URI
			cursorLine := params.Position.Line

			docInfo, exists := state.Index[uri]
			if !exists {
				return nil, nil
			}

			// 1. Find the link under the cursor
			var targetLink *ExtractedLink
			for _, link := range docInfo.Links {
				if cursorLine >= link.Range.Start.Line && cursorLine <= link.Range.End.Line {
					targetLink = &link
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

					// THE FIX: Return a map containing both the range AND the explicit placeholder
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
		},

		// Triggered when you save a file in Neovim (:w)
		TextDocumentDidSave: func(context *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
			// State is already updated via DidChange.
			// Return nil to acknowledge the save and silence the JSON-RPC error.
			return nil
		},

		// Triggered when you close a buffer in Neovim
		TextDocumentDidClose: func(context *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
			// You could remove the file from the index here to save memory,
			// but for a workspace-aware wiki, it is usually better to keep it cached!
			return nil
		},
	}

	// Start standard I/O server
	s := server.NewServer(&handler, "gcgb-md-lsp", false)
	log.Fatal(s.RunStdio())
}

// FindProjectRoot looks upward for our anchor file
func FindProjectRoot(startPath string) (string, error) {
	current := filepath.Clean(startPath)
	for {
		markerPath := filepath.Join(current, "gcgb-md.toml")
		if _, err := os.Stat(markerPath); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}
		current = parent
	}
}

// CrawlWorkspace looks for all markdown files underneath the project root
func (s *ServerState) CrawlWorkspace() error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	return filepath.WalkDir(s.WorkspaceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (strings.HasSuffix(d.Name(), ".md") || strings.HasSuffix(d.Name(), ".markdown")) {
			uri := "file://" + path
			err := s.ParseAndIndexFile(uri, path)
			if err != nil {
				log.Printf("Failed to parse %s: %v", path, err)
			}
		}
		return nil
	})
}

// ParseAndIndexContent parses raw byte arrays directly without crashing on inline nodes
func (s *ServerState) ParseAndIndexContent(uri string, content []byte) error {
	// 1. THIS is the part that went missing!
	md := goldmark.New()
	reader := text.NewReader(content)
	doc := md.Parser().Parse(reader)

	var lineOffsets []int
	lineOffsets = append(lineOffsets, 0)
	for i, b := range content {
		if b == '\n' {
			lineOffsets = append(lineOffsets, i+1)
		}
	}

	getLineFromOffset := func(offset int) uint32 {
		for lineNum, startOffset := range lineOffsets {
			if offset >= startOffset && (lineNum == len(lineOffsets)-1 || offset < lineOffsets[lineNum+1]) {
				return uint32(lineNum)
			}
		}
		return 0
	}

	var extractedLinks []ExtractedLink
	var docTitle string
	var hasH1 bool // <-- The flag we added for autocomplete filtering

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		// Extract links
		if entering && n.Kind() == ast.KindLink {
			ln := n.(*ast.Link)
			destPath := string(ln.Destination)

			parent := n.Parent()
			for parent != nil && parent.Type() == ast.TypeInline {
				parent = parent.Parent()
			}

			var startLine uint32 = 0
			var endLine uint32 = 0

			if parent != nil && parent.Lines().Len() > 0 {
				firstSegment := parent.Lines().At(0)
				lastSegment := parent.Lines().At(parent.Lines().Len() - 1)

				startLine = getLineFromOffset(firstSegment.Start)
				endLine = getLineFromOffset(lastSegment.Stop)
			}

			extractedLinks = append(extractedLinks, ExtractedLink{
				Path: destPath,
				Range: protocol.Range{
					Start: protocol.Position{Line: startLine, Character: 0},
					End:   protocol.Position{Line: endLine, Character: 999},
				},
			})
		}

		// Extract the main H1 Title
		if entering && n.Kind() == ast.KindHeading {
			heading := n.(*ast.Heading)
			if heading.Level == 1 && docTitle == "" {
				var headingText strings.Builder
				for i := 0; i < heading.Lines().Len(); i++ {
					line := heading.Lines().At(i)
					headingText.Write(content[line.Start:line.Stop])
				}
				docTitle = strings.TrimSpace(headingText.String())
				hasH1 = true // <-- Record that a real header exists
			}
		}

		return ast.WalkContinue, nil
	})

	// Fallback title for files without an H1
	if docTitle == "" {
		docTitle = filepath.Base(strings.TrimPrefix(uri, "file://"))
	}

	s.Index[uri] = &DocumentInfo{
		URI:     uri,
		Content: content,
		AST:     doc,
		Links:   extractedLinks,
		Title:   docTitle,
		HasH1:   hasH1,
	}

	return nil
}

// ParseAndIndexFile handles reading from disk then indexing
func (s *ServerState) ParseAndIndexFile(uri string, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return s.ParseAndIndexContent(uri, content)
}
