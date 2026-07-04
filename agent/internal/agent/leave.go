package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Leave takes the node out of management (PRD §4.4): best-effort notify the
// control plane, optionally restore the pre-adoption snapshot (level 2,
// "disconnect + full cleanup"), then wipe the local identity. None of it
// depends on the server being reachable — an unreachable control plane must
// not hold a router hostage.
func Leave(ctx context.Context, statePath string, cleanup, yes bool) error {
	st, err := LoadState(statePath)
	if errors.Is(err, os.ErrNotExist) {
		if cleanup {
			// Not enrolled but a snapshot may still exist (e.g. a previous
			// leave without cleanup): allow restoring it.
			return CleanupToSnapshot(ctx, statePath, yes, os.Stdin, os.Stdout)
		}
		fmt.Println("not enrolled; nothing to do")
		return nil
	}
	if err != nil {
		// Corrupt state still gets wiped: local exit must always work.
		fmt.Fprintf(os.Stderr, "warning: %v; wiping local state anyway\n", err)
		return WipeState(statePath)
	}

	// Cleanup runs before the identity is wiped so a declined confirmation
	// leaves the node fully enrolled and untouched.
	if cleanup {
		if err := CleanupToSnapshot(ctx, statePath, yes, os.Stdin, os.Stdout); err != nil {
			return err
		}
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
