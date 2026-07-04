package store

import (
	"context"
	"encoding/json"
	"time"
)

// MetricSample is one point in a node's metric history (F6).
type MetricSample struct {
	SampledAt     time.Time `json:"t"`
	Load1         *float64  `json:"load1,omitempty"`
	MemUsedPct    *float64  `json:"mem_used_pct,omitempty"`
	RootFSUsedPct *float64  `json:"rootfs_used_pct,omitempty"`
	RxBytes       *int64    `json:"rx_bytes,omitempty"`
	TxBytes       *int64    `json:"tx_bytes,omitempty"`
	DHCPClients   *int      `json:"dhcp_clients,omitempty"`
}

// heartbeatMetrics is the subset of the agent's JSON payload we retain as a
// time series. Deriving the sample here (not on the agent) keeps the wire
// format and the agent untouched.
type heartbeatMetrics struct {
	Load1          float64 `json:"load1"`
	MemTotalKB     float64 `json:"mem_total_kb"`
	MemAvailableKB float64 `json:"mem_available_kb"`
	RootFSTotalKB  float64 `json:"rootfs_total_kb"`
	RootFSFreeKB   float64 `json:"rootfs_free_kb"`
	Interfaces     []struct {
		RxBytes int64 `json:"rx_bytes"`
		TxBytes int64 `json:"tx_bytes"`
	} `json:"interfaces"`
	DHCPClients []json.RawMessage `json:"dhcp_clients"`
}

// InsertMetricSample derives a history row from a raw heartbeat payload and
// stores it. A nil/empty payload is ignored (heartbeats may omit metrics).
func (s *Store) InsertMetricSample(ctx context.Context, nodeID string, rawMetrics []byte) error {
	if len(rawMetrics) == 0 || string(rawMetrics) == "null" {
		return nil
	}
	var hm heartbeatMetrics
	if err := json.Unmarshal(rawMetrics, &hm); err != nil {
		return nil // don't fail a heartbeat over an unparseable sample
	}

	var memUsed, fsUsed *float64
	if hm.MemTotalKB > 0 {
		v := 100 * (hm.MemTotalKB - hm.MemAvailableKB) / hm.MemTotalKB
		memUsed = &v
	}
	if hm.RootFSTotalKB > 0 {
		v := 100 * (hm.RootFSTotalKB - hm.RootFSFreeKB) / hm.RootFSTotalKB
		fsUsed = &v
	}
	var rx, tx int64
	for _, i := range hm.Interfaces {
		rx += i.RxBytes
		tx += i.TxBytes
	}

	_, err := s.pool.Exec(ctx,
		`insert into node_metrics_history
		   (node_id, load1, mem_used_pct, rootfs_used_pct, rx_bytes, tx_bytes, dhcp_clients)
		 values ($1, $2, $3, $4, $5, $6, $7)`,
		nodeID, hm.Load1, memUsed, fsUsed, rx, tx, len(hm.DHCPClients))
	return err
}

// MetricHistory returns a node's samples since `since`, oldest first.
func (s *Store) MetricHistory(ctx context.Context, nodeID string, since time.Time) ([]MetricSample, error) {
	rows, err := s.pool.Query(ctx,
		`select sampled_at, load1, mem_used_pct, rootfs_used_pct, rx_bytes, tx_bytes, dhcp_clients
		   from node_metrics_history
		  where node_id = $1 and sampled_at >= $2
		  order by sampled_at`, nodeID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricSample{}
	for rows.Next() {
		var m MetricSample
		if err := rows.Scan(&m.SampledAt, &m.Load1, &m.MemUsedPct, &m.RootFSUsedPct,
			&m.RxBytes, &m.TxBytes, &m.DHCPClients); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PruneMetricHistory deletes samples older than the retention window.
func (s *Store) PruneMetricHistory(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`delete from node_metrics_history where sampled_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
