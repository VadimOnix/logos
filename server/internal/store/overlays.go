package store

import (
	"context"
	"time"
)

// Overlay is a WireGuard overlay network (F7). The control plane coordinates
// it: membership, IP assignment and public-key distribution live here; the
// private keys stay on the devices.
type Overlay struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CIDR      string    `json:"cidr"`
	CreatedAt time.Time `json:"created_at"`
	// HubNodeID switches the overlay to hub-and-spoke topology: spokes
	// peer only with this node and route the overlay through it. Nil =
	// full mesh.
	HubNodeID *string `json:"hub_node_id,omitempty"`
}

// OverlayMember is one node's slot in an overlay, joined with the node fields
// the sync engine needs (name/status for display, last_addr for endpoints).
type OverlayMember struct {
	OverlayID  int64     `json:"overlay_id"`
	NodeID     string    `json:"node_id"`
	OverlayIP  string    `json:"overlay_ip"`
	PublicKey  string    `json:"public_key"`
	ListenPort int       `json:"listen_port"`
	Subnets    []string  `json:"subnets"`
	SyncError  string    `json:"sync_error,omitempty"`
	JoinedAt   time.Time `json:"joined_at"`

	NodeName   string `json:"node_name"`
	NodeStatus string `json:"node_status"`
	NodeAddr   string `json:"node_addr,omitempty"`
}

func (s *Store) CreateOverlay(ctx context.Context, name, cidr string) (*Overlay, error) {
	o := &Overlay{}
	err := s.pool.QueryRow(ctx,
		`insert into overlays (name, cidr) values ($1, $2) returning id, name, cidr, created_at, hub_node_id`,
		name, cidr).Scan(&o.ID, &o.Name, &o.CIDR, &o.CreatedAt, &o.HubNodeID)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Store) GetOverlay(ctx context.Context, id int64) (*Overlay, error) {
	o := &Overlay{}
	err := s.pool.QueryRow(ctx,
		`select id, name, cidr, created_at, hub_node_id from overlays where id = $1`, id).
		Scan(&o.ID, &o.Name, &o.CIDR, &o.CreatedAt, &o.HubNodeID)
	if noRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Store) ListOverlays(ctx context.Context) ([]*Overlay, error) {
	rows, err := s.pool.Query(ctx, `select id, name, cidr, created_at, hub_node_id from overlays order by id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Overlay{}
	for rows.Next() {
		o := &Overlay{}
		if err := rows.Scan(&o.ID, &o.Name, &o.CIDR, &o.CreatedAt, &o.HubNodeID); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SetOverlayHub switches an overlay between hub-and-spoke (node id) and
// full mesh (nil).
func (s *Store) SetOverlayHub(ctx context.Context, overlayID int64, nodeID *string) error {
	tag, err := s.pool.Exec(ctx, `update overlays set hub_node_id = $2 where id = $1`, overlayID, nodeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteOverlay(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `delete from overlays where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const memberCols = `m.overlay_id, m.node_id, m.overlay_ip, m.public_key, m.listen_port,
	m.subnets, m.sync_error, m.joined_at, n.name, n.status, n.last_addr`

func scanMember(row interface{ Scan(...any) error }) (*OverlayMember, error) {
	m := &OverlayMember{}
	err := row.Scan(&m.OverlayID, &m.NodeID, &m.OverlayIP, &m.PublicKey, &m.ListenPort,
		&m.Subnets, &m.SyncError, &m.JoinedAt, &m.NodeName, &m.NodeStatus, &m.NodeAddr)
	if noRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Store) ListOverlayMembers(ctx context.Context, overlayID int64) ([]*OverlayMember, error) {
	rows, err := s.pool.Query(ctx,
		`select `+memberCols+` from overlay_members m join nodes n on n.id = m.node_id
		  where m.overlay_id = $1 order by m.joined_at`, overlayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*OverlayMember{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListNodeOverlayIDs reports which overlays a node belongs to — used to
// reconcile the node's WireGuard interfaces when its agent (re)connects.
func (s *Store) ListNodeOverlayIDs(ctx context.Context, nodeID string) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`select overlay_id from overlay_members where node_id = $1 order by overlay_id`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) UsedOverlayIPs(ctx context.Context, overlayID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`select overlay_ip from overlay_members where overlay_id = $1`, overlayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		out = append(out, ip)
	}
	return out, rows.Err()
}

func (s *Store) AddOverlayMember(ctx context.Context, overlayID int64, nodeID, ip string, listenPort int, subnets []string) (*OverlayMember, error) {
	row := s.pool.QueryRow(ctx,
		`with ins as (
		    insert into overlay_members (overlay_id, node_id, overlay_ip, listen_port, subnets)
		    values ($1, $2, $3, $4, $5) returning *
		 )
		 select `+memberCols+` from ins m join nodes n on n.id = m.node_id`,
		overlayID, nodeID, ip, listenPort, subnets)
	return scanMember(row)
}

func (s *Store) RemoveOverlayMember(ctx context.Context, overlayID int64, nodeID string) error {
	tag, err := s.pool.Exec(ctx,
		`delete from overlay_members where overlay_id = $1 and node_id = $2`, overlayID, nodeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetOverlayMemberKey records the WireGuard public key the agent reported.
func (s *Store) SetOverlayMemberKey(ctx context.Context, overlayID int64, nodeID, publicKey string) error {
	_, err := s.pool.Exec(ctx,
		`update overlay_members set public_key = $3 where overlay_id = $1 and node_id = $2`,
		overlayID, nodeID, publicKey)
	return err
}

// SetOverlayMemberSyncError records the last sync outcome ("" = success) so
// the panel can surface e.g. "wireguard-tools not installed".
func (s *Store) SetOverlayMemberSyncError(ctx context.Context, overlayID int64, nodeID, msg string) error {
	_, err := s.pool.Exec(ctx,
		`update overlay_members set sync_error = $3 where overlay_id = $1 and node_id = $2`,
		overlayID, nodeID, msg)
	return err
}

// SetNodeAddr records the source address of the agent's channel — the best
// candidate WireGuard endpoint other peers can dial.
func (s *Store) SetNodeAddr(ctx context.Context, nodeID, addr string) error {
	_, err := s.pool.Exec(ctx, `update nodes set last_addr = $2 where id = $1`, nodeID, addr)
	return err
}
