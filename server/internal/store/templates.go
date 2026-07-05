package store

import (
	"context"
	"encoding/json"
	"time"
)

// ConfigTemplate is a reusable list of UCI operations with ${var}
// placeholders (v1.0). Body is the JSON array of changes exactly as
// submitted; rendering happens at apply time in the API layer.
type ConfigTemplate struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Body      json.RawMessage `json:"changes"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Store) CreateConfigTemplate(ctx context.Context, name string, body []byte) (*ConfigTemplate, error) {
	t := &ConfigTemplate{Name: name, Body: body}
	err := s.pool.QueryRow(ctx,
		`insert into config_templates (name, body) values ($1, $2) returning id, created_at`,
		name, body).Scan(&t.ID, &t.CreatedAt)
	return t, err
}

func (s *Store) GetConfigTemplate(ctx context.Context, id int64) (*ConfigTemplate, error) {
	t := &ConfigTemplate{}
	err := s.pool.QueryRow(ctx,
		`select id, name, body, created_at from config_templates where id = $1`, id).
		Scan(&t.ID, &t.Name, &t.Body, &t.CreatedAt)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *Store) ListConfigTemplates(ctx context.Context) ([]*ConfigTemplate, error) {
	rows, err := s.pool.Query(ctx,
		`select id, name, body, created_at from config_templates order by name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ConfigTemplate{}
	for rows.Next() {
		t := &ConfigTemplate{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Body, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DeleteConfigTemplate(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `delete from config_templates where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
