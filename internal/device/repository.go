package device

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository handles database operations for the Device Registry
type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// CreateDevice persists a new device to PostgreSQL
func (r *Repository) CreateDevice(ctx context.Context, d *Device) error {
	const query = `
		INSERT INTO devices (device_id, floor, device_type, firmware_ver, status, certificate_pem)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.Exec(ctx, query, d.ID, d.Floor, d.Type, d.FirmwareVer, d.Status, d.CertificatePEM)
	if err != nil {
		return fmt.Errorf("failed to insert device: %w", err)
	}
	return nil
}

// GetDevice retrieves a device by its unique ID
func (r *Repository) GetDevice(ctx context.Context, id string) (*Device, error) {
	const query = `
		SELECT device_id, floor, device_type, firmware_ver, status, created_at, updated_at
		FROM devices WHERE device_id = $1
	`
	var d Device
	err := r.db.QueryRow(ctx, query, id).Scan(
		&d.ID, &d.Floor, &d.Type, &d.FirmwareVer, &d.Status, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("device not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get device: %w", err)
	}
	return &d, nil
}

// ListDevices retrieves a filtered list of devices
func (r *Repository) ListDevices(ctx context.Context, floor, status string) ([]*Device, error) {
	query := `SELECT device_id, floor, device_type, firmware_ver, status, created_at FROM devices WHERE 1=1`
	args := []any{}
	argCount := 1

	if floor != "" {
		query += fmt.Sprintf(" AND floor = $%d", argCount)
		args = append(args, floor)
		argCount++
	}
	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", argCount)
		args = append(args, status)
		argCount++
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list devices: %w", err)
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Floor, &d.Type, &d.FirmwareVer, &d.Status, &d.CreatedAt); err != nil {
			return nil, err
		}
		devices = append(devices, &d)
	}

	return devices, nil
}

// UpdateThresholds updates or inserts alert limits for a device
// Requirement: Dynamic threshold management (Page 2)
func (r *Repository) UpdateThresholds(ctx context.Context, t *Thresholds) error {
	const query = `
		INSERT INTO device_thresholds (device_id, temp_max, vibe_max, current_max, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (device_id) DO UPDATE SET
			temp_max = EXCLUDED.temp_max,
			vibe_max = EXCLUDED.vibe_max,
			current_max = EXCLUDED.current_max,
			updated_at = NOW()
	`
	_, err := r.db.Exec(ctx, query, t.DeviceID, t.TempMax, t.VibeMax, t.CurrentMax)
	if err != nil {
		return fmt.Errorf("failed to update thresholds: %w", err)
	}
	return nil
}

// GetThresholds retrieves alert limits for a device
func (r *Repository) GetThresholds(ctx context.Context, id string) (*Thresholds, error) {
	const query = `
		SELECT device_id, temp_max, vibe_max, current_max
		FROM device_thresholds WHERE device_id = $1
	`
	var t Thresholds
	err := r.db.QueryRow(ctx, query, id).Scan(&t.DeviceID, &t.TempMax, &t.VibeMax, &t.CurrentMax)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Return default thresholds (unlimited) if none set
			return &Thresholds{DeviceID: id, TempMax: 999, VibeMax: 999, CurrentMax: 9999}, nil
		}
		return nil, fmt.Errorf("failed to get thresholds: %w", err)
	}
	return &t, nil
}

// UpdateStatus updates a device's status (active, silent, degraded)
func (r *Repository) UpdateStatus(ctx context.Context, id string, status string) error {
	const query = `UPDATE devices SET status = $1, updated_at = NOW() WHERE device_id = $2`
	_, err := r.db.Exec(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}
	return nil
}
