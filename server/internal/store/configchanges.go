package store

import (
	"context"
	"encoding/json"
	"time"
)

// Config-change lifecycle (F4): every push is a versioned row. "applying"
// means the agent has applied it and the auto-revert watchdog is armed;
// the row is decided as confirmed (connectivity proven), reverted (watchdog
// fired), or failed (agent rejected it).
const (
	ChangeStatusApplying  = "applying"
	ChangeStatusConfirmed = "confirmed"
	ChangeStatusReverted  = "reverted"
	ChangeStatusFailed    = "failed"
)

type ConfigChange struct {
	ID        int64           `json:"id"`
	NodeID    string          `json:"node_id"`
	Kind      string          `json:"kind"`
	Changes   json.RawMessage `json:"changes"`
	Snapshots json.RawMessage `json:"snapshots,omitempty"`
	Status    string          `json:"status"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	DecidedAt *time.Time      `json:"decided_at,omitempty"`
}

func (s *Store) CreateConfigChange(ctx context.Context, nodeID, kind string, changes json.RawMessage, createdBy int64) (*ConfigChange, error) {
	c := &ConfigChange{NodeID: nodeID, Kind: kind, Changes: changes, Status: ChangeStatusApplying}
	err := s.pool.QueryRow(ctx,
		`insert into config_changes (node_id, kind, changes, created_by)
		 values ($1, $2, $3, $4) returning id, created_at`,
		nodeID, kind, changes, createdBy).Scan(&c.ID, &c.CreatedAt)
	return c, err
}

func (s *Store) GetConfigChange(ctx context.Context, nodeID string, id int64) (*ConfigChange, error) {
	c := &ConfigChange{}
	err := s.pool.QueryRow(ctx,
		`select id, node_id, kind, changes, snapshots, status, error, created_at, decided_at
		   from config_changes where id = $1 and node_id = $2`, id, nodeID).
		Scan(&c.ID, &c.NodeID, &c.Kind, &c.Changes, &c.Snapshots, &c.Status, &c.Error, &c.CreatedAt, &c.DecidedAt)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Store) ListConfigChanges(ctx context.Context, nodeID string, limit int) ([]ConfigChange, error) {
	rows, err := s.pool.Query(ctx,
		`select id, node_id, kind, changes, snapshots, status, error, created_at, decided_at
		   from config_changes where node_id = $1 order by id desc limit $2`, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConfigChange{}
	for rows.Next() {
		var c ConfigChange
		if err := rows.Scan(&c.ID, &c.NodeID, &c.Kind, &c.Changes, &c.Snapshots, &c.Status, &c.Error, &c.CreatedAt, &c.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) SetConfigChangeSnapshots(ctx context.Context, id int64, snapshots json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `update config_changes set snapshots = $2 where id = $1`, id, snapshots)
	return err
}

// DecideConfigChange finalizes a change, but only while it is still
// "applying" — decisions are single-shot (the confirm goroutine and the
// hello-reconciliation path can race).
func (s *Store) DecideConfigChange(ctx context.Context, id int64, status, errText string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`update config_changes set status = $2, error = $3, decided_at = now()
		  where id = $1 and status = 'applying'`, id, status, errText)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
