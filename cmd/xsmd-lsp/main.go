package main

import (
	"log"

	"github.com/RaquerLabs/xsmd/internal/lsp"
	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/tliron/glsp/server"
)

func main() {
	sState := state.NewServerState()
	handler := lsp.BuildHandler(sState)

	s := server.NewServer(handler, "xsmd-lsp", false)
	log.Fatal(s.RunStdio())
}
