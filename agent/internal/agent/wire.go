package agent

import "encoding/json"

// Wire messages mirrored from the server (JSON over WebSocket, MVP transport).

const (
	msgHello     = "hello"
	msgHeartbeat = "heartbeat"
	msgLeave     = "leave"
	msgRPC       = "rpc"        // server → agent: invoke a method
	msgRPCResult = "rpc_result" // agent → server: method result

	// F10 remote terminal: a server-initiated interactive shell session
	// multiplexed over the management channel.
	msgTermOpen  = "term_open"  // server → agent
	msgTermData  = "term_data"  // both directions
	msgTermClose = "term_close" // both directions
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

	// term_open / term_data / term_close (F10)
	TermID string `json:"term_id,omitempty"`
	Data   []byte `json:"data,omitempty"` // base64 on the wire
	Cols   uint16 `json:"cols,omitempty"`
	Rows   uint16 `json:"rows,omitempty"`
}

type enrollRequest struct {
	Code         string `json:"code"`
	PublicKey    string `json:"public_key"`
	CSR          string `json:"csr,omitempty"`
	Hostname     string `json:"hostname"`
	AgentVersion string `json:"agent_version"`
	OSVersion    string `json:"os_version"`
	Arch         string `json:"arch"`
}

type enrollResponse struct {
	NodeID    string `json:"node_id"`
	NodeName  string `json:"node_name"`
	NodeToken string `json:"node_token"`

	ClientCert    string `json:"client_cert,omitempty"`
	CACert        string `json:"ca_cert,omitempty"`
	AgentEndpoint string `json:"agent_endpoint,omitempty"`
}
