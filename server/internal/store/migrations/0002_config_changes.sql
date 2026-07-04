create table config_changes (
    id         bigserial primary key,
    node_id    uuid        not null references nodes (id) on delete cascade,
    kind       text        not null default 'apply', -- apply | rollback
    changes    jsonb       not null default '[]',
    snapshots  jsonb,                                -- config name → uci export text (pre-change)
    status     text        not null default 'applying', -- applying | confirmed | reverted | failed
    error      text        not null default '',
    created_by bigint      references users (id) on delete set null,
    created_at timestamptz not null default now(),
    decided_at timestamptz
);

create index config_changes_node_idx on config_changes (node_id, id desc);
