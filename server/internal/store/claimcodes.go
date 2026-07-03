package store

import (
	"context"
	"time"
)

type ClaimCode struct {
	ID        int64      `json:"id"`
	Note      string     `json:"note"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	NodeID    *string    `json:"node_id,omitempty"`
}

func (s *Store) CreateClaimCode(ctx context.Context, codeHash []byte, note string, createdBy int64, expiresAt time.Time) (*ClaimCode, error) {
	c := &ClaimCode{Note: note, ExpiresAt: expiresAt}
	err := s.pool.QueryRow(ctx,
		`insert into claim_codes (code_hash, note, created_by, expires_at)
		 values ($1, $2, $3, $4) returning id, created_at`,
		codeHash, note, createdBy, expiresAt).Scan(&c.ID, &c.CreatedAt)
	return c, err
}

func (s *Store) ListClaimCodes(ctx context.Context) ([]ClaimCode, error) {
	rows, err := s.pool.Query(ctx,
		`select id, note, created_at, expires_at, used_at, node_id::text
		   from claim_codes order by id desc limit 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ClaimCode{}
	for rows.Next() {
		var c ClaimCode
		if err := rows.Scan(&c.ID, &c.Note, &c.CreatedAt, &c.ExpiresAt, &c.UsedAt, &c.NodeID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ConsumeClaimCode atomically marks an unused, unexpired code as used by the
// given node. Codes are single-use and expiring by design (PRD §6 Security).
func (s *Store) ConsumeClaimCode(ctx context.Context, codeHash []byte, nodeID string) error {
	tag, err := s.pool.Exec(ctx,
		`update claim_codes set used_at = now(), node_id = $2
		  where code_hash = $1 and used_at is null and expires_at > now()`,
		codeHash, nodeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteClaimCode(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `delete from claim_codes where id = $1 and used_at is null`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
