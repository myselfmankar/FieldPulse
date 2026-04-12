package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/myselfmankar/FieldPulse/internal/telemetry"
	"github.com/redis/go-redis/v9"
	"github.com/uptrace/uptrace-go/uptrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("ingest-worker")

func main() {
	log.Println("Ingest Worker initializing...")
	// 1. Configuration
	mqttBroker := os.Getenv("MQTT_BROKER_URL")
	if mqttBroker == "" {
		mqttBroker = "tls://localhost:8883"
	}
	dbURL := os.Getenv("TELEMETRY_DB_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:password@127.0.0.1:5433/telemetry_db?sslmode=disable"
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	// 2. Initialize OTEL (Requirement Page 1)
	upDSN := os.Getenv("UPTRACE_DSN")
	if upDSN != "" {
		uptrace.ConfigureOpentelemetry(
			uptrace.WithDSN(upDSN),
			uptrace.WithServiceName("ingest-worker"),
			uptrace.WithServiceVersion("1.0.0"),
			uptrace.WithDeploymentEnvironment("production"),
		)
		defer uptrace.Shutdown(context.Background())
	}

	// 2. Initialize Clients
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("failed to connect to timescale: %v", err)
	}
	defer dbPool.Close()

	rdb := redis.NewClient(&redis.Options{Addr: redisURL})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}

	repo := telemetry.NewRepository(dbPool)

	// 3. Setup MQTT with mTLS
	tlsConfig, err := loadTLSConfig("certs/ca.crt", "certs/client.crt", "certs/client.key")
	if err != nil {
		log.Fatalf("failed to load TLS config: %v", err)
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(mqttBroker)
	opts.SetClientID("ingest-worker-01")
	opts.SetTLSConfig(tlsConfig)
	opts.SetCleanSession(false)
	opts.SetAutoReconnect(true)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("failed to connect to mqtt: %v", token.Error())
	}

	// 4. Subscription Handler (The Processing Engine)
	handler := func(client mqtt.Client, msg mqtt.Message) {
		// Topic: devices/{device_id}/telemetry
		parts := strings.Split(msg.Topic(), "/")
		if len(parts) < 2 {
			return
		}
		deviceID := parts[1]

		// Parse the nested payload format
		rd, err := telemetry.ParseIncomingPayload(msg.Payload())
		if err != nil {
			log.Printf("[%s] dropping malformed payload: %v", deviceID, err)
			return
		}
		rd.DeviceID = deviceID

		// -- REQUIREMENT: TRACING (ROOT CAUSE FIX) --
		// Extract context from payload if it exists (from Connector)
		spanCtx := ctx
		if rd.TraceContext != nil {
			propagator := otel.GetTextMapPropagator()
			spanCtx = propagator.Extract(ctx, propagation.MapCarrier(rd.TraceContext))
		}

		// Start span for ingestion processing
		spanCtx, span := tracer.Start(spanCtx, "Ingest Reading", trace.WithAttributes(
			attribute.String("device_id", deviceID),
		), trace.WithSpanKind(trace.SpanKindConsumer))
		defer span.End()

		// -- REQUIREMENT: VALIDATION --
		if rd.Timestamp.After(time.Now().Add(5 * time.Second)) {
			log.Printf("[%s] dropping reading with future timestamp: %v", deviceID, rd.Timestamp)
			span.SetStatus(codes.Error, "future timestamp")
			return
		}

		// -- REQUIREMENT: DEDUPLICATION (REDIS) --
		// Using device_id + unix timestamp as the key
		dedupKey := fmt.Sprintf("dedup:%s:%d", deviceID, rd.Timestamp.Unix())
		isNew, err := rdb.SetNX(spanCtx, dedupKey, "1", 10*time.Minute).Result()
		if err != nil {
			log.Printf("[%s] redis dedup error: %v", deviceID, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, "redis error")
			return
		}
		if !isNew {
			log.Printf("[%s] dropping duplicate reading for ts %v", deviceID, rd.Timestamp)
			span.SetAttributes(attribute.Bool("duplicate", true))
			return
		}

		// -- REQUIREMENT: PERSISTENCE (TIMESCALE) --
		if err := repo.InsertReading(spanCtx, rd); err != nil {
			log.Printf("[%s] persistence error: %v", deviceID, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, "persistence error")
			return
		}

		span.SetStatus(codes.Ok, "ingested")
		log.Printf("[%s] ingested reading at %v (temp=%.1f, vibe=%.1f, cur=%.1f)",
			deviceID, rd.Timestamp, rd.Temperature, rd.Vibration, rd.CurrentMa)
	}

	if token := client.Subscribe("devices/+/telemetry", 1, handler); token.Wait() && token.Error() != nil {
		log.Fatalf("failed to subscribe: %v", token.Error())
	}

	log.Println("Ingestion Worker started. Listening for telemetry...")
	<-ctx.Done()
	log.Println("Shutting down worker...")
	client.Disconnect(250)
}

func loadTLSConfig(caFile, certFile, keyFile string) (*tls.Config, error) {
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, // For dev only: broker common name might not match local IP
	}, nil
}
