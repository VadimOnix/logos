package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/VadimOnix/logos/server/internal/store"
)

func tp(t time.Time) *time.Time { return &t }

func TestDecide(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	after := 3 * time.Minute
	nodes := []*store.Node{
		{ID: "gone", Name: "gone", Status: store.NodeStatusEnrolled, LastSeenAt: tp(now.Add(-10 * time.Minute))},
		{ID: "fresh", Name: "fresh", Status: store.NodeStatusEnrolled, LastSeenAt: tp(now.Add(-time.Minute))},
		{ID: "back", Name: "back", Status: store.NodeStatusEnrolled, LastSeenAt: tp(now),
			AlertedOfflineAt: tp(now.Add(-time.Hour))},
		{ID: "still-gone", Name: "still-gone", Status: store.NodeStatusEnrolled,
			LastSeenAt: tp(now.Add(-time.Hour)), AlertedOfflineAt: tp(now.Add(-30 * time.Minute))},
		{ID: "never-seen", Name: "never-seen", Status: store.NodeStatusEnrolled},
		{ID: "left", Name: "left", Status: store.NodeStatusLeft, LastSeenAt: tp(now.Add(-time.Hour))},
	}
	online := map[string]bool{"fresh": true, "back": true}

	evs := decide(nodes, online, after, now)
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %+v", evs)
	}
	if evs[0].NodeID != "gone" || !evs[0].Offline || !strings.Contains(evs[0].Subject, "offline") {
		t.Errorf("offline event: %+v", evs[0])
	}
	if evs[1].NodeID != "back" || evs[1].Offline || !strings.Contains(evs[1].Subject, "back online") {
		t.Errorf("recovery event: %+v", evs[1])
	}
}

func TestDecideOnlineButStaleHeartbeat(t *testing.T) {
	// A live channel wins over a stale last_seen_at: no alert while the hub
	// still holds the connection.
	now := time.Now()
	nodes := []*store.Node{{ID: "a", Name: "a", Status: store.NodeStatusEnrolled,
		LastSeenAt: tp(now.Add(-time.Hour))}}
	if evs := decide(nodes, map[string]bool{"a": true}, time.Minute, now); len(evs) != 0 {
		t.Errorf("alerted despite live channel: %+v", evs)
	}
}

func TestWebhookNotifier(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type %q", ct)
		}
	}))
	defer srv.Close()

	n := &WebhookNotifier{URL: srv.URL}
	if err := n.Notify(context.Background(), "subj", "text"); err != nil {
		t.Fatal(err)
	}
	if got["subject"] != "subj" || got["text"] != "text" || got["source"] != "logos" {
		t.Errorf("payload: %v", got)
	}
}

func TestWebhookNotifierError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	if err := (&WebhookNotifier{URL: srv.URL}).Notify(context.Background(), "s", "t"); err == nil {
		t.Error("4xx response reported as success")
	}
}
