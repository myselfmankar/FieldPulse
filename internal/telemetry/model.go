package telemetry

import (
	"encoding/json"
	"time"
)

// Reading represents a single telemetry message from a sensor
type Reading struct {
	DeviceID      string    `json:"device_id"`
	Timestamp     time.Time `json:"timestamp"`
	Temperature   float64   `json:"temperature_c"`
	Vibration     float64   `json:"vibration_hz"`
	CurrentMa     float64   `json:"current_ma"`
	TraceContext  map[string]string `json:"_trace_context,omitempty"`
}

// IncomingPayload matches the wire format sent by the Simulator and Connector,
// where metrics are nested inside a "metrics" object.
type IncomingPayload struct {
	DeviceID     string            `json:"device_id"`
	Timestamp    time.Time         `json:"timestamp"`
	Metrics      MetricsPayload    `json:"metrics"`
	TraceContext map[string]string `json:"_trace_context,omitempty"`
}

// MetricsPayload is the nested metrics structure in the incoming JSON.
type MetricsPayload struct {
	Temperature float64 `json:"temperature_c"`
	Vibration   float64 `json:"vibration_hz"`
	CurrentMa   float64 `json:"current_ma"`
}

// ParseIncomingPayload deserializes a raw MQTT payload into a Reading.
// Handles the nested "metrics" wire format.
func ParseIncomingPayload(raw []byte) (*Reading, error) {
	var p IncomingPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}

	return &Reading{
		DeviceID:     p.DeviceID,
		Timestamp:    p.Timestamp,
		Temperature:  p.Metrics.Temperature,
		Vibration:    p.Metrics.Vibration,
		CurrentMa:    p.Metrics.CurrentMa,
		TraceContext: p.TraceContext,
	}, nil
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
