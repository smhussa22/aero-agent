-- time-series samples, one row per metric value (narrow layout so rules can address any metric by name)
CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE metrics (
    time    timestamptz      NOT NULL,
    host_id text             NOT NULL,
    metric  text             NOT NULL, -- e.g. cpu.usage_percent, disk.used_percent
    label   text             NOT NULL DEFAULT '', -- mount, interface, or process; empty for host-wide metrics
    value   double precision NOT NULL
);

SELECT create_hypertable('metrics', 'time', chunk_time_interval => INTERVAL '1 day');

CREATE INDEX metrics_lookup ON metrics (host_id, metric, label, time DESC);

ALTER TABLE metrics SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'host_id, metric, label'
);

SELECT add_compression_policy('metrics', INTERVAL '7 days');
SELECT add_retention_policy('metrics', INTERVAL '30 days');
