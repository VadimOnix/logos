-- F11 extension (v1.0 alert-rules increment): outstanding memory-pressure
-- alert mark, mirroring alerted_offline_at / alerted_diskfull_at.

alter table nodes add column alerted_memfull_at timestamptz;
