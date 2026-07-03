// Package agent implements the logos-agent core: enrollment, the persistent
// management channel, heartbeats, and clean offboarding.
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultStatePath is where the agent keeps its identity on OpenWrt.
// Overridable with --state or LOGOS_AGENT_STATE for development.
const DefaultStatePath = "/etc/logos/agent.json"

// State is the agent's persistent identity. It is created by `enroll` and
// destroyed by `leave` — wiping this file is what makes offboarding work
// without headend connectivity (PRD §4.4).
type State struct {
	ServerURL  string `json:"server_url"`
	NodeID     string `json:"node_id"`
	NodeToken  string `json:"node_token"`
	PrivateKey string `json:"private_key"` // hex ed25519 seed; used for mTLS in M1
}

func StatePath() string {
	if p := os.Getenv("LOGOS_AGENT_STATE"); p != "" {
		return p
	}
	return DefaultStatePath
}

func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if st.ServerURL == "" || st.NodeID == "" || st.NodeToken == "" {
		return nil, fmt.Errorf("%s is incomplete; re-enroll", path)
	}
	return &st, nil
}

func SaveState(path string, st *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	// 0600: the node token is a credential.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WipeState removes the agent identity; missing file is not an error.
func WipeState(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
