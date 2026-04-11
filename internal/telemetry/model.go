package telemetry

import (
	"time"
)

// Reading represents a single telemetry message from a sensor
type Reading struct {
	DeviceID      string    `json:"device_id"`
	Timestamp     time.Time `json:"timestamp"`
	Temperature   float64   `json:"temperature_c"`
	Vibration     float64   `json:"vibration_hz"`
	CurrentMa     float64   `json:"current_ma"`
	TraceContext  map[string]string `json:"_trace_context"`
}

// Aggregate represents bucketed calculations (min/max/avg)
type Aggregate struct {
	DeviceID     string    `json:"device_id"`
	Bucket       time.Time `json:"bucket"`
	AvgTemp      float64   `json:"avg_temp"`
	MaxTemp      float64   `json:"max_temp"`
	MinTemp      float64   `json:"min_temp"`
	AvgVibration float64   `json:"avg_vibration"`
	AvgCurrent   float64   `json:"avg_current"`
}
