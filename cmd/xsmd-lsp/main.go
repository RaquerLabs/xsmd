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
	sState := state.NewServerState()
	handler := lsp.BuildHandler(sState)

	s := server.NewServer(handler, "xsmd-lsp", false)
	log.Fatal(s.RunStdio())
}

func logToFile(msg string) {
	f, _ := os.OpenFile("xsmd.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	f.WriteString(fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), msg))
}
