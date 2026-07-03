// Package hub tracks live agent connections. A node is "online" iff its
// agent currently holds the persistent management channel.
package hub

import "sync"

// AgentConn is what the hub needs from a live agent connection.
type AgentConn interface {
	// SendLeave asks the agent to unenroll itself; best-effort.
	SendLeave(reason string)
	// Close tears the connection down.
	Close()
}

type Hub struct {
	mu    sync.RWMutex
	conns map[string]AgentConn // node ID → connection
}

func New() *Hub {
	return &Hub{conns: make(map[string]AgentConn)}
}

// Register attaches a connection for a node, replacing (and closing) any
// previous one — a reconnecting agent must win over a stale connection.
func (h *Hub) Register(nodeID string, c AgentConn) {
	h.mu.Lock()
	prev := h.conns[nodeID]
	h.conns[nodeID] = c
	h.mu.Unlock()
	if prev != nil && prev != c {
		prev.Close()
	}
}

// Unregister detaches the connection if it is still the current one.
func (h *Hub) Unregister(nodeID string, c AgentConn) {
	h.mu.Lock()
	if h.conns[nodeID] == c {
		delete(h.conns, nodeID)
	}
	h.mu.Unlock()
}

func (h *Hub) IsOnline(nodeID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.conns[nodeID]
	return ok
}

// Kick asks a connected agent to leave and drops the channel. No-op when offline.
func (h *Hub) Kick(nodeID, reason string) {
	h.mu.Lock()
	c := h.conns[nodeID]
	delete(h.conns, nodeID)
	h.mu.Unlock()
	if c != nil {
		c.SendLeave(reason)
		c.Close()
	}
}

func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}
