-- F11 (extension): low-flash alerting. Mirrors alerted_offline_at — the
-- timestamp records that a disk-usage alert went out for the node, so the
-- watcher neither repeats it nor loses track across restarts; it is cleared
-- (after a recovery notice) once usage falls back below the threshold.

alter table nodes add column alerted_diskfull_at timestamptz;
