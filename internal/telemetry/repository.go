package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// InsertReading saves a new sensor reading into the hypertable
// Constraint: time column is used as partitioned time.
func (r *Repository) InsertReading(ctx context.Context, rd *Reading) error {
	const query = `
		INSERT INTO telemetry (device_id, "time", temperature_c, vibration_hz, current_ma)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.db.Exec(ctx, query, rd.DeviceID, rd.Timestamp, rd.Temperature, rd.Vibration, rd.CurrentMa)
	if err != nil {
		return fmt.Errorf("failed to insert telemetry: %w", err)
	}
	return nil
}

// GetLatestReading fetches the most recent data point for a device
func (r *Repository) GetLatestReading(ctx context.Context, deviceID string) (*Reading, error) {
	const query = `
		SELECT device_id, "time", temperature_c, vibration_hz, current_ma
		FROM telemetry
		WHERE device_id = $1
		ORDER BY "time" DESC
		LIMIT 1
	`
	var rd Reading
	err := r.db.QueryRow(ctx, query, deviceID).Scan(
		&rd.DeviceID, &rd.Timestamp, &rd.Temperature, &rd.Vibration, &rd.CurrentMa,
	)
	if err != nil {
		return nil, err
	}
	return &rd, nil
}

// QueryAggregates returns time-bucketed stats (1m, 5m, 1h)
// Requirement (Page 11): No ORM. Pure SQL.
func (r *Repository) QueryAggregates(ctx context.Context, deviceID string, resolution string, from, to time.Time, limit, offset int32) ([]*Aggregate, error) {
	var bucketInterval string
	switch resolution {
	case "1m":
		bucketInterval = "1 minute"
	case "5m":
		bucketInterval = "5 minutes"
	case "1h":
		bucketInterval = "1 hour"
	default:
		return nil, fmt.Errorf("unsupported resolution: %s", resolution)
	}

	if limit <= 0 { limit = 100 } // Safety default

	query := fmt.Sprintf(`
		SELECT 
			time_bucket('%s', "time") AS bucket,
			AVG(temperature_c),
			MAX(temperature_c),
			MIN(temperature_c),
			AVG(vibration_hz),
			AVG(current_ma)
		FROM telemetry
		WHERE device_id = $1 AND "time" >= $2 AND "time" <= $3
		GROUP BY bucket
		ORDER BY bucket ASC
		LIMIT $4 OFFSET $5
	`, bucketInterval)

	rows, err := r.db.Query(ctx, query, deviceID, from, to, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aggs []*Aggregate
	for rows.Next() {
		var a Aggregate
		a.DeviceID = deviceID
		if err := rows.Scan(&a.Bucket, &a.AvgTemp, &a.MaxTemp, &a.MinTemp, &a.AvgVibration, &a.AvgCurrent); err != nil {
			return nil, err
		}
		aggs = append(aggs, &a)
	}

	return aggs, nil
}
