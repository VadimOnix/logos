-- F6: short-retention metric history. One row per heartbeat sample with the
-- scalar readings the panel charts (load, memory, filesystem, aggregate
-- interface traffic). Kept small — a background janitor prunes old rows.

create table node_metrics_history (
    node_id       uuid        not null references nodes (id) on delete cascade,
    sampled_at    timestamptz not null default now(),
    load1         real,
    mem_used_pct  real,
    rootfs_used_pct real,
    rx_bytes      bigint,     -- cumulative across interfaces
    tx_bytes      bigint,
    dhcp_clients  int
);

-- History queries are always "one node, recent-first".
create index node_metrics_history_node_time on node_metrics_history (node_id, sampled_at desc);
