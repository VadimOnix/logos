// Package hub tracks live agent connections. A node is "online" iff its
// agent currently holds the persistent management channel.
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// ErrOffline is returned when an RPC targets a node with no live channel.
var ErrOffline = errors.New("node is offline")

// AgentConn is what the hub needs from a live agent connection.
type AgentConn interface {
	// SendLeave asks the agent to unenroll itself; best-effort.
	SendLeave(reason string)
	// Call invokes a method on the agent and waits for its result.
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
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

// Call invokes a method on a node's agent over its live channel.
func (h *Hub) Call(ctx context.Context, nodeID, method string, params any) (json.RawMessage, error) {
	h.mu.RLock()
	c := h.conns[nodeID]
	h.mu.RUnlock()
	if c == nil {
		return nil, ErrOffline
	}
	return c.Call(ctx, method, params)
}

func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}
