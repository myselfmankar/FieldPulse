package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/myselfmankar/FieldPulse/internal/device"
	pb "github.com/myselfmankar/FieldPulse/proto/device"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceFlow_Integration(t *testing.T) {
	// 1. Setup Connection (Requirement: No Mocking)
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/device_registry"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("Skipping integration test: Database not reachable at %s. Ensure Docker is running.", dbURL)
	}
	defer dbPool.Close()

	// 2. Initialize Service Stack
	// Use relative paths that work from the workspace root
	caCertPath := "certs/ca.crt"
	caKeyPath := "certs/ca.key"
	
	repo := device.NewRepository(dbPool)
	svc, err := device.NewService(repo, caCertPath, caKeyPath, "mqtts://localhost:8883")
	require.NoError(t, err, "Failed to initialize device service (ensure you ran bootstrap-ca)")

	// 3. Test CreateDevice
	deviceID := "int-test-device-" + time.Now().Format("150405")
	req := &pb.CreateDeviceRequest{
		DeviceId:    deviceID,
		Floor:       "Basement-1",
		DeviceType:  "vibration",
		FirmwareVer: "v0.9.0-test",
	}

	resp, err := svc.CreateDevice(ctx, req)
	
	// Critical Assertion: Stop if creation failed
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, deviceID, resp.DeviceId)
	assert.NotEmpty(t, resp.ClientCertPem)

	// 4. Verify Persistence in Real Postgres
	dbDevice, err := repo.GetDevice(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, "Basement-1", dbDevice.Floor)
	assert.Equal(t, "active", dbDevice.Status)

	// 5. Test Duplicate Prevention (Requirement Page 7)
	_, err = svc.CreateDevice(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}
