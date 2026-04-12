package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5/pgxpool"
	devicepb "github.com/myselfmankar/FieldPulse/proto/device"
	"github.com/uptrace/uptrace-go/uptrace"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var tracer = otel.Tracer("status-watchdog")

func main() {
	log.Println("Watchdog initializing...")
	upDSN := os.Getenv("UPTRACE_DSN")
	if upDSN != "" {
		uptrace.ConfigureOpentelemetry(
			uptrace.WithDSN(upDSN),
			uptrace.WithServiceName("status-watchdog"),
		)
		defer uptrace.Shutdown(context.Background())
	}

	dbURL := os.Getenv("TELEMETRY_DB_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:password@127.0.0.1:5433/telemetry_db?sslmode=disable"
	}
	deviceSvcAddr := os.Getenv("DEVICE_SERVICE_ADDR")
	if deviceSvcAddr == "" {
		deviceSvcAddr = "127.0.0.1:50051"
	}
	mqttBroker := os.Getenv("MQTT_BROKER_URL")
	if mqttBroker == "" {
		mqttBroker = "tls://localhost:8883"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil { log.Fatalf("watchdog connect db fail: %v", err) }

	conn, err := grpc.Dial(deviceSvcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil { log.Fatalf("watchdog connect grpc fail: %v", err) }
	deviceClient := devicepb.NewDeviceServiceClient(conn)

	tlsConfig, err := loadTLSConfig("certs/ca.crt", "certs/client.crt", "certs/client.key")
	if err != nil { log.Fatalf("watchdog load tls fail: %v", err) }
	
	opts := mqtt.NewClientOptions().AddBroker(mqttBroker).SetTLSConfig(tlsConfig).SetClientID("watchdog")
	mqttClient := mqtt.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("watchdog mqtt connect fail: %v", token.Error())
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	log.Println("Status Watchdog started. Checking for silent devices (>15m) every minute...")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkDeviceHealth(ctx, dbPool, deviceClient, mqttClient)
		}
	}
}

func checkDeviceHealth(ctx context.Context, db *pgxpool.Pool, client devicepb.DeviceServiceClient, mq mqtt.Client) {
	_, span := tracer.Start(ctx, "Health Check Cycle", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	const query = `
		SELECT device_id FROM (
			SELECT device_id, MAX(time) as last_ts FROM telemetry GROUP BY device_id
		) last_readings WHERE last_ts < (NOW() - INTERVAL '15 minutes')
	`
	rows, err := db.Query(ctx, query)
	if err != nil { return }
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil { continue }

		_, err := client.UpdateDeviceStatus(ctx, &devicepb.UpdateDeviceStatusRequest{
			DeviceId: id,
			Status:   "silent",
		})
		if err != nil { continue }

		event := map[string]string{
			"device_id": id,
			"event":     "device_timed_out",
			"status":    "silent",
			"timestamp": time.Now().Format(time.RFC3339),
		}
		payload, _ := json.Marshal(event)
		mq.Publish("device/silent/"+id, 1, false, payload)
		log.Printf("Watchdog: Device %s marked SILENT.", id)
	}
}

func loadTLSConfig(caFile, certFile, keyFile string) (*tls.Config, error) {
	caCert, err := os.ReadFile(caFile)
	if err != nil { return nil, err }
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil { return nil, err }
	return &tls.Config{
		RootCAs: caCertPool,
		Certificates: []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}, nil
}
