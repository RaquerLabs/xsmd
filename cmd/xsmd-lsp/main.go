package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RaquerLabs/xsmd/internal/lsp"
	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/tliron/glsp/server"
)

// version is the build version. Override at build time with
// -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "list":
			listNotes()
			return
		case "--version", "-v":
			fmt.Printf("xsmd %s\n", version)
			return
		}
	}

	serverState := state.NewServerState()
	serverState.DebugLog = logToFile(serverState)
	handler := lsp.BuildHandler(serverState)

	s := server.NewServer(handler, "xsmd-lsp", false)
	log.Fatal(s.RunStdio())
}

func listNotes() {
	jsonOut := len(os.Args) > 2 && os.Args[2] == "--json"

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get current working directory: %v\n", err)
		os.Exit(1)
	}

	root, err := state.FindProjectRoot(cwd)
	if err != nil {
		// Fallback to current working directory if xsmd.toml not found in ancestry
		root = cwd
	}

	serverState := state.NewServerState()
	serverState.WorkspaceRoot = root
	serverState.LoadConfig()

	err = serverState.CrawlWorkspace()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to crawl workspace: %v\n", err)
		os.Exit(1)
	}

	serverState.Mu.RLock()
	defer serverState.Mu.RUnlock()

	type note struct {
		Path  string `json:"path"`
		Title string `json:"title"`
		HasH1 bool   `json:"has_h1"`
	}

	notes := make([]note, 0)
	for uri, doc := range serverState.Index {
		absPath := strings.TrimPrefix(uri, "file://")
		rel, err := filepath.Rel(root, absPath)
		if err == nil {
			rel = filepath.ToSlash(rel)
			if !serverState.IsIgnored(rel) {
				notes = append(notes, note{Path: rel, Title: doc.Title, HasH1: doc.HasH1})
			}
		}
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path })

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(notes); err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode notes: %v\n", err)
			os.Exit(1)
		}
		return
	}

	for _, n := range notes {
		fmt.Println(n.Path)
	}
}

func logToFile(s *state.ServerState) func(string) {
	return func(msg string) {
		// Log into the workspace root so xsmd.log lands where users look for
		// it, regardless of the directory the server was launched from.
		// Read without the mutex: LogNoLock callers may already hold it.
		dir := s.WorkspaceRoot
		if dir == "" {
			dir = "."
		}
		f, _ := os.OpenFile(filepath.Join(dir, "xsmd.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		defer f.Close()
		fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), msg)
	}
}
