package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/RaquerLabs/xsmd/internal/lsp"
	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/tliron/glsp/server"
)

func main() {
	serverState := state.NewServerState()
	serverState.DebugLog = debug
	handler := lsp.BuildHandler(serverState)

	s := server.NewServer(handler, "xsmd-lsp", false)
	log.Fatal(s.RunStdio())
}

func debug(msg string) {
	logToFile(msg)
}

func logToFile(msg string) {
	f, _ := os.OpenFile("xsmd.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), msg)
}
