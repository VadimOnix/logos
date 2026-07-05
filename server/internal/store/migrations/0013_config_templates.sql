-- v1.0 (PRD §5.2 "config templates with variables"): a named, reusable list
-- of UCI operations with ${var} placeholders, rendered per node at apply
-- time and pushed through the same versioned auto-revert machinery as any
-- other config change.

create table config_templates (
    id         bigint generated always as identity primary key,
    name       text        not null unique,
    body       jsonb       not null, -- [{op, key, value}] with ${var} placeholders
    created_at timestamptz not null default now()
);
