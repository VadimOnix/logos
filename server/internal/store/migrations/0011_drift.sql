-- v1.0 (PRD §5.2 "drift detection"): fingerprint of the node's config the
-- operator considers canonical. Heartbeats carry the live hash; a mismatch
-- means the config changed outside Logos (e.g. local LuCI edits). Set on
-- first contact, re-set when a Logos change confirms or the operator
-- accepts the current state.

alter table nodes add column config_baseline_hash text;
