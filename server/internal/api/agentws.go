package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/VadimOnix/logos/server/internal/auth"
	"github.com/VadimOnix/logos/server/internal/store"
)

// heartbeatTimeout is how long the server waits for any agent message before
// declaring the channel dead. Agents send heartbeats every 30s.
const heartbeatTimeout = 90 * time.Second

// agentConn adapts a websocket connection to hub.AgentConn.
type agentConn struct {
	ws     *websocket.Conn
	nodeID string

	mu     sync.Mutex // guards writes
	closed bool
}

func (c *agentConn) SendLeave(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg, _ := json.Marshal(agentMsg{Type: msgLeave, Reason: reason})
	c.ws.Write(ctx, websocket.MessageText, msg) // best-effort
}

func (c *agentConn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.ws.Close(websocket.StatusNormalClosure, "")
}

// handleAgentWS is the persistent management channel (PRD F1/F3): the agent
// dials out, authenticates with its node token, and streams hello/heartbeat
// messages. Node liveness in the panel derives from this connection.
func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	tok := bearerToken(r)
	if tok == "" {
		httpError(w, http.StatusUnauthorized, "node token required")
		return
	}
	node, err := s.store.GetNodeByTokenHash(r.Context(), auth.HashToken(tok))
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusUnauthorized, "unknown or revoked node token")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}

	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept already wrote the HTTP error
	}
	conn := &agentConn{ws: ws, nodeID: node.ID}
	s.hub.Register(node.ID, conn)
	s.log.Info("agent connected", "node", node.ID, "name", node.Name)

	defer func() {
		s.hub.Unregister(node.ID, conn)
		conn.Close()
		s.log.Info("agent disconnected", "node", node.ID)
	}()

	// Detached from the request context: the read loop below owns the lifetime.
	ctx := context.WithoutCancel(r.Context())
	for {
		readCtx, cancel := context.WithTimeout(ctx, heartbeatTimeout)
		_, data, err := ws.Read(readCtx)
		cancel()
		if err != nil {
			return
		}
		var msg agentMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			s.log.Warn("agent sent invalid message", "node", node.ID, "err", err)
			continue
		}
		switch msg.Type {
		case msgHello:
			err = s.store.UpdateNodeInfo(ctx, node.ID, store.NodeInfo{
				Hostname:     msg.Hostname,
				AgentVersion: msg.AgentVersion,
				OSVersion:    msg.OSVersion,
				Arch:         msg.Arch,
			})
		case msgHeartbeat:
			err = s.store.TouchNode(ctx, node.ID, msg.Metrics)
		default:
			s.log.Warn("agent sent unknown message type", "node", node.ID, "type", msg.Type)
		}
		if err != nil {
			s.log.Error("persist agent message", "node", node.ID, "err", err)
		}
	}
}
