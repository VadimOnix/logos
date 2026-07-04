package adopt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CodeMinter mints fresh single-use claim codes from the control plane, so a
// fleet run can give each router its own (F12 v1). Requires an API token with
// panel access — the same token an operator creates under "API tokens".
type CodeMinter struct {
	Server string
	Token  string
	Client *http.Client
}

func NewCodeMinter(server, token string) *CodeMinter {
	return &CodeMinter{
		Server: strings.TrimRight(server, "/"),
		Token:  token,
		Client: &http.Client{Timeout: 20 * time.Second},
	}
}

// Mint requests one claim code. The note ties the code to its router in the
// panel's claim-code list for auditability.
func (m *CodeMinter) Mint(ctx context.Context, note string) (string, error) {
	body, _ := json.Marshal(map[string]string{"note": note})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		m.Server+"/api/v1/claim-codes", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.Token)
	resp, err := m.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mint claim code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mint claim code: control plane returned %s", resp.Status)
	}
	var out struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Code == "" {
		return "", fmt.Errorf("control plane returned an empty claim code")
	}
	return out.Code, nil
}
