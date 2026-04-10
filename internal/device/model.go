package device

import "time"

// Device represents the domain model for an industrial sensor
type Device struct {
	ID             string    `json:"device_id"`
	Floor          string    `json:"floor"`
	Type           string    `json:"device_type"`
	FirmwareVer    string    `json:"firmware_ver"`
	Status         string    `json:"status"` // active, silent, degraded
	CertificatePEM string    `json:"certificate_pem"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Thresholds represents the alert limits for a device
type Thresholds struct {
	DeviceID   string  `json:"device_id"`
	TempMax    float64 `json:"temp_max"`
	VibeMax    float64 `json:"vibe_max"`
	CurrentMax float64 `json:"current_max"`
}
