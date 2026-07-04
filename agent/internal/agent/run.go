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
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

	// A leftover unconfirmed config change means the previous apply never got
	// confirmed (crash/reboot/lost channel) — restore the snapshot first.
	SetStateDir(filepath.Dir(statePath), log)
	if err := RevertPendingOnStart(); err != nil {
		log.Error("revert of unconfirmed config change failed", "err", err)
	}

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

// wsSession serializes writes to the socket: heartbeats from the main loop
// and rpc_result messages from handler goroutines share one connection.
type wsSession struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *wsSession) send(ctx context.Context, msg wireMsg) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.conn.Write(writeCtx, websocket.MessageText, data)
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

	sess := &wsSession{conn: conn}
	connCtx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()

	hostname, _ := os.Hostname()
	if err := sess.send(connCtx, wireMsg{
		Type:           msgHello,
		Hostname:       hostname,
		AgentVersion:   Version,
		OSVersion:      OSVersion(),
		Arch:           runtime.GOARCH,
		PendingApplyID: PendingApplyID(),
	}); err != nil {
		return err
	}
	if err := sendHeartbeat(connCtx, sess); err != nil {
		return err
	}

	// Reader: server-initiated messages — leave and RPCs (packages, config).
	rpcSem := make(chan struct{}, rpcConcurrency)
	readErr := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(connCtx)
			if err != nil {
				readErr <- err
				return
			}
			var msg wireMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case msgLeave:
				log.Info("leave requested by server", "reason", msg.Reason)
				readErr <- errLeft
				return
			case msgRPC:
				dispatchRPC(connCtx, msg, log, rpcSem, func(out wireMsg) {
					if err := sess.send(connCtx, out); err != nil && connCtx.Err() == nil {
						log.Warn("send rpc result", "err", err)
					}
				})
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
			if err := sendHeartbeat(connCtx, sess); err != nil {
				return err
			}
		}
	}
}

func sendHeartbeat(ctx context.Context, sess *wsSession) error {
	metrics, err := json.Marshal(CollectMetrics())
	if err != nil {
		return err
	}
	return sess.send(ctx, wireMsg{Type: msgHeartbeat, Metrics: metrics})
}
