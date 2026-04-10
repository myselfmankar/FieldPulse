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
