-- F10: remote terminal audit trail. Every session is recorded with who
-- opened it and when it ended; the session content itself is not stored.

create table terminal_sessions (
    id         bigserial primary key,
    node_id    uuid        not null references nodes (id) on delete cascade,
    user_email text        not null,
    started_at timestamptz not null default now(),
    ended_at   timestamptz,
    reason     text        not null default ''
);
