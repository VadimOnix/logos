create table users (
    id            bigserial primary key,
    email         text        not null unique,
    password_hash text        not null,
    created_at    timestamptz not null default now()
);

create table sessions (
    token_hash bytea       primary key,
    user_id    bigint      not null references users (id) on delete cascade,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null
);

create table api_tokens (
    id           bigserial primary key,
    user_id      bigint      not null references users (id) on delete cascade,
    name         text        not null,
    token_hash   bytea       not null unique,
    created_at   timestamptz not null default now(),
    last_used_at timestamptz
);

create table nodes (
    id            uuid primary key,
    name          text        not null,
    token_hash    bytea unique,
    public_key    text        not null default '',
    hostname      text        not null default '',
    agent_version text        not null default '',
    os_version    text        not null default '',
    arch          text        not null default '',
    status        text        not null default 'enrolled', -- enrolled | left
    enrolled_at   timestamptz not null default now(),
    left_at       timestamptz,
    last_seen_at  timestamptz,
    last_metrics  jsonb
);

create table claim_codes (
    id         bigserial primary key,
    code_hash  bytea       not null unique,
    note       text        not null default '',
    created_by bigint      references users (id) on delete set null,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null,
    used_at    timestamptz,
    node_id    uuid        references nodes (id) on delete set null
);
