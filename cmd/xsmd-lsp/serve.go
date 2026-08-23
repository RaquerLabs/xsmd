package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/RaquerLabs/xsmd/internal/lsptrace"
	"github.com/RaquerLabs/xsmd/internal/state"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// stdrwc adapts stdin/stdout to io.ReadWriteCloser for the JSON-RPC transport.
type stdrwc struct{}

func (stdrwc) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdrwc) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

func (stdrwc) Close() error {
	err := os.Stdin.Close()
	if err == nil {
		return os.Stdout.Close()
	}
	return err
}

// serve runs the LSP server over stdio, tracing every JSON-RPC message to the
// debug web UI. The UI binds its port only after debug mode turns on, so it is
// unreachable unless debug = true (xsmd.toml or client initializationOptions).
//
// The transport and handler glue mirror what glsp's server package does, with
// one addition: jsonrpc2.OnRecv/OnSend callbacks feed every message, in both
// directions, to the recorder.
func serve(sState *state.ServerState, handler *protocol.Handler) error {
	rec := lsptrace.New(lsptrace.DefaultMaxMessages)
	sState.OnDebugChange = func(debug bool) {
		if debug {
			if err := rec.Serve(lsptrace.DefaultAddr); err != nil {
				log.Printf("debug UI on %s: %v", lsptrace.DefaultAddr, err)
			}
		}
	}

	conn := jsonrpc2.NewConn(
		context.Background(),
		jsonrpc2.NewBufferedStream(stdrwc{}, jsonrpc2.VSCodeObjectCodec{}),
		jsonrpc2.HandlerWithError(func(ctx context.Context, connection *jsonrpc2.Conn, request *jsonrpc2.Request) (any, error) {
			glspContext := glsp.Context{
				Method: request.Method,
				Notify: func(method string, params any) {
					if err := connection.Notify(ctx, method, params); err != nil {
						log.Printf("lsp: notify %s: %v", method, err)
					}
				},
				Call: func(method string, params any, result any) {
					if err := connection.Call(ctx, method, params, result); err != nil {
						log.Printf("lsp: call %s: %v", method, err)
					}
				},
			}
			if request.Params != nil {
				glspContext.Params = *request.Params
			}

			switch request.Method {
			case "exit":
				// Give the attached handler a chance first; ignore its result.
				handler.Handle(&glspContext)
				err := connection.Close()
				return nil, err

			default:
				// jsonrpc2 never calls this function when request.Params is
				// invalid JSON, so CodeParseError needs no handling here.
				r, validMethod, validParams, err := handler.Handle(&glspContext)
				if !validMethod {
					return nil, &jsonrpc2.Error{
						Code:    jsonrpc2.CodeMethodNotFound,
						Message: fmt.Sprintf("method not supported: %s", request.Method),
					}
				} else if !validParams {
					if err != nil {
						return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: err.Error()}
					}
					return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams}
				} else if err != nil {
					return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidRequest, Message: err.Error()}
				}
				return r, nil
			}
		}),
		jsonrpc2.OnRecv(func(req *jsonrpc2.Request, resp *jsonrpc2.Response) {
			if req != nil {
				rec.RecordRequest(lsptrace.FromClient, req)
			} else if resp != nil {
				rec.RecordResponse(lsptrace.FromClient, resp)
			}
		}),
		jsonrpc2.OnSend(func(req *jsonrpc2.Request, resp *jsonrpc2.Response) {
			if req != nil {
				rec.RecordRequest(lsptrace.FromServer, req)
			} else if resp != nil {
				rec.RecordResponse(lsptrace.FromServer, resp)
			}
		}),
	)

	<-conn.DisconnectNotify()
	return nil
}
