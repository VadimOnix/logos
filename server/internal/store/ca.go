package store

import "context"

// GetCA returns the stored CA PEMs, or ErrNotFound on first start.
func (s *Store) GetCA(ctx context.Context) (certPEM, keyPEM string, err error) {
	err = s.pool.QueryRow(ctx, `select cert_pem, key_pem from ca where id = 1`).Scan(&certPEM, &keyPEM)
	if noRows(err) {
		return "", "", ErrNotFound
	}
	return certPEM, keyPEM, err
}

// SaveCA persists the CA exactly once; a concurrent first-start loses the
// race and re-reads the winner's CA.
func (s *Store) SaveCA(ctx context.Context, certPEM, keyPEM string) (won bool, err error) {
	tag, err := s.pool.Exec(ctx,
		`insert into ca (id, cert_pem, key_pem) values (1, $1, $2) on conflict (id) do nothing`,
		certPEM, keyPEM)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// SetNodeCertNotAfter records the expiry of the node's current client cert.
func (s *Store) SetNodeCertNotAfter(ctx context.Context, nodeID string, notAfter any) error {
	_, err := s.pool.Exec(ctx, `update nodes set cert_not_after = $2 where id = $1`, nodeID, notAfter)
	return err
}
