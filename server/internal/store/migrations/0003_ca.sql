-- Internal CA for agent mTLS (PRD §6: per-node certs). Single row; the key
-- lives with the rest of the control-plane secrets in Postgres. TODO(EE/HA):
-- envelope-encrypt the key with an external KMS secret.
create table ca (
    id         int primary key default 1 check (id = 1),
    cert_pem   text        not null,
    key_pem    text        not null,
    created_at timestamptz not null default now()
);

alter table nodes add column cert_not_after timestamptz;
