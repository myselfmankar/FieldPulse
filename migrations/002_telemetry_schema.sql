-- Telemetry Table (Time-Series Storage)
-- Page 11 Requirements: No ORM. Pure SQL.
CREATE TABLE IF NOT EXISTS telemetry (
    time TIMESTAMPTZ NOT NULL,
    device_id TEXT NOT NULL,
    temperature_c FLOAT,
    vibration_hz FLOAT,
    current_ma FLOAT
);

-- 1. Create Hypertable (Partitioned on 'time')
-- Page 9: Hypertable with justified chunk interval (1 day)
SELECT create_hypertable('telemetry', 'time', chunk_time_interval => INTERVAL '1 day', if_not_exists => TRUE);

-- Create index for device_id since we query per device frequently
CREATE INDEX IF NOT EXISTS idx_telemetry_device_time ON telemetry(device_id, time DESC);

-- 2. Compression Policy (Requirement: Page 9 - 7 days)
-- We compress older chunks to save space while keeping them queryable.
ALTER TABLE telemetry SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'device_id'
);
SELECT add_compression_policy('telemetry', INTERVAL '7 days', if_not_exists => TRUE);

-- 3. Retention Policy (Requirement: Page 9 - 90 days)
-- Auto-delete data older than 90 days.
SELECT add_retention_policy('telemetry', INTERVAL '90 days', if_not_exists => TRUE);

-- 4. Continuous Aggregate (Requirement: Page 9 - 1h resolution)
-- Pre-calculates 1-hour windows for faster dashboard queries.
CREATE MATERIALIZED VIEW IF NOT EXISTS telemetry_hourly
WITH (timescaledb.continuous) AS
SELECT 
    device_id,
    time_bucket(INTERVAL '1 hour', time) AS bucket,
    AVG(temperature_c) as avg_temp,
    MAX(temperature_c) as max_temp,
    MIN(temperature_c) as min_temp,
    AVG(vibration_hz) as avg_vibe,
    AVG(current_ma) as avg_current
FROM telemetry
GROUP BY device_id, bucket;

-- Refresh policy for the continuous aggregate
SELECT add_continuous_aggregate_policy('telemetry_hourly',
    start_offset => INTERVAL '3 hours',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists => TRUE);
