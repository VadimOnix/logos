-- v1.0 (PRD §5.2): audit log of admin actions. CE-level basic audit: who
-- did what to which object, when. The actor's email is denormalized so the
-- trail survives user deletion; detail is free-form context (never secrets).

create table audit_log (
    id         bigint generated always as identity primary key,
    at         timestamptz not null default now(),
    user_email text        not null,
    action     text        not null, -- e.g. "node.remove", "overlay.create"
    target     text        not null default '', -- object id/name the action applied to
    detail     text        not null default ''
);

-- The panel reads "most recent first"; retention pruning uses the same order.
create index audit_log_at on audit_log (at desc);
