package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	devicepb "github.com/myselfmankar/FieldPulse/proto/device"
	"github.com/redis/go-redis/v9"
	"github.com/uptrace/uptrace-go/uptrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

var tracer = otel.Tracer("alert-service")

// incomingTelemetry matches the nested wire format from Simulator/Connector.
type incomingTelemetry struct {
	Metrics struct {
		Temperature float32 `json:"temperature_c"`
		Vibration   float32 `json:"vibration_hz"`
		Current     float32 `json:"current_ma"`
	} `json:"metrics"`
}

func main() {
	log.Println("Alert Service initializing...")
	// 0. Observability (Uptrace/OpenTelemetry)
	upDSN := os.Getenv("UPTRACE_DSN")
	if upDSN != "" {
		uptrace.ConfigureOpentelemetry(
			uptrace.WithDSN(upDSN),
			uptrace.WithServiceName("alert-service"),
			uptrace.WithServiceVersion("1.0.0"),
			uptrace.WithDeploymentEnvironment("production"),
		)
		defer uptrace.Shutdown(context.Background())
	}

	// 1. Config
	mqttBroker := os.Getenv("MQTT_BROKER_URL")
	if mqttBroker == "" {
		mqttBroker = "tls://localhost:8883"
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}
	deviceSvcAddr := os.Getenv("DEVICE_SERVICE_ADDR")
	if deviceSvcAddr == "" {
		deviceSvcAddr = "127.0.0.1:50051"
	}

	// 2. Clients
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb := redis.NewClient(&redis.Options{Addr: redisURL})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}

	conn, err := grpc.Dial(deviceSvcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		log.Fatalf("failed to connect to device svc: %v", err)
	}
	defer conn.Close()
	deviceSvc := devicepb.NewDeviceServiceClient(conn)

	// 3. MQTT Setup
	tlsConfig, err := loadTLSConfig("certs/ca.crt", "certs/client.crt", "certs/client.key")
	if err != nil {
		log.Fatalf("failed to load TLS config: %v", err)
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(mqttBroker)
	opts.SetClientID("alert-service-01")
	opts.SetTLSConfig(tlsConfig)
	
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("mqtt connect fail: %v", token.Error())
	}

	// 4. Processing logic
	handler := func(mqttClient mqtt.Client, msg mqtt.Message) {
		// Topic: devices/{id}/telemetry
		parts := strings.Split(msg.Topic(), "/")
		if len(parts) < 2 { return }
		id := parts[1]

		_, span := tracer.Start(ctx, "Evaluate Alert", trace.WithAttributes(
			attribute.String("device_id", id),
		), trace.WithSpanKind(trace.SpanKindConsumer))
		defer span.End()

		// a) Check Silence (Requirement: Manual Silencer)
		silenced, _ := rdb.Exists(ctx, "silence:"+id).Result()
		if silenced > 0 {
			log.Printf("[%s] device is currently silenced, ignoring telemetry for alerts", id)
			span.SetAttributes(attribute.Bool("silenced", true))
			return
		}

		// b) Fetch Thresholds
		t, err := deviceSvc.GetThreshold(ctx, &devicepb.GetThresholdRequest{DeviceId: id})
		if err != nil {
			log.Printf("[%s] could not fetch thresholds: %v", id, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, "threshold fetch failed")
			return
		}

		// c) Parse Payload (FIX: handle nested "metrics" structure)
		var rd incomingTelemetry
		if err := json.Unmarshal(msg.Payload(), &rd); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "unmarshal failed")
			return
		}

		span.SetAttributes(
			attribute.Float64("temp", float64(rd.Metrics.Temperature)),
			attribute.Float64("vibe", float64(rd.Metrics.Vibration)),
			attribute.Float64("current", float64(rd.Metrics.Current)),
		)

		// d) Evaluation & Alerting
		evaluate(ctx, id, "temperature", rd.Metrics.Temperature, t.TempMax, rdb, mqttClient)
		evaluate(ctx, id, "vibration", rd.Metrics.Vibration, t.VibeMax, rdb, mqttClient)
		evaluate(ctx, id, "current", rd.Metrics.Current, t.CurrentMax, rdb, mqttClient)

		span.SetStatus(codes.Ok, "evaluated")
	}

	client.Subscribe("devices/+/telemetry", 1, handler)
	log.Println("Alert Service started. Monitoring telemetry stream...")
	<-ctx.Done()
}

func evaluate(ctx context.Context, id, metric string, val, limit float32, rdb *redis.Client, mq mqtt.Client) {
	if val <= limit {
		// Reset alert state if it was triggered
		rdb.Del(ctx, fmt.Sprintf("alert_active:%s:%s", id, metric))
		return
	}

	// REQUIREMENT: Deduplication (Page 3)
	// Only alert if we haven't already alerted for this specific breach in the last 5 minutes
	isSet, _ := rdb.SetArgs(ctx, fmt.Sprintf("alert_active:%s:%s", id, metric), "1", redis.SetArgs{
		Mode: "NX",
		TTL:  5 * time.Minute,
	}).Result()
	if isSet != "OK" {
		return
	}

	// REQUIREMENT: Alert Publishing (Page 2)
	alert := map[string]interface{}{
		"device_id": id,
		"alert":     metric + "_high",
		"value":     val,
		"threshold": limit,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	payload, _ := json.Marshal(alert)
	mq.Publish("alerts/"+id, 1, false, payload)
	
	log.Printf("⚠️  ALERT [%s]: %s is %.2f (Threshold: %.2f)", id, metric, val, limit)
}

func loadTLSConfig(caFile, certFile, keyFile string) (*tls.Config, error) {
	caCert, _ := os.ReadFile(caFile)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	cert, _ := tls.LoadX509KeyPair(certFile, keyFile)
	return &tls.Config{
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}, nil
}
