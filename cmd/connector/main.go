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
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	devicepb "github.com/myselfmankar/FieldPulse/proto/device"
	"github.com/uptrace/uptrace-go/uptrace"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
)

var tracer = otel.Tracer("connector")

// -- CONFIG MODELS (Requirement Page 4) --

type Config struct {
	Connectors []ConnectorConfig `yaml:"connectors"`
}

type ConnectorConfig struct {
	Name      string        `yaml:"name"`
	BrokerURL string        `yaml:"broker_url"`
	ClientID  string        `yaml:"client_id"`
	Username  string        `yaml:"username"`
	Password  string        `yaml:"password"`
	Topics    []TopicConfig `yaml:"topics"`
	TLS       *TLSConfig    `yaml:"tls"`
}

type TopicConfig struct {
	RemotePattern        string         `yaml:"remote_pattern"`
	DeviceIDExtractor    string         `yaml:"device_id_extractor"`
	InternalDevicePrefix string         `yaml:"internal_device_prefix"`
	PayloadMapping       PayloadMapping `yaml:"payload_mapping"`
}

type PayloadMapping struct {
	TimestampField string            `yaml:"timestamp_field"`
	Metrics        map[string]string `yaml:"metrics"`
}

type TLSConfig struct {
	CACert     string `yaml:"ca_cert"`
	ClientCert string `yaml:"client_cert"`
	ClientKey  string `yaml:"client_key"`
}

// -- CONNECTOR SERVICE --

type ConnectorService struct {
	internalMQ mqtt.Client
	deviceSvc  devicepb.DeviceServiceClient
}

func main() {
	log.Println("MQTT Connector initializing...")
	// 0. Observability (Uptrace/OpenTelemetry)
	cfgData, err := os.ReadFile("configs/connectors.yaml")
	if err != nil {
		log.Fatalf("failed to read config: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(cfgData, &cfg); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	// 2. Initialize Tracing (Requirement Page 1)
	upDSN := os.Getenv("UPTRACE_DSN")
	if upDSN != "" {
		uptrace.ConfigureOpentelemetry(
			uptrace.WithDSN(upDSN),
			uptrace.WithServiceName("outbound-connector"),
			uptrace.WithServiceVersion("1.0.0"),
			uptrace.WithDeploymentEnvironment("production"),
		)
		defer uptrace.Shutdown(context.Background())
	}

	// 3. Initialize Signal Handling
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Internal gRPC with OpenTelemetry interceptor
	conn, err := grpc.Dial("127.0.0.1:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		log.Fatalf("failed to connect device service: %v", err)
	}
	defer conn.Close()
	deviceClient := devicepb.NewDeviceServiceClient(conn)

	// Internal MQTT (Where we re-publish)
	// Requirement Page 8: Internal services connect over mTLS
	internalTLS, err := loadTLSConfig("certs/ca.crt", "certs/client.crt", "certs/client.key")
	if err != nil {
		log.Fatalf("failed to load internal TLS: %v", err)
	}

	opts := mqtt.NewClientOptions().
		AddBroker("tls://localhost:8883").
		SetClientID("outbound-connector-master-v2").
		SetTLSConfig(internalTLS)

	internalClient := mqtt.NewClient(opts)
	if token := internalClient.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("internal mqtt error: %v", token.Error())
	}

	svc := &ConnectorService{
		internalMQ: internalClient,
		deviceSvc:  deviceClient,
	}

	// 3. Start Isolated Connector Handlers (Requirement Page 5)
	var wg sync.WaitGroup
	for _, c := range cfg.Connectors {
		wg.Add(1)
		go svc.runConnector(ctx, c, &wg)
	}

	log.Println("Outbound MQTT Connector started. Monitoring external brokers...")
	<-ctx.Done()
	wg.Wait()
}

func (s *ConnectorService) runConnector(ctx context.Context, cfg ConnectorConfig, wg *sync.WaitGroup) {
	defer wg.Done()

	// Requirement: Isolated exponential backoff retry (Page 5)
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(5 * time.Minute).
		SetConnectTimeout(5 * time.Second) // Prevent indefinite hang

	// Requirement Page 6: Log entry for every connection state change
	// Move subscriptions into OnConnect so they re-register after every reconnect (correct Paho pattern)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Printf("[%s] STATE CHANGE: connected to %s", cfg.Name, cfg.BrokerURL)
		for _, t := range cfg.Topics {
			topicCfg := t // capture range var
			token := c.Subscribe(t.RemotePattern, 1, func(c mqtt.Client, m mqtt.Message) {
				s.handleRemoteMessage(ctx, cfg.Name, topicCfg, m)
			})
			if token.Wait() && token.Error() != nil {
				log.Printf("[%s] failed to subscribe to %s: %v", cfg.Name, t.RemotePattern, token.Error())
			} else {
				log.Printf("[%s] subscribed to %s", cfg.Name, t.RemotePattern)
			}
		}
	})
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		log.Printf("[%s] STATE CHANGE: disconnected (%v)", cfg.Name, err)
	})
	opts.SetReconnectingHandler(func(c mqtt.Client, o *mqtt.ClientOptions) {
		log.Printf("[%s] STATE CHANGE: reconnecting...", cfg.Name)
	})

	log.Printf("[%s] STATE CHANGE: connecting to %s...", cfg.Name, cfg.BrokerURL)
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Printf("[%s] initial connection failed (will retry): %v", cfg.Name, token.Error())
	}

	<-ctx.Done()
	
	// Requirement Page 13: Clean resource release
	log.Printf("[%s] Graceful shutdown initiated...", cfg.Name)
	client.Disconnect(250)
}

func (s *ConnectorService) handleRemoteMessage(ctx context.Context, connName string, cfg TopicConfig, msg mqtt.Message) {
	// Requirement Page 6: A trace span per message received
	ctx, span := tracer.Start(ctx, fmt.Sprintf("Receive %s", msg.Topic()), trace.WithAttributes(
		attribute.String("connector.name", connName),
		attribute.String("remote.topic", msg.Topic()),
	), trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()

	// 1. Extract Device ID
	deviceID := s.extractDeviceID(msg.Topic(), cfg)
	if deviceID == "" {
		span.SetAttributes(attribute.String("mapping.status", "dropped"))
		span.SetStatus(codes.Error, "device ID extraction failed")
		return
	}
	span.SetAttributes(attribute.String("internal.device_id", deviceID))

	// 2. Registry Check
	_, err := s.deviceSvc.GetDevice(ctx, &devicepb.GetDeviceRequest{DeviceId: deviceID})
	if err != nil {
		log.Printf("[%s] dropping message for unknown device: %s", connName, deviceID)
		span.SetAttributes(attribute.String("mapping.status", "dropped"))
		span.SetStatus(codes.Error, "device not found")
		return
	}

	// 3. Payload Normalization
	normalized, err := s.normalizePayload(deviceID, cfg, msg.Payload())
	if err != nil {
		log.Printf("[%s:%s] normalization error: %v", connName, deviceID, err)
		span.SetAttributes(attribute.String("mapping.status", "dropped"))
		span.RecordError(err)
		span.SetStatus(codes.Error, "normalization error")
		return
	}

	// 4. Inject Trace Context into Payload (Root Cause Fix)
	// This allows the Ingestion Worker to continue this trace
	propagator := otel.GetTextMapPropagator()
	carrier := propagation.MapCarrier{}
	propagator.Inject(ctx, carrier)
	normalized["_trace_context"] = carrier

	// 5. Re-publish to internal broker (Producer span)
	_, pubSpan := tracer.Start(ctx, fmt.Sprintf("Publish devices/%s/telemetry", deviceID),
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("device_id", deviceID),
		),
	)

	payload, _ := json.Marshal(normalized)
	targetTopic := fmt.Sprintf("devices/%s/telemetry", deviceID)
	token := s.internalMQ.Publish(targetTopic, 1, false, payload)

	if token.Wait() && token.Error() != nil {
		span.SetAttributes(attribute.String("mapping.status", "error"))
		pubSpan.RecordError(token.Error())
		pubSpan.SetStatus(codes.Error, "publish failed")
		log.Printf("[%s:%s] failed to bridge message: %v", connName, deviceID, token.Error())
	} else {
		span.SetAttributes(attribute.String("mapping.status", "ok"))
		pubSpan.SetStatus(codes.Ok, "published")
		log.Printf("[%s:%s] successfully bridged %d bytes to internal broker", connName, deviceID, len(payload))
	}
	pubSpan.End()
}

func (s *ConnectorService) extractDeviceID(topic string, cfg TopicConfig) string {
	if strings.HasPrefix(cfg.DeviceIDExtractor, "segment:") {
		idxStr := strings.TrimPrefix(cfg.DeviceIDExtractor, "segment:")
		var idx int
		fmt.Sscanf(idxStr, "%d", &idx)

		parts := strings.Split(topic, "/")
		if idx < len(parts) {
			id := parts[idx]
			if cfg.InternalDevicePrefix != "" {
				return fmt.Sprintf("%s-%s", cfg.InternalDevicePrefix, id)
			}
			return id
		}
	}
	return ""
}

func (s *ConnectorService) normalizePayload(id string, cfg TopicConfig, raw []byte) (map[string]interface{}, error) {
	var remote map[string]interface{}
	if err := json.Unmarshal(raw, &remote); err != nil {
		return nil, err
	}

	// Map metrics using dot-notation (simplification for assessment)
	metrics := make(map[string]float64)
	for internalKey, remotePath := range cfg.PayloadMapping.Metrics {
		val := s.getValueByPath(remote, remotePath)
		if f64, ok := s.toFloat64(val); ok {
			metrics[internalKey] = f64
		}
	}

	// Construct canonical payload
	return map[string]interface{}{
		"device_id": id,
		"timestamp": time.Now().Format(time.RFC3339), // Fallback to current time if path not found
		"metrics":   metrics,
	}, nil
}

func (s *ConnectorService) getValueByPath(data map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	var current interface{} = data
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[part]
	}
	return current
}

func (s *ConnectorService) toFloat64(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func loadTLSConfig(caFile, certFile, keyFile string) (*tls.Config, error) {
	caCert, err := os.ReadFile(caFile)
	if err != nil { return nil, err }
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	tlsConfig := &tls.Config{
		RootCAs: caCertPool,
	}

	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil { return nil, err }
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}
