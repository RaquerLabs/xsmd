package lsp

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/RaquerLabs/xsmd/internal/parser"
	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
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
			TriggerCharacters: []string{"[", " ", "("},
		},
		RenameProvider: &protocol.RenameOptions{
			PrepareProvider: &prepareProvider,
		},
		ExecuteCommandProvider: &protocol.ExecuteCommandOptions{
			Commands: []string{"xsmd.dumpState"},
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

			sState.LoadConfig()

			// Kick off the workspace crawl asynchronously
			go func() {
				err := sState.CrawlWorkspace()
				if err != nil {
					log.Printf("Error crawling workspace: %v", err)
				} else {
					sState.Mu.RLock()
					count := len(sState.Index)
					sState.Mu.RUnlock()
					sState.Log(fmt.Sprintf("Workspace successfully indexed! Found %d files.", count))
				}
			}()

			return protocol.InitializeResult{Capabilities: capabilities}, nil
		},

		// Absorbs Neovim's post-handshake notification
		Initialized: func(context *glsp.Context, params *protocol.InitializedParams) error {
			sState.Log("LSP client handshake completed successfully!")

			watchers := []protocol.FileSystemWatcher{
				{GlobPattern: "**/*.md"},
				{GlobPattern: "**/*.markdown"},
			}
			regParams := protocol.RegistrationParams{
				Registrations: []protocol.Registration{
					{
						ID:     "xsmd-file-watcher",
						Method: "workspace/didChangeWatchedFiles",
						RegisterOptions: protocol.DidChangeWatchedFilesRegistrationOptions{
							Watchers: watchers,
						},
					},
				},
			}

			go func() {
				var result any
				context.Call("client/registerCapability", regParams, &result)
			}()

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

			lineOffsets := parser.NewLineOffsetTable(docInfo.Content)
			getLineFromOffset := lineOffsets.GetLineFromOffset

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

			docInfo, exists := sState.Index[uri]
			if !exists {
				return nil, nil
			}

			targetLink := parser.FindLinkAtPosition(docInfo.Links, params.Position)
			if targetLink == nil {
				return nil, nil
			}

			targetAbsPath := sState.ResolveLinkPath(uri, targetLink.Path)
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

			locations := []protocol.Location{}

			for _, docInfo := range sState.Index {
				for _, link := range docInfo.Links {
					if strings.HasPrefix(link.Path, "#") || link.Path == "" {
						continue
					}
					targetAbsPath := sState.ResolveLinkPath(docInfo.URI, link.Path)

					if filepath.Clean(targetAbsPath) == filepath.Clean(currentAbsPath) {
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
			uri := params.TextDocument.URI
			if !strings.HasSuffix(uri, ".md") && !strings.HasSuffix(uri, ".markdown") {
				return nil
			}

			path := strings.TrimPrefix(uri, "file://")

			sState.Mu.Lock()
			err := sState.ParseAndIndexFile(uri, path)
			sState.Mu.Unlock()

			if err == nil {
				PublishDiagnostics(sState, context, uri)
			}
			return err
		},

		// Triggered when you close a buffer in Neovim
		TextDocumentDidClose: func(context *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
			return nil
		},

		WorkspaceDidChangeWatchedFiles: func(context *glsp.Context, params *protocol.DidChangeWatchedFilesParams) error {
			sState.Mu.Lock()
			defer sState.Mu.Unlock()

			for _, change := range params.Changes {
				uri := change.URI
				if !strings.HasSuffix(uri, ".md") && !strings.HasSuffix(uri, ".markdown") {
					continue
				}
				path := strings.TrimPrefix(uri, "file://")

				switch change.Type {
				case protocol.FileChangeTypeCreated, protocol.FileChangeTypeChanged:
					sState.LogNoLock(fmt.Sprintf("File watch event: Created/Changed %s", path))
					err := sState.ParseAndIndexFile(uri, path)
					if err != nil {
						log.Printf("Failed to parse watched file %s: %v", path, err)
					}
				case protocol.FileChangeTypeDeleted:
					sState.LogNoLock(fmt.Sprintf("File watch event: Deleted %s", path))
					delete(sState.Index, uri)
					delete(sState.ProcessedRenames, state.CleanURIPath(uri))
				}
			}
			return nil
		},

		WorkspaceDidCreateFiles: func(context *glsp.Context, params *protocol.CreateFilesParams) error {
			sState.Mu.Lock()
			defer sState.Mu.Unlock()

			for _, file := range params.Files {
				uri := file.URI
				if !strings.HasSuffix(uri, ".md") && !strings.HasSuffix(uri, ".markdown") {
					continue
				}
				path := strings.TrimPrefix(uri, "file://")
				err := sState.ParseAndIndexFile(uri, path)
				if err != nil {
					log.Printf("Failed to parse created file %s: %v", path, err)
				}
			}
			return nil
		},

		WorkspaceDidDeleteFiles: func(context *glsp.Context, params *protocol.DeleteFilesParams) error {
			sState.Mu.Lock()
			defer sState.Mu.Unlock()

			for _, file := range params.Files {
				uri := file.URI
				if !strings.HasSuffix(uri, ".md") && !strings.HasSuffix(uri, ".markdown") {
					continue
				}
				delete(sState.Index, uri)
				delete(sState.ProcessedRenames, state.CleanURIPath(uri))
			}
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

		WorkspaceExecuteCommand: func(context *glsp.Context, params *protocol.ExecuteCommandParams) (any, error) {
			if params.Command == "xsmd.dumpState" {
				sState.Mu.RLock()
				keys := make([]string, 0, len(sState.Index))
				for k := range sState.Index {
					keys = append(keys, k)
				}
				debugLog := sState.DebugLog
				sState.Mu.RUnlock()

				if debugLog != nil {
					debugLog(fmt.Sprintf("Current Index Keys: %v", keys))
				}
				return "State dumped to xsmd.log", nil
			}
			return nil, nil
		},
	}

	return &handler
}
