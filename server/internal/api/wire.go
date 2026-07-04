package api

import "encoding/json"

// Agent↔server wire messages. JSON over a WebSocket for the MVP; the
// transport question (protobuf/gRPC/MQTT) is PRD §11(2) and stays open —
// these types are deliberately transport-agnostic.

const (
	msgHello     = "hello"
	msgHeartbeat = "heartbeat"
	msgLeave     = "leave"      // server → agent: unenroll yourself
	msgRPC       = "rpc"        // server → agent: invoke a method
	msgRPCResult = "rpc_result" // agent → server: method result
)

type agentMsg struct {
	Type string `json:"type"`

	// hello
	Hostname     string `json:"hostname,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	OSVersion    string `json:"os_version,omitempty"`
	Arch         string `json:"arch,omitempty"`
	// PendingApplyID: agent reports an unconfirmed config change on
	// reconnect; the reconnect itself proves connectivity, so the server
	// confirms the change (see handlers_config.go).
	PendingApplyID string `json:"pending_apply_id,omitempty"`

	// heartbeat
	Metrics json.RawMessage `json:"metrics,omitempty"`

	// leave
	Reason string `json:"reason,omitempty"`

	// rpc / rpc_result
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	OK     bool            `json:"ok,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// enrollRequest is the body of POST /api/v1/enroll.
type enrollRequest struct {
	Code         string `json:"code"`
	PublicKey    string `json:"public_key"`
	Hostname     string `json:"hostname"`
	AgentVersion string `json:"agent_version"`
	OSVersion    string `json:"os_version"`
	Arch         string `json:"arch"`
}

type enrollResponse struct {
	NodeID    string `json:"node_id"`
	NodeName  string `json:"node_name"`
	NodeToken string `json:"node_token"`
}
