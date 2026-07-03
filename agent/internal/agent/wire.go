package agent

import "encoding/json"

// Wire messages mirrored from the server (JSON over WebSocket, MVP transport).

const (
	msgHello     = "hello"
	msgHeartbeat = "heartbeat"
	msgLeave     = "leave"
)

type wireMsg struct {
	Type string `json:"type"`

	Hostname     string `json:"hostname,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	OSVersion    string `json:"os_version,omitempty"`
	Arch         string `json:"arch,omitempty"`

	Metrics json.RawMessage `json:"metrics,omitempty"`

	Reason string `json:"reason,omitempty"`
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
