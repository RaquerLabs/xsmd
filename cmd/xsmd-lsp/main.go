package main

import (
	"log"

	"github.com/tliron/glsp/server"
	"github.com/RaquerLabs/xsmd/internal/lsp"
	"github.com/RaquerLabs/xsmd/internal/state"
)

func main() {
	sState := state.NewServerState()
	handler := lsp.BuildHandler(sState)

	s := server.NewServer(handler, "xsmd-lsp", false)
	log.Fatal(s.RunStdio())
}
