package store

import (
	"context"
	"time"
)

const (
	NodeStatusEnrolled = "enrolled"
	NodeStatusLeft     = "left"
)

type Node struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	PublicKey    string     `json:"public_key"`
	Hostname     string     `json:"hostname"`
	AgentVersion string     `json:"agent_version"`
	OSVersion    string     `json:"os_version"`
	Arch         string     `json:"arch"`
	Status       string     `json:"status"`
	EnrolledAt   time.Time  `json:"enrolled_at"`
	LeftAt       *time.Time `json:"left_at,omitempty"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	LastMetrics  []byte     `json:"-"`
	// AlertedOfflineAt is set while an offline alert for the node is
	// outstanding (F11); cleared when the node comes back.
	AlertedOfflineAt *time.Time `json:"-"`
	// AlertedDiskFullAt is set while a low-flash alert for the node is
	// outstanding (F11); cleared when usage falls back below the threshold.
	AlertedDiskFullAt *time.Time `json:"-"`
	// AlertedMemFullAt is the memory-pressure counterpart of AlertedDiskFullAt.
	AlertedMemFullAt *time.Time `json:"-"`
	// ConfigBaselineHash is the accepted `uci export` fingerprint for drift
	// detection (v1.0); nil until the node first reports one.
	ConfigBaselineHash *string `json:"-"`
}

type NodeInfo struct {
	Hostname     string
	AgentVersion string
	OSVersion    string
	Arch         string
	PublicKey    string
}

const nodeCols = `id, name, public_key, hostname, agent_version, os_version, arch,
	status, enrolled_at, left_at, last_seen_at, last_metrics, alerted_offline_at,
	alerted_diskfull_at, alerted_memfull_at, config_baseline_hash`

func (s *Store) scanNode(row interface{ Scan(...any) error }) (*Node, error) {
	n := &Node{}
	err := row.Scan(&n.ID, &n.Name, &n.PublicKey, &n.Hostname, &n.AgentVersion,
		&n.OSVersion, &n.Arch, &n.Status, &n.EnrolledAt, &n.LeftAt, &n.LastSeenAt, &n.LastMetrics,
		&n.AlertedOfflineAt, &n.AlertedDiskFullAt, &n.AlertedMemFullAt, &n.ConfigBaselineHash)
	if noRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Store) CreateNode(ctx context.Context, id, name string, tokenHash []byte, info NodeInfo) (*Node, error) {
	row := s.pool.QueryRow(ctx,
		`insert into nodes (id, name, token_hash, public_key, hostname, agent_version, os_version, arch)
		 values ($1, $2, $3, $4, $5, $6, $7, $8) returning `+nodeCols,
		id, name, tokenHash, info.PublicKey, info.Hostname, info.AgentVersion, info.OSVersion, info.Arch)
	return s.scanNode(row)
}

func (s *Store) GetNode(ctx context.Context, id string) (*Node, error) {
	return s.scanNode(s.pool.QueryRow(ctx, `select `+nodeCols+` from nodes where id = $1`, id))
}

// GetNodeByTokenHash authenticates an agent: only nodes that are still
// enrolled can use their token.
func (s *Store) GetNodeByTokenHash(ctx context.Context, tokenHash []byte) (*Node, error) {
	return s.scanNode(s.pool.QueryRow(ctx,
		`select `+nodeCols+` from nodes where token_hash = $1 and status = 'enrolled'`, tokenHash))
}

func (s *Store) ListNodes(ctx context.Context) ([]*Node, error) {
	rows, err := s.pool.Query(ctx, `select `+nodeCols+` from nodes order by enrolled_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Node{}
	for rows.Next() {
		n, err := s.scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpdateNodeInfo refreshes identity fields the agent reports on connect.
func (s *Store) UpdateNodeInfo(ctx context.Context, id string, info NodeInfo) error {
	_, err := s.pool.Exec(ctx,
		`update nodes set hostname = $2, agent_version = $3, os_version = $4, arch = $5, last_seen_at = now()
		  where id = $1`,
		id, info.Hostname, info.AgentVersion, info.OSVersion, info.Arch)
	return err
}

// TouchNode records a heartbeat with the latest metrics snapshot (JSON).
func (s *Store) TouchNode(ctx context.Context, id string, metrics []byte) error {
	_, err := s.pool.Exec(ctx,
		`update nodes set last_seen_at = now(), last_metrics = coalesce(nullif($2::jsonb, 'null'::jsonb), last_metrics)
		  where id = $1`, id, metrics)
	return err
}

// MarkNodeLeft flips a node to the "left" state and revokes its token so the
// agent can no longer authenticate. Distinct from offline (PRD §4.4).
func (s *Store) MarkNodeLeft(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`update nodes set status = 'left', left_at = now(), token_hash = null
		  where id = $1 and status = 'enrolled'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetNodeOfflineAlerted records or clears the outstanding-offline-alert mark
// (F11). Persisted so a server restart neither re-alerts nor forgets.
func (s *Store) SetNodeOfflineAlerted(ctx context.Context, id string, alerted bool) error {
	q := `update nodes set alerted_offline_at = now() where id = $1`
	if !alerted {
		q = `update nodes set alerted_offline_at = null where id = $1`
	}
	_, err := s.pool.Exec(ctx, q, id)
	return err
}

// SetNodeDiskFullAlerted records or clears the outstanding low-flash-alert
// mark (F11), mirroring SetNodeOfflineAlerted.
func (s *Store) SetNodeDiskFullAlerted(ctx context.Context, id string, alerted bool) error {
	q := `update nodes set alerted_diskfull_at = now() where id = $1`
	if !alerted {
		q = `update nodes set alerted_diskfull_at = null where id = $1`
	}
	_, err := s.pool.Exec(ctx, q, id)
	return err
}

// SetNodeMemFullAlerted records or clears the outstanding memory-pressure
// alert mark, mirroring SetNodeDiskFullAlerted.
func (s *Store) SetNodeMemFullAlerted(ctx context.Context, id string, alerted bool) error {
	q := `update nodes set alerted_memfull_at = now() where id = $1`
	if !alerted {
		q = `update nodes set alerted_memfull_at = null where id = $1`
	}
	_, err := s.pool.Exec(ctx, q, id)
	return err
}

// SetNodeConfigBaseline records the accepted config fingerprint for drift
// detection (v1.0): on first contact, after a confirmed Logos change, or
// when the operator accepts the current state.
func (s *Store) SetNodeConfigBaseline(ctx context.Context, id, hash string) error {
	_, err := s.pool.Exec(ctx, `update nodes set config_baseline_hash = $2 where id = $1`, id, hash)
	return err
}

// DeleteNode removes a node record entirely (server-side data deletion, PRD §4.4).
func (s *Store) DeleteNode(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `delete from nodes where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
