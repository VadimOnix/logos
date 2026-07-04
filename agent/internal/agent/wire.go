package agent

import "encoding/json"

// Wire messages mirrored from the server (JSON over WebSocket, MVP transport).

const (
	msgHello     = "hello"
	msgHeartbeat = "heartbeat"
	msgLeave     = "leave"
	msgRPC       = "rpc"        // server → agent: invoke a method
	msgRPCResult = "rpc_result" // agent → server: method result
)

type wireMsg struct {
	Type string `json:"type"`

	Hostname     string `json:"hostname,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	OSVersion    string `json:"os_version,omitempty"`
	Arch         string `json:"arch,omitempty"`
	// PendingApplyID reports an unconfirmed config change on reconnect so
	// the server can confirm it (the reconnect proves connectivity).
	PendingApplyID string `json:"pending_apply_id,omitempty"`

	Metrics json.RawMessage `json:"metrics,omitempty"`

	Reason string `json:"reason,omitempty"`

	// rpc / rpc_result
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	OK     bool            `json:"ok,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

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
