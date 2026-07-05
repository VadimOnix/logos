-- F7/v1.0: hub-and-spoke overlay topology. Null = full mesh (default).
-- When set, spokes peer only with the hub and route the whole overlay
-- through it — the relay path for peers that cannot reach each other
-- directly. Deleting the hub node falls back to mesh.

alter table overlays add column hub_node_id uuid references nodes (id) on delete set null;
