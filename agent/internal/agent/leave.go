package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Leave takes the node out of management (PRD §4.4 level 1 "disconnect"):
// best-effort notify the control plane, then wipe the local identity.
// Wiping never depends on the server being reachable — an unreachable
// control plane must not hold a router hostage.
func Leave(ctx context.Context, statePath string) error {
	st, err := LoadState(statePath)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("not enrolled; nothing to do")
		return nil
	}
	if err != nil {
		// Corrupt state still gets wiped: local exit must always work.
		fmt.Fprintf(os.Stderr, "warning: %v; wiping local state anyway\n", err)
		return WipeState(statePath)
	}

	httpCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(httpCtx, http.MethodPost, st.ServerURL+"/api/v1/agent/leave", nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+st.NodeToken)
		resp, derr := http.DefaultClient.Do(req)
		if derr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not notify control plane (%v); leaving locally\n", derr)
		} else {
			resp.Body.Close()
		}
	}

	if err := WipeState(statePath); err != nil {
		return err
	}
	fmt.Println("left management; local identity wiped")
	return nil
}
