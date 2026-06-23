package lsp

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/yourusername/gcgb-md/internal/parser"
	"github.com/yourusername/gcgb-md/internal/state"
	"github.com/yuin/goldmark/ast"
)

// BuildHandler constructs the full LSP handler set mapped to the ServerState
func BuildHandler(sState *state.ServerState) *protocol.Handler {
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

	handler := protocol.Handler{
		Initialize: func(context *glsp.Context, params *protocol.InitializeParams) (any, error) {
			if params.RootURI != nil {
				startPath := strings.TrimPrefix(*params.RootURI, "file://")
				root, err := state.FindProjectRoot(startPath)
				if err == nil {
					sState.WorkspaceRoot = root
				} else {
					sState.WorkspaceRoot = startPath // Fallback
				}
			}

			// Kick off the workspace crawl asynchronously
			go func() {
				err := sState.CrawlWorkspace()
				if err != nil {
					log.Printf("Error crawling workspace: %v", err)
				} else {
					sState.Mu.RLock()
					count := len(sState.Index)
					sState.Mu.RUnlock()
					log.Printf("Workspace successfully indexed! Found %d files.", count)
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
			uri := params.TextDocument.URI

			sState.Mu.Lock()
			err := sState.ParseAndIndexContent(uri, []byte(params.TextDocument.Text))
			sState.Mu.Unlock()

			if err == nil {
				PublishDiagnostics(sState, context, uri)
			}
			return err
		},

		// Triggered on every keystroke/modification in Neovim
		TextDocumentDidChange: func(context *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
			uri := params.TextDocument.URI

			if len(params.ContentChanges) > 0 {
				change := params.ContentChanges[0].(protocol.TextDocumentContentChangeEventWhole)

				sState.Mu.Lock()
				err := sState.ParseAndIndexContent(uri, []byte(change.Text))
				sState.Mu.Unlock()

				if err == nil {
					PublishDiagnostics(sState, context, uri)
				}
				return err
			}
			return nil
		},

		// Triggered by Neovim to fetch collapsible regions (Headers AND Lists)
		TextDocumentFoldingRange: func(context *glsp.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
			sState.Mu.RLock()
			defer sState.Mu.RUnlock()

			uri := params.TextDocument.URI
			docInfo, exists := sState.Index[uri]
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
			_ = ast.Walk(docInfo.AST, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
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
					_ = ast.Walk(n, func(child ast.Node, childEntering bool) (ast.WalkStatus, error) {
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

		// Triggered when you hit 'gd' on a link in Neovim
		TextDocumentDefinition: func(context *glsp.Context, params *protocol.DefinitionParams) (any, error) {
			sState.Mu.RLock()
			defer sState.Mu.RUnlock()

			uri := params.TextDocument.URI
			cursorLine := params.Position.Line

			docInfo, exists := sState.Index[uri]
			if !exists {
				return nil, nil
			}

			var targetLink *parser.ExtractedLink
			for i := range docInfo.Links {
				link := &docInfo.Links[i]
				if cursorLine >= link.Range.Start.Line && cursorLine <= link.Range.End.Line {
					targetLink = link
					break
				}
			}

			if targetLink == nil {
				return nil, nil
			}

			cleanPath := filepath.Clean(targetLink.Path)
			cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
			cleanPath = strings.TrimPrefix(cleanPath, "/")

			targetAbsPath := filepath.Join(sState.WorkspaceRoot, cleanPath)
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
			sState.Mu.RLock()
			defer sState.Mu.RUnlock()

			currentURI := params.TextDocument.URI

			currentAbsPath := strings.TrimPrefix(currentURI, "file://")
			currentRelPath, err := filepath.Rel(sState.WorkspaceRoot, currentAbsPath)
			if err != nil {
				return nil, nil
			}

			locations := []protocol.Location{}

			for _, docInfo := range sState.Index {
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

		// Triggered when you save a file in Neovim (:w)
		TextDocumentDidSave: func(context *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
			return nil
		},

		// Triggered when you close a buffer in Neovim
		TextDocumentDidClose: func(context *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
			return nil
		},

		// Delegation handlers
		TextDocumentCompletion: func(context *glsp.Context, params *protocol.CompletionParams) (any, error) {
			return HandleTextDocumentCompletion(sState, context, params)
		},

		WorkspaceWillRenameFiles: func(context *glsp.Context, params *protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) {
			return HandleWorkspaceWillRenameFiles(sState, context, params)
		},

		TextDocumentRename: func(context *glsp.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
			return HandleTextDocumentRename(sState, context, params)
		},

		TextDocumentPrepareRename: func(context *glsp.Context, params *protocol.PrepareRenameParams) (any, error) {
			return HandleTextDocumentPrepareRename(sState, context, params)
		},
	}

	return &handler
}
