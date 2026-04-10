-- Device Registry Table
CREATE TABLE IF NOT EXISTS devices (
    device_id TEXT PRIMARY KEY,
    floor TEXT NOT NULL,
    device_type TEXT NOT NULL,
    firmware_ver TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active', -- active, silent, degraded
    certificate_pem TEXT NOT NULL, -- Storing the public cert for revocation checks
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for filtering by floor and status as requested in ListDevices
CREATE INDEX IF NOT EXISTS idx_devices_floor ON devices(floor);
CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);

-- Thresholds for the Alert Service
CREATE TABLE IF NOT EXISTS device_thresholds (
    device_id TEXT PRIMARY KEY REFERENCES devices(device_id) ON DELETE CASCADE,
    temp_max FLOAT,
    vibe_max FLOAT,
    current_max FLOAT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
