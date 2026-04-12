package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	devicepb "github.com/myselfmankar/FieldPulse/proto/device"
	"github.com/uptrace/uptrace-go/uptrace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func main() {
	// 0. Observability (Uptrace/OpenTelemetry)
	upDSN := os.Getenv("UPTRACE_DSN")
	if upDSN != "" {
		uptrace.ConfigureOpentelemetry(
			uptrace.WithDSN(upDSN),
			uptrace.WithServiceName("status-watchdog"),
			uptrace.WithServiceVersion("1.0.0"),
		)
		defer uptrace.Shutdown(context.Background())
	}

	// 1. Config
	dbURL := os.Getenv("TELEMETRY_DB_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:password@127.0.0.1:5433/telemetry_db?sslmode=disable"
	}
	deviceSvcAddr := os.Getenv("DEVICE_SERVICE_ADDR")
	if deviceSvcAddr == "" {
		deviceSvcAddr = "127.0.0.1:50051"
	}

	// 2. Clients
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("watchdog connect db fail: %v", err)
	}
	defer dbPool.Close()

	conn, err := grpc.Dial(deviceSvcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("watchdog connect grpc fail: %v", err)
	}
	defer conn.Close()
	deviceClient := devicepb.NewDeviceServiceClient(conn)

	// 3. Loop
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Println("Status Watchdog started. Checking for silent devices every 30s...")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkDeviceHealth(ctx, dbPool, deviceClient)
		}
	}
}

func checkDeviceHealth(ctx context.Context, db *pgxpool.Pool, client devicepb.DeviceServiceClient) {
	// Requirement: Mark silent after 60s (Page 2)
	const query = `
		SELECT device_id
		FROM (
			SELECT device_id, MAX(time) as last_ts
			FROM telemetry
			GROUP BY device_id
		) last_readings
		WHERE last_ts < (NOW() - INTERVAL '60 seconds')
	`
	rows, err := db.Query(ctx, query)
	if err != nil {
		log.Printf("watchdog query error: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var deviceID string
		if err := rows.Scan(&deviceID); err != nil {
			continue
		}

		// Update to silent
		log.Printf("Watchdog: Device %s has timed out. Marking as silent.", deviceID)
		_, err := client.UpdateDeviceStatus(ctx, &devicepb.UpdateDeviceStatusRequest{
			DeviceId: deviceID,
			Status:   "silent",
		})
		if err != nil {
			st, ok := status.FromError(err)
			if ok && st.Code() == codes.NotFound {
				log.Printf("Watchdog: Device %s not found in registry. Skipping stale record.", deviceID)
				continue
			}
			log.Printf("watchdog update fail for %s: %v", deviceID, err)
		}
	}
}
