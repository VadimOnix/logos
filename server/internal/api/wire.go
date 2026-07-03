package api

import "encoding/json"

// Agent↔server wire messages. JSON over a WebSocket for the MVP; the
// transport question (protobuf/gRPC/MQTT) is PRD §11(2) and stays open —
// these types are deliberately transport-agnostic.

const (
	msgHello     = "hello"
	msgHeartbeat = "heartbeat"
	msgLeave     = "leave" // server → agent: unenroll yourself
)

type agentMsg struct {
	Type string `json:"type"`

	// hello
	Hostname     string `json:"hostname,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	OSVersion    string `json:"os_version,omitempty"`
	Arch         string `json:"arch,omitempty"`

	// heartbeat
	Metrics json.RawMessage `json:"metrics,omitempty"`

	// leave
	Reason string `json:"reason,omitempty"`
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
