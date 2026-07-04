package alerts

import (
	"context"
	"encoding/json"
	"fmt"
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

	evs := decide(nodes, online, after, 0, now)
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %+v", evs)
	}
	if evs[0].NodeID != "gone" || evs[0].Kind != kindOffline || !evs[0].Raise || !strings.Contains(evs[0].Subject, "offline") {
		t.Errorf("offline event: %+v", evs[0])
	}
	if evs[1].NodeID != "back" || evs[1].Kind != kindOffline || evs[1].Raise || !strings.Contains(evs[1].Subject, "back online") {
		t.Errorf("recovery event: %+v", evs[1])
	}
}

// fsMetrics builds a heartbeat payload with a given rootfs usage percentage.
func fsMetrics(usedPct float64) []byte {
	const total = 10000.0
	free := total * (100 - usedPct) / 100
	return []byte(fmt.Sprintf(`{"rootfs_total_kb":%g,"rootfs_free_kb":%g}`, total, free))
}

func TestDecideDiskFull(t *testing.T) {
	now := time.Now()
	online := map[string]bool{"full": true, "recovered": true, "hovering": true, "offline-full": false}
	nodes := []*store.Node{
		// crosses the threshold, not yet alerted → raise
		{ID: "full", Name: "full", Status: store.NodeStatusEnrolled, LastMetrics: fsMetrics(95)},
		// alerted, dropped below threshold-margin → clear
		{ID: "recovered", Name: "recovered", Status: store.NodeStatusEnrolled,
			LastMetrics: fsMetrics(80), AlertedDiskFullAt: tp(now)},
		// alerted, still in the hysteresis band (88 > 90-5) → no event
		{ID: "hovering", Name: "hovering", Status: store.NodeStatusEnrolled,
			LastMetrics: fsMetrics(88), AlertedDiskFullAt: tp(now)},
		// full but offline → stale data, no raise
		{ID: "offline-full", Name: "offline-full", Status: store.NodeStatusEnrolled,
			LastMetrics: fsMetrics(99)},
		// below threshold, never alerted → nothing
		{ID: "healthy", Name: "healthy", Status: store.NodeStatusEnrolled, LastMetrics: fsMetrics(20)},
	}
	online["healthy"] = true

	evs := decide(nodes, online, time.Minute, 90, now)
	if len(evs) != 2 {
		t.Fatalf("want 2 disk events, got %+v", evs)
	}
	byNode := map[string]event{}
	for _, e := range evs {
		byNode[e.NodeID] = e
	}
	if e, ok := byNode["full"]; !ok || e.Kind != kindDisk || !e.Raise {
		t.Errorf("expected raise for 'full': %+v", e)
	}
	if e, ok := byNode["recovered"]; !ok || e.Kind != kindDisk || e.Raise {
		t.Errorf("expected clear for 'recovered': %+v", e)
	}
	if _, ok := byNode["hovering"]; ok {
		t.Error("'hovering' should not flap inside the hysteresis band")
	}
	if _, ok := byNode["offline-full"]; ok {
		t.Error("offline node should not raise a disk alert from stale metrics")
	}
}

func TestDecideDiskDisabled(t *testing.T) {
	now := time.Now()
	nodes := []*store.Node{{ID: "full", Name: "full", Status: store.NodeStatusEnrolled, LastMetrics: fsMetrics(99)}}
	if evs := decide(nodes, map[string]bool{"full": true}, time.Minute, 0, now); len(evs) != 0 {
		t.Errorf("diskPct=0 should disable low-flash alerts: %+v", evs)
	}
}

func TestDecideOnlineButStaleHeartbeat(t *testing.T) {
	// A live channel wins over a stale last_seen_at: no alert while the hub
	// still holds the connection.
	now := time.Now()
	nodes := []*store.Node{{ID: "a", Name: "a", Status: store.NodeStatusEnrolled,
		LastSeenAt: tp(now.Add(-time.Hour))}}
	if evs := decide(nodes, map[string]bool{"a": true}, time.Minute, 90, now); len(evs) != 0 {
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
