package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RaquerLabs/xsmd/internal/lsp"
	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/tliron/glsp/server"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "list" {
		listNotes()
		return
	}

	serverState := state.NewServerState()
	serverState.DebugLog = debug
	handler := lsp.BuildHandler(serverState)

	s := server.NewServer(handler, "xsmd-lsp", false)
	log.Fatal(s.RunStdio())
}

func listNotes() {
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

	for uri := range serverState.Index {
		absPath := strings.TrimPrefix(uri, "file://")
		rel, err := filepath.Rel(root, absPath)
		if err == nil {
			if !serverState.IsIgnored(rel) {
				fmt.Println(filepath.ToSlash(rel))
			}
		}
	}
}

func debug(msg string) {
	logToFile(msg)
}

func logToFile(msg string) {
	f, _ := os.OpenFile("xsmd.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), msg)
}
