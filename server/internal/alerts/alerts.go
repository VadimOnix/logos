// Package alerts implements node-offline alerting (F11): a watcher compares
// every enrolled node's liveness against a threshold and notifies the
// configured sinks (webhook and/or SMTP) on offline and recovery
// transitions. State lives in the nodes table, so restarts neither repeat
// nor lose alerts.
package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/VadimOnix/logos/server/internal/store"
)

// Notifier delivers one alert to one sink. Implementations must be safe for
// concurrent use.
type Notifier interface {
	Notify(ctx context.Context, subject, text string) error
	Name() string
}

// WebhookNotifier POSTs a small JSON document to a URL — the lowest common
// denominator for chat hooks and incident tooling.
type WebhookNotifier struct {
	URL    string
	Client *http.Client
}

func (w *WebhookNotifier) Name() string { return "webhook" }

func (w *WebhookNotifier) Notify(ctx context.Context, subject, text string) error {
	body, err := json.Marshal(map[string]string{
		"source":  "logos",
		"subject": subject,
		"text":    text,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}

// SMTPNotifier sends plain-text mail via SMTP (STARTTLS when the server
// offers it, AUTH PLAIN when credentials are configured).
type SMTPNotifier struct {
	Addr     string // host:port
	From     string
	To       []string
	Username string
	Password string
}

func (m *SMTPNotifier) Name() string { return "smtp" }

func (m *SMTPNotifier) Notify(_ context.Context, subject, text string) error {
	var auth smtp.Auth
	if m.Username != "" {
		host, _, _ := strings.Cut(m.Addr, ":")
		auth = smtp.PlainAuth("", m.Username, m.Password, host)
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n",
		m.From, strings.Join(m.To, ", "), subject, text)
	return smtp.SendMail(m.Addr, auth, m.From, m.To, []byte(msg))
}

// event is one alert-worthy transition found in a scan.
type event struct {
	NodeID  string
	Offline bool // false = recovery
	Subject string
	Text    string
}

// decide computes the transitions for one scan. Pure — all state comes in
// via the node rows and the online set.
func decide(nodes []*store.Node, online map[string]bool, offlineAfter time.Duration, now time.Time) []event {
	var out []event
	for _, n := range nodes {
		if n.Status != store.NodeStatusEnrolled {
			continue
		}
		isOnline := online[n.ID]
		switch {
		case !isOnline && n.AlertedOfflineAt == nil:
			// Never-seen nodes (enrolled but the agent has not connected
			// yet) are not "offline" — there is nothing to lose contact with.
			if n.LastSeenAt == nil || now.Sub(*n.LastSeenAt) < offlineAfter {
				continue
			}
			out = append(out, event{
				NodeID:  n.ID,
				Offline: true,
				Subject: fmt.Sprintf("[logos] node %s is offline", n.Name),
				Text: fmt.Sprintf("Node %q (%s, %s) has not been seen since %s (threshold %s).",
					n.Name, n.Hostname, n.ID, n.LastSeenAt.UTC().Format(time.RFC3339), offlineAfter),
			})
		case isOnline && n.AlertedOfflineAt != nil:
			out = append(out, event{
				NodeID:  n.ID,
				Offline: false,
				Subject: fmt.Sprintf("[logos] node %s is back online", n.Name),
				Text: fmt.Sprintf("Node %q (%s, %s) reconnected after being offline since %s.",
					n.Name, n.Hostname, n.ID, n.AlertedOfflineAt.UTC().Format(time.RFC3339)),
			})
		}
	}
	return out
}

// Watcher periodically scans the registry and fires notifications.
type Watcher struct {
	Store        *store.Store
	IsOnline     func(nodeID string) bool // hub liveness
	Notifiers    []Notifier
	OfflineAfter time.Duration
	Interval     time.Duration
	Log          *slog.Logger

	now func() time.Time
}

// Run blocks until ctx is done. Call in a goroutine.
func (w *Watcher) Run(ctx context.Context) {
	if len(w.Notifiers) == 0 {
		return
	}
	if w.now == nil {
		w.now = time.Now
	}
	w.Log.Info("offline alerting enabled",
		"threshold", w.OfflineAfter, "sinks", w.sinkNames())
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.scan(ctx)
		}
	}
}

func (w *Watcher) sinkNames() string {
	names := make([]string, len(w.Notifiers))
	for i, n := range w.Notifiers {
		names[i] = n.Name()
	}
	return strings.Join(names, ",")
}

func (w *Watcher) scan(ctx context.Context) {
	nodes, err := w.Store.ListNodes(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.Log.Error("alert scan: list nodes", "err", err)
		}
		return
	}
	online := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		online[n.ID] = w.IsOnline(n.ID)
	}
	for _, ev := range decide(nodes, online, w.OfflineAfter, w.now()) {
		// Flip the mark first: a duplicate alert is worse than a missed one
		// (the next transition re-alerts anyway).
		if err := w.Store.SetNodeOfflineAlerted(ctx, ev.NodeID, ev.Offline); err != nil {
			w.Log.Error("alert state", "node", ev.NodeID, "err", err)
			continue
		}
		w.Log.Warn("node alert", "subject", ev.Subject)
		for _, n := range w.Notifiers {
			nctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if err := n.Notify(nctx, ev.Subject, ev.Text); err != nil {
				w.Log.Error("alert delivery failed", "sink", n.Name(), "err", err)
			}
			cancel()
		}
	}
}
