package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Version is stamped at build time via -ldflags.
var Version = "0.1.0-dev"

const (
	heartbeatInterval = 30 * time.Second
	backoffMin        = time.Second
	backoffMax        = time.Minute
)

// errLeft signals that the server told us to unenroll.
var errLeft = errors.New("server requested leave")

// Run is the agent main loop: keep the outbound management channel up
// forever, with exponential backoff + jitter between attempts (PRD §6
// Resilience). Returns only on context cancellation or a server-ordered leave.
func Run(ctx context.Context, statePath string, log *slog.Logger) error {
	st, err := LoadState(statePath)
	if err != nil {
		return fmt.Errorf("not enrolled: %w (run `logos-agent enroll` first)", err)
	}
	log.Info("logos-agent starting", "version", Version, "server", st.ServerURL, "node", st.NodeID)

	backoff := backoffMin
	for {
		err := connectAndServe(ctx, st, log)
		switch {
		case errors.Is(err, errLeft):
			log.Info("server removed this node from management; wiping local identity")
			if werr := WipeState(statePath); werr != nil {
				return werr
			}
			return nil
		case ctx.Err() != nil:
			return nil
		}

		// Full jitter: sleep a random duration up to the current backoff cap.
		sleep := time.Duration(rand.Int64N(int64(backoff)))
		log.Warn("management channel down, reconnecting", "err", err, "retry_in", sleep.Round(time.Millisecond))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(sleep):
		}
		backoff = min(backoff*2, backoffMax)
	}
}

func connectAndServe(ctx context.Context, st *State, log *slog.Logger) error {
	wsURL := strings.Replace(st.ServerURL, "http", "ws", 1) + "/api/v1/agent/ws"

	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	conn, resp, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + st.NodeToken}},
	})
	cancel()
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			// Token revoked server-side (e.g. panel "Remove from management"
			// while we were offline): honor the removal.
			return errLeft
		}
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	log.Info("management channel established")

	hostname, _ := os.Hostname()
	if err := sendMsg(ctx, conn, wireMsg{
		Type:         msgHello,
		Hostname:     hostname,
		AgentVersion: Version,
		OSVersion:    OSVersion(),
		Arch:         runtime.GOARCH,
	}); err != nil {
		return err
	}
	if err := sendHeartbeat(ctx, conn); err != nil {
		return err
	}

	// Reader: watch for server-initiated messages (leave).
	readErr := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				readErr <- err
				return
			}
			var msg wireMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg.Type == msgLeave {
				log.Info("leave requested by server", "reason", msg.Reason)
				readErr <- errLeft
				return
			}
		}
	}()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case <-ticker.C:
			if err := sendHeartbeat(ctx, conn); err != nil {
				return err
			}
		}
	}
}

func sendHeartbeat(ctx context.Context, conn *websocket.Conn) error {
	metrics, err := json.Marshal(CollectMetrics())
	if err != nil {
		return err
	}
	return sendMsg(ctx, conn, wireMsg{Type: msgHeartbeat, Metrics: metrics})
}

func sendMsg(ctx context.Context, conn *websocket.Conn, msg wireMsg) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}
