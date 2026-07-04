package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// rpcHandler executes one server-initiated method. Params are the raw JSON
// from the wire; the returned value is marshaled into rpc_result.result.
type rpcHandler func(ctx context.Context, params json.RawMessage) (any, error)

// rpcHandlers is the agent's method table. Every entry is an explicit,
// narrowly-scoped capability — the agent never executes arbitrary commands
// from the server (PRD §6: minimal agent surface).
var rpcHandlers = map[string]rpcHandler{
	"ping": func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]string{"pong": time.Now().UTC().Format(time.RFC3339)}, nil
	},
	"packages.list":    handlePackagesList,
	"packages.install": handlePackagesInstall,
	"packages.remove":  handlePackagesRemove,
	"packages.update":  handlePackagesUpdate,
	"uci.export":       handleUCIExport,
}

// rpcConcurrency bounds parallel RPC execution so a burst of requests cannot
// exhaust a 128 MB-RAM router (PRD §6 Footprint).
const rpcConcurrency = 2

// rpcTimeout is the agent-side execution ceiling; the server enforces its own.
const rpcTimeout = 290 * time.Second

// dispatchRPC runs the requested method and sends the result back.
// sendResult is the connection's serialized writer.
func dispatchRPC(ctx context.Context, msg wireMsg, log *slog.Logger, sem chan struct{}, sendResult func(wireMsg)) {
	respond := func(result any, err error) {
		out := wireMsg{Type: msgRPCResult, ID: msg.ID}
		if err != nil {
			out.Error = err.Error()
		} else {
			data, merr := json.Marshal(result)
			if merr != nil {
				out.Error = merr.Error()
			} else {
				out.OK = true
				out.Result = data
			}
		}
		sendResult(out)
	}

	handler, ok := rpcHandlers[msg.Method]
	if !ok {
		respond(nil, fmt.Errorf("unknown method %q (agent %s)", msg.Method, Version))
		return
	}

	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	go func() {
		defer func() { <-sem }()
		log.Info("rpc", "method", msg.Method, "id", msg.ID)
		execCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		result, err := handler(execCtx, msg.Params)
		if err != nil {
			log.Warn("rpc failed", "method", msg.Method, "err", err)
		}
		respond(result, err)
	}()
}
