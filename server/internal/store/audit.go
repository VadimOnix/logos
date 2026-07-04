package store

import (
	"context"
	"time"
)

// AuditEntry is one recorded admin action (v1.0 basic audit log). The
// actor's email is denormalized so the trail survives user deletion.
type AuditEntry struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	UserEmail string    `json:"user_email"`
	Action    string    `json:"action"`
	Target    string    `json:"target,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

func (s *Store) InsertAudit(ctx context.Context, userEmail, action, target, detail string) error {
	_, err := s.pool.Exec(ctx,
		`insert into audit_log (user_email, action, target, detail) values ($1, $2, $3, $4)`,
		userEmail, action, target, detail)
	return err
}

// ListAudit returns the most recent entries, newest first.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]*AuditEntry, error) {
	rows, err := s.pool.Query(ctx,
		`select id, at, user_email, action, target, detail from audit_log order by at desc, id desc limit $1`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*AuditEntry{}
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(&e.ID, &e.At, &e.UserEmail, &e.Action, &e.Target, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneAudit deletes entries older than the cutoff, returning how many.
func (s *Store) PruneAudit(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `delete from audit_log where at < $1`, olderThan)
	return tag.RowsAffected(), err
}
