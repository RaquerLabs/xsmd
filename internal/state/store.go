package state

import (
	"os"
	"sync"

	"github.com/RaquerLabs/xsmd/internal/parser"
	"github.com/yuin/goldmark/ast"
)

// DocumentInfo stores indexed information about a single document
type DocumentInfo struct {
	URI     string
	Content []byte
	AST     ast.Node
	Links   []parser.ExtractedLink
	Title   string // Caches the file's primary # Title (or fallback)
	HasH1   bool   // Strictly tracks if an H1 exists
}

// ServerState manages your global workspace memory
type ServerState struct {
	Mu               sync.RWMutex
	WorkspaceRoot    string
	Index            map[string]*DocumentInfo
	Debug            bool
	DebugLog         func(string)
	IgnoreDirs       []string
	ProcessedRenames map[string]string // Maps OldURI -> NewURI to avoid redundant workspaceWillRename updates
}

// NewServerState creates a new empty ServerState
func NewServerState() *ServerState {
	return &ServerState{
		Index:            make(map[string]*DocumentInfo),
		ProcessedRenames: make(map[string]string),
	}
}

// Log prints a message using the configured DebugLog if Debug is enabled
func (s *ServerState) Log(msg string) {
	s.Mu.RLock()
	debug := s.Debug
	debugLog := s.DebugLog
	s.Mu.RUnlock()

	if debug && debugLog != nil {
		debugLog(msg)
	}
}

// LogNoLock prints a message using the configured DebugLog if Debug is enabled, without acquiring any locks
func (s *ServerState) LogNoLock(msg string) {
	if s.Debug && s.DebugLog != nil {
		s.DebugLog(msg)
	}
}

// ParseAndIndexContent parses raw byte arrays directly using the parser package
func (s *ServerState) ParseAndIndexContent(uri string, content []byte) error {
	doc, links, title, hasH1 := parser.ParseMarkdown(uri, content)

	s.Index[uri] = &DocumentInfo{
		URI:     uri,
		Content: content,
		AST:     doc,
		Links:   links,
		Title:   title,
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
