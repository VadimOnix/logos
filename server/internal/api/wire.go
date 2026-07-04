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

	// F10 remote terminal, multiplexed over the same channel.
	msgTermOpen  = "term_open"  // server → agent
	msgTermData  = "term_data"  // both directions
	msgTermClose = "term_close" // both directions
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

	// term_open / term_data / term_close (F10)
	TermID string `json:"term_id,omitempty"`
	Data   []byte `json:"data,omitempty"` // base64 on the wire
	Cols   uint16 `json:"cols,omitempty"`
	Rows   uint16 `json:"rows,omitempty"`
}

// enrollRequest is the body of POST /api/v1/enroll.
type enrollRequest struct {
	Code         string `json:"code"`
	PublicKey    string `json:"public_key"`
	CSR          string `json:"csr,omitempty"` // PEM; enables the mTLS channel
	Hostname     string `json:"hostname"`
	AgentVersion string `json:"agent_version"`
	OSVersion    string `json:"os_version"`
	Arch         string `json:"arch"`
}

type enrollResponse struct {
	NodeID    string `json:"node_id"`
	NodeName  string `json:"node_name"`
	NodeToken string `json:"node_token"`

	// mTLS channel material, present when the request carried a CSR.
	ClientCert    string `json:"client_cert,omitempty"`
	CACert        string `json:"ca_cert,omitempty"`
	AgentEndpoint string `json:"agent_endpoint,omitempty"`
}
