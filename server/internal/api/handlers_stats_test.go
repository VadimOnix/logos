package api

import (
	"testing"
	"time"

	"github.com/VadimOnix/logos/server/internal/store"
)

func TestFleetStats(t *testing.T) {
	now := time.Now()
	nodes := []*store.Node{
		{ID: "a", Status: store.NodeStatusEnrolled},                                              // online, healthy
		{ID: "b", Status: store.NodeStatusEnrolled, AlertedDiskFullAt: &now},                     // online, disk alert outstanding
		{ID: "c", Status: store.NodeStatusEnrolled, AlertedOfflineAt: &now},                      // offline, alerted
		{ID: "d", Status: store.NodeStatusLeft},                                                  // left
		{ID: "e", Status: store.NodeStatusLeft, AlertedOfflineAt: &now, AlertedDiskFullAt: &now}, // left: stale marks must NOT count as outstanding
	}
	online := map[string]bool{"a": true, "b": true}

	v := fleetStats(nodes, func(id string) bool { return online[id] }, 3)

	if v.Nodes.Total != 5 || v.Nodes.Online != 2 || v.Nodes.Offline != 1 || v.Nodes.Left != 2 {
		t.Errorf("nodes: %+v", v.Nodes)
	}
	if v.Nodes.Online+v.Nodes.Offline+v.Nodes.Left != v.Nodes.Total {
		t.Errorf("buckets don't sum to total: %+v", v.Nodes)
	}
	if v.Overlays != 3 {
		t.Errorf("overlays = %d, want 3", v.Overlays)
	}
	if v.Alerts.Offline != 1 || v.Alerts.DiskFull != 1 {
		t.Errorf("alerts: %+v", v.Alerts)
	}
}

func TestFleetStatsEmpty(t *testing.T) {
	v := fleetStats(nil, func(string) bool { return false }, 0)
	if v.Nodes.Total != 0 || v.Overlays != 0 || v.Alerts.Offline != 0 || v.Alerts.DiskFull != 0 {
		t.Errorf("empty fleet: %+v", v)
	}
}
