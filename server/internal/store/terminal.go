package store

import (
	"context"
	"time"
)

// TerminalSession is one audited remote-terminal session (F10).
type TerminalSession struct {
	ID        int64      `json:"id"`
	NodeID    string     `json:"node_id"`
	UserEmail string     `json:"user_email"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Reason    string     `json:"reason,omitempty"`
}

func (s *Store) CreateTerminalSession(ctx context.Context, nodeID, userEmail string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`insert into terminal_sessions (node_id, user_email) values ($1, $2) returning id`,
		nodeID, userEmail).Scan(&id)
	return id, err
}

func (s *Store) CloseTerminalSession(ctx context.Context, id int64, reason string) error {
	_, err := s.pool.Exec(ctx,
		`update terminal_sessions set ended_at = now(), reason = $2 where id = $1 and ended_at is null`,
		id, reason)
	return err
}

func (s *Store) ListTerminalSessions(ctx context.Context, nodeID string, limit int) ([]*TerminalSession, error) {
	rows, err := s.pool.Query(ctx,
		`select id, node_id, user_email, started_at, ended_at, reason
		   from terminal_sessions where node_id = $1 order by started_at desc limit $2`,
		nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TerminalSession{}
	for rows.Next() {
		t := &TerminalSession{}
		if err := rows.Scan(&t.ID, &t.NodeID, &t.UserEmail, &t.StartedAt, &t.EndedAt, &t.Reason); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
