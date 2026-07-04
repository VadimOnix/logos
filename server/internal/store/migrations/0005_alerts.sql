-- F11: node-offline alerting. The timestamp records that an offline alert
-- went out, so the watcher neither repeats it nor loses track across server
-- restarts; it is cleared (after a recovery notice) when the node returns.

alter table nodes add column alerted_offline_at timestamptz;
