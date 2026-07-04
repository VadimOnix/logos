package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/VadimOnix/logos/server/internal/auth"
	"github.com/VadimOnix/logos/server/internal/store"
)

// heartbeatTimeout is how long the server waits for any agent message before
// declaring the channel dead. Agents send heartbeats every 30s.
const heartbeatTimeout = 90 * time.Second

// agentConn adapts a websocket connection to hub.AgentConn and correlates
// RPC requests with their results.
type agentConn struct {
	ws     *websocket.Conn
	nodeID string

	writeMu sync.Mutex // one writer at a time on the socket
	closed  bool

	pendingMu sync.Mutex
	pending   map[string]chan agentMsg
	nextID    uint64

	termMu sync.Mutex
	terms  map[string]chan agentMsg // term ID → bridge inbox (F10)
}

func newAgentConn(ws *websocket.Conn, nodeID string) *agentConn {
	return &agentConn{ws: ws, nodeID: nodeID,
		pending: make(map[string]chan agentMsg), terms: make(map[string]chan agentMsg)}
}

// openTermRoute registers a terminal inbox; agent term messages with this ID
// are delivered to the returned channel until closeTermRoute.
func (c *agentConn) openTermRoute(id string) chan agentMsg {
	ch := make(chan agentMsg, 256)
	c.termMu.Lock()
	c.terms[id] = ch
	c.termMu.Unlock()
	return ch
}

func (c *agentConn) closeTermRoute(id string) {
	c.termMu.Lock()
	ch := c.terms[id]
	delete(c.terms, id)
	c.termMu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// routeTerm delivers an agent-sent terminal message to its bridge. Data is
// dropped if the bridge cannot keep up — a stuck browser must not block the
// shared channel reader.
func (c *agentConn) routeTerm(msg agentMsg) {
	c.termMu.Lock()
	ch := c.terms[msg.TermID]
	c.termMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

func (c *agentConn) write(ctx context.Context, msg agentMsg) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return errors.New("connection closed")
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.ws.Write(writeCtx, websocket.MessageText, data)
}

func (c *agentConn) SendLeave(reason string) {
	c.write(context.Background(), agentMsg{Type: msgLeave, Reason: reason}) // best-effort
}

func (c *agentConn) Close() {
	c.writeMu.Lock()
	if c.closed {
		c.writeMu.Unlock()
		return
	}
	c.closed = true
	c.writeMu.Unlock()
	c.ws.Close(websocket.StatusNormalClosure, "")

	// Fail every in-flight RPC.
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()

	// End every terminal bridge.
	c.termMu.Lock()
	for id, ch := range c.terms {
		close(ch)
		delete(c.terms, id)
	}
	c.termMu.Unlock()
}

// Call sends an RPC to the agent and waits for the matching rpc_result.
func (c *agentConn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var rawParams json.RawMessage
	if params != nil {
		p, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		rawParams = p
	}

	c.pendingMu.Lock()
	c.nextID++
	id := strconv.FormatUint(c.nextID, 10)
	ch := make(chan agentMsg, 1)
	c.pending[id] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	if err := c.write(ctx, agentMsg{Type: msgRPC, ID: id, Method: method, Params: rawParams}); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res, ok := <-ch:
		if !ok {
			return nil, errors.New("connection lost while waiting for the agent")
		}
		if !res.OK {
			return nil, fmt.Errorf("agent error: %s", res.Error)
		}
		return res.Result, nil
	}
}

// resolve routes an rpc_result to its waiting Call, if any.
func (c *agentConn) resolve(msg agentMsg) {
	c.pendingMu.Lock()
	ch := c.pending[msg.ID]
	delete(c.pending, msg.ID)
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- msg
	}
}

// handleAgentWS is the legacy token-authenticated management channel on the
// main listener. New enrollments use the mTLS listener (handleAgentWSMTLS);
// this path remains for nodes enrolled before certificates existed.
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
	s.serveAgentWS(w, r, node)
}

// serveAgentWS runs the persistent management channel (PRD F1/F3): the agent
// streams hello/heartbeat messages and the server invokes RPCs (packages,
// config) over the same socket. Node liveness derives from this connection.
func (s *Server) serveAgentWS(w http.ResponseWriter, r *http.Request, node *store.Node) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept already wrote the HTTP error
	}
	conn := newAgentConn(ws, node.ID)
	s.hub.Register(node.ID, conn)
	s.log.Info("agent connected", "node", node.ID, "name", node.Name)

	// The channel's source address is the best WireGuard endpoint candidate
	// other overlay peers can dial (F7).
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && host != "" {
		if err := s.store.SetNodeAddr(r.Context(), node.ID, host); err != nil {
			s.log.Warn("record node address", "node", node.ID, "err", err)
		}
	}

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
			if msg.PendingApplyID != "" {
				// The reconnect proves connectivity: confirm the change the
				// agent is still holding a revert window for.
				go s.reconcilePendingApply(node.ID, msg.PendingApplyID)
			}
			// Bring the node's WireGuard interfaces to the desired state —
			// overlays may have changed while it was offline (F7).
			go s.reconcileNodeOverlays(node.ID)
		case msgHeartbeat:
			err = s.store.TouchNode(ctx, node.ID, msg.Metrics)
		case msgRPCResult:
			conn.resolve(msg)
		case msgTermData, msgTermClose:
			conn.routeTerm(msg)
		default:
			s.log.Warn("agent sent unknown message type", "node", node.ID, "type", msg.Type)
		}
		if err != nil {
			s.log.Error("persist agent message", "node", node.ID, "err", err)
		}
	}
}
