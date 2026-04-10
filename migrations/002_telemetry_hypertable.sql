-- Telemetry Table
CREATE TABLE IF NOT EXISTS telemetry (
    time TIMESTAMPTZ NOT NULL,
    device_id TEXT NOT NULL,
    temperature_c FLOAT,
    vibration_hz FLOAT,
    current_ma FLOAT
);

-- Convert to Hypertable (TimescaleDB specific)
-- We partition by 'time'.
SELECT create_hypertable('telemetry', 'time', if_not_exists => TRUE);

-- Create index for device_id since we query per device frequently
CREATE INDEX IF NOT EXISTS idx_telemetry_device_time ON telemetry(device_id, time DESC);

-- Set compression policy (after 7 days)
ALTER TABLE telemetry SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'device_id'
);
SELECT add_compression_policy('telemetry', INTERVAL '7 days', if_not_exists => TRUE);

-- Set retention policy (90 days)
SELECT add_retention_policy('telemetry', INTERVAL '90 days', if_not_exists => TRUE);

-- Continuous Aggregate for 1h resolution as requested
CREATE MATERIALIZED VIEW IF NOT EXISTS telemetry_hourly
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 hour', time) AS bucket,
       device_id,
       avg(temperature_c) as avg_temp,
       max(temperature_c) as max_temp,
       min(temperature_c) as min_temp,
       avg(vibration_hz) as avg_vibe,
       avg(current_ma) as avg_current
FROM telemetry
GROUP BY bucket, device_id;

SELECT add_continuous_aggregate_policy('telemetry_hourly',
    start_offset => INTERVAL '3 hours',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists => TRUE);
