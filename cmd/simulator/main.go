package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/uptrace/uptrace-go/uptrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("simulator")

type DeviceConfig struct {
	ID        string
	BrokerURL string
	TLSConfig *tls.Config
	Remote    bool
}

func main() {
	log.Println("Simulator initializing...")
	upDSN := os.Getenv("UPTRACE_DSN")
	if upDSN != "" {
		uptrace.ConfigureOpentelemetry(
			uptrace.WithDSN(upDSN),
			uptrace.WithServiceName("device-simulator"),
		)
		defer uptrace.Shutdown(context.Background())
	}

	// 1. Config & Environment Prioritization
	mqttBroker := os.Getenv("MQTT_BROKER_URL")
	if mqttBroker == "" {
		mqttBroker = "tls://localhost:8883"
	}
	
	// Flags allow overrides, but defaults now come from ENV
	brokerUrlFlag := flag.String("broker-url", mqttBroker, "MQTT Broker URL")
	certFile := flag.String("tls-cert", "certs/client.crt", "Path to client certificate")
	keyFile := flag.String("tls-key", "certs/client.key", "Path to client key")
	caFile := flag.String("tls-ca", "certs/ca.crt", "Path to CA certificate")
	modeRemote := flag.Bool("mode-remote", false, "Use remote payload schema (un-nested)")
	numDevices := flag.Int("n", 3, "Number of devices to simulate")
	flag.Parse()

	// Use the final flag value (which defaults to env)
	finalBrokerURL := *brokerUrlFlag

	var tlsConfig *tls.Config
	if !*modeRemote {
		var err error
		tlsConfig, err = loadTLSConfig(*caFile, *certFile, *keyFile)
		if err != nil {
			log.Fatalf("failed to load TLS: %v", err)
		}
	}

	var wg sync.WaitGroup
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	for i := 1; i <= *numDevices; i++ {
		deviceID := fmt.Sprintf("sim-device-%03d", i)
		wg.Add(1)
		go simulateDevice(ctx, DeviceConfig{
			ID:        deviceID,
			BrokerURL: *brokerURL,
			TLSConfig: tlsConfig,
			Remote:    *modeRemote,
		}, &wg)
	}

	log.Printf("Fleet of %d devices online. Simulating industrial load...", *numDevices)
	wg.Wait()
}

func simulateDevice(ctx context.Context, cfg DeviceConfig, wg *sync.WaitGroup) {
	defer wg.Done()

	opts := mqtt.NewClientOptions().AddBroker(cfg.BrokerURL).SetClientID(cfg.ID)
	if cfg.TLSConfig != nil {
		opts.SetTLSConfig(cfg.TLSConfig)
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Printf("[%s] Connect error: %v", cfg.ID, token.Error())
		return
	}
	defer client.Disconnect(250)

	silentChan := time.NewTicker(5 * time.Minute)
	defer silentChan.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-silentChan.C:
			if rand.Intn(10) == 0 {
				log.Printf("[%s] DISCONNECTED (Simulating failure)", cfg.ID)
				time.Sleep(5 * time.Minute)
				log.Printf("[%s] RECONNECTED", cfg.ID)
			}
		default:
			// Requirement: Randomised intervals between 2-10 seconds
			wait := time.Duration(rand.Intn(8)+2) * time.Second
			time.Sleep(wait)
			publishTelemetry(client, cfg)
		}
	}
}

func publishTelemetry(client mqtt.Client, cfg DeviceConfig) {
	ctx := context.Background()
	_, span := tracer.Start(ctx, "Publish Telemetry",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(attribute.String("device_id", cfg.ID)),
	)
	defer span.End()

	isAnomaly := rand.Float32() < 0.05
	temp := 22.0 + rand.Float64()*5.0
	if isAnomaly {
		temp += 50.0
		span.SetAttributes(attribute.Bool("anomaly", true))
	}

	payload := map[string]interface{}{
		"device_id": cfg.ID,
		"timestamp": time.Now().Format(time.RFC3339),
		"metrics": map[string]float64{
			"temperature_c": temp,
			"vibration_hz":  float64(rand.Intn(50)),
			"current_ma":    float64(rand.Intn(1000)),
		},
	}

	var data []byte
	topic := "devices/" + cfg.ID + "/telemetry"
	
	if cfg.Remote {
		remoteData := map[string]interface{}{
			"ts": time.Now().Unix(),
			"sensors": map[string]float64{"temp": temp},
		}
		data, _ = json.Marshal(remoteData)
		topic = "factory/line-1/" + cfg.ID + "/data"
	} else {
		data, _ = json.Marshal(payload)
	}

	token := client.Publish(topic, 1, false, data)
	if token.Wait() && token.Error() != nil {
		span.SetStatus(codes.Error, token.Error().Error())
	} else {
		span.SetStatus(codes.Ok, "published")
	}

	if rand.Float32() < 0.03 {
		client.Publish(topic, 1, false, data) // Duplicate
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
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}, nil
}
