-- F7: WireGuard overlay networks. The control plane is the coordinator:
-- it owns membership, IP assignment (IPAM) and public-key distribution;
-- private keys never leave the devices.

alter table nodes add column last_addr text not null default '';

create table overlays (
    id         bigserial primary key,
    name       text        not null unique,
    cidr       text        not null,
    created_at timestamptz not null default now()
);

create table overlay_members (
    overlay_id  bigint      not null references overlays (id) on delete cascade,
    node_id     uuid        not null references nodes (id) on delete cascade,
    overlay_ip  text        not null,
    public_key  text        not null default '', -- reported by the agent after first sync
    listen_port int         not null,
    subnets     text[]      not null default '{}', -- LANs this node advertises (subnet-router mode)
    sync_error  text        not null default '',
    joined_at   timestamptz not null default now(),
    primary key (overlay_id, node_id),
    unique (overlay_id, overlay_ip)
);
