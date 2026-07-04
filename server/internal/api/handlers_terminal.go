package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/VadimOnix/logos/server/internal/store"
)

// parseResize recognizes the {"resize":[cols,rows]} control frame the panel
// sends on a terminal resize; anything else is treated as keystrokes.
func parseResize(data []byte) (cols, rows uint16, ok bool) {
	var ctrl struct {
		Resize []uint16 `json:"resize"`
	}
	if err := json.Unmarshal(data, &ctrl); err != nil || len(ctrl.Resize) != 2 {
		return 0, 0, false
	}
	return ctrl.Resize[0], ctrl.Resize[1], true
}

// F10 remote terminal. A browser opens a WebSocket to the panel; the server
// bridges it to the node's management channel (term_open/term_data/
// term_close) and records an audit row for the session. The control plane
// is the only way in, and everything is tied to the channel's lifetime.

const termWriteTimeout = 10 * time.Second

// GET /api/v1/nodes/{id}/terminal/log — audit history for the node.
func (s *Server) handleTerminalLog(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id := r.PathValue("id")
	if _, err := s.store.GetNode(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "node not found")
		return
	} else if err != nil {
		s.internalError(w, err)
		return
	}
	sessions, err := s.store.ListTerminalSessions(r.Context(), id, 50)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

// GET /api/v1/nodes/{id}/terminal/ws — the interactive bridge.
func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	node, err := s.store.GetNode(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if node.Status != store.NodeStatusEnrolled {
		httpError(w, http.StatusConflict, "node has left management")
		return
	}

	// The terminal rides the agent's channel; grab the live connection.
	conn := s.hub.AgentConn(node.ID)
	ac, ok := conn.(*agentConn)
	if !ok || ac == nil {
		httpError(w, http.StatusServiceUnavailable, "node is offline")
		return
	}

	browser, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer browser.Close(websocket.StatusNormalClosure, "")

	termID := strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + node.ID[:8]
	inbox := ac.openTermRoute(termID)
	defer ac.closeTermRoute(termID)

	auditID, err := s.store.CreateTerminalSession(r.Context(), node.ID, u.Email)
	if err != nil {
		s.internalError(w, err)
		return
	}
	closeReason := "closed"
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		s.store.CloseTerminalSession(ctx, auditID, closeReason)
		cancel()
	}()
	s.log.Info("terminal opened", "node", node.ID, "user", u.Email, "term", termID)

	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()

	// Open the shell on the agent.
	if err := ac.write(ctx, agentMsg{Type: msgTermOpen, TermID: termID}); err != nil {
		closeReason = "failed to reach node"
		return
	}
	// Ensure the agent tears the shell down when we leave.
	defer func() {
		wctx, wcancel := context.WithTimeout(context.Background(), termWriteTimeout)
		ac.write(wctx, agentMsg{Type: msgTermClose, TermID: termID})
		wcancel()
	}()

	// Agent → browser.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-inbox:
				if !ok {
					browser.Close(websocket.StatusNormalClosure, "channel closed")
					cancel()
					return
				}
				switch msg.Type {
				case msgTermData:
					wctx, wcancel := context.WithTimeout(ctx, termWriteTimeout)
					err := browser.Write(wctx, websocket.MessageBinary, msg.Data)
					wcancel()
					if err != nil {
						cancel()
						return
					}
				case msgTermClose:
					browser.Close(websocket.StatusNormalClosure, "shell closed")
					cancel()
					return
				}
			}
		}
	}()

	// Browser → agent. A control frame {"resize":[cols,rows]} sets the
	// window size; everything else is raw keystrokes.
	for {
		typ, data, err := browser.Read(ctx)
		if err != nil {
			closeReason = "operator disconnected"
			return
		}
		out := agentMsg{Type: msgTermData, TermID: termID}
		if typ == websocket.MessageText {
			if cols, rows, ok := parseResize(data); ok {
				out.Cols, out.Rows = cols, rows
			} else {
				out.Data = data
			}
		} else {
			out.Data = data
		}
		if err := ac.write(ctx, out); err != nil {
			closeReason = "node channel lost"
			return
		}
	}
}
