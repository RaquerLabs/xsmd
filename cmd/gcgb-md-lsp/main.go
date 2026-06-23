package main

import (
	"log"

	"github.com/tliron/glsp/server"
	"github.com/yourusername/gcgb-md/internal/lsp"
	"github.com/yourusername/gcgb-md/internal/state"
)

func main() {
	sState := state.NewServerState()
	handler := lsp.BuildHandler(sState)

	s := server.NewServer(handler, "gcgb-md-lsp", false)
	log.Fatal(s.RunStdio())
}
