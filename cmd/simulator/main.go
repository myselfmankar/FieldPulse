package main

import (
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
)

type DeviceConfig struct {
	ID        string
	BrokerURL string
	TLSConfig *tls.Config
	Remote    bool
}

type TelemetryPayload struct {
	DeviceID string  `json:"device_id"`
	Timestamp string `json:"timestamp"`
	Metrics  Metrics `json:"metrics"`
}

type Metrics struct {
	Temp    float64 `json:"temperature_c"`
	Vibe    float64 `json:"vibration_hz"`
	Current float64 `json:"current_ma"`
}

func main() {
	// 1. Flags (Requirement Page 8)
	brokerURL := flag.String("broker-url", "tls://localhost:8883", "MQTT Broker URL")
	certFile := flag.String("tls-cert", "certs/client.crt", "Path to client certificate")
	keyFile := flag.String("tls-key", "certs/client.key", "Path to client key")
	caFile := flag.String("tls-ca", "certs/ca.crt", "Path to CA certificate")
	modeRemote := flag.Bool("mode-remote", false, "Use remote payload schema")
	numDevices := flag.Int("n", 50, "Number of devices to simulate")
	flag.Parse()

	// 2. Load mTLS if not in remote mode (simplification: simulator uses one cert for all as per Page 8)
	var tlsConfig *tls.Config
	if !*modeRemote {
		var err error
		tlsConfig, err = loadTLSConfig(*caFile, *certFile, *keyFile)
		if err != nil {
			log.Fatalf("failed to load TLS: %v", err)
		}
	}

	// 3. Spawning Fleet (Requirement Page 3)
	var wg sync.WaitGroup
	for i := 0; i < *numDevices; i++ {
		deviceID := fmt.Sprintf("sim-device-%03d", i)
		wg.Add(1)
		go simulateDevice(DeviceConfig{
			ID:        deviceID,
			BrokerURL: *brokerURL,
			TLSConfig: tlsConfig,
			Remote:    *modeRemote,
		}, &wg)
	}

	log.Printf("Fleet of %d devices online. Simulating industrial load...", *numDevices)
	wg.Wait()
}

func simulateDevice(cfg DeviceConfig, wg *sync.WaitGroup) {
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

	// Requirement: Silent device every 5 minutes (Page 3)
	// We'll simulate this by having a "silent" chance
	isSilent := false
	silentChan := time.NewTicker(5 * time.Minute)
	defer silentChan.Stop()

	for {
		select {
		case <-silentChan.C:
			// 1 in 50 chance to go silent for 5 minutes
			if rand.Intn(50) == 0 {
				log.Printf("[%s] DISCONNECTED (Simulating failure)", cfg.ID)
				isSilent = true
				time.Sleep(5 * time.Minute)
				isSilent = false
				log.Printf("[%s] RECONNECTED", cfg.ID)
			}
		default:
			if isSilent {
				time.Sleep(10 * time.Second)
				continue
			}

			// Randomized interval 2-10s (Requirement Page 3)
			wait := time.Duration(rand.Intn(8)+2) * time.Second
			time.Sleep(wait)

			publishTelemetry(client, cfg)
		}
	}
}

func publishTelemetry(client mqtt.Client, cfg DeviceConfig) {
	// Requirement: 5% Anomalies (Page 3)
	isAnomaly := rand.Float32() < 0.05
	temp := 22.0 + rand.Float64()*5.0
	vibe := 15.0 + rand.Float64()*10.0
	curr := 500.0 + rand.Float64()*100.0

	if isAnomaly {
		temp += 50.0 // Push above threshold
		log.Printf("[%s] ⚠️  Injecting Anomaly", cfg.ID)
	}

	payload := TelemetryPayload{
		DeviceID:  cfg.ID,
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Metrics: Metrics{
			Temp:    temp,
			Vibe:    vibe,
			Current: curr,
		},
	}

	// Support for remote mode (foreign schema)
	var data []byte
	topic := "devices/" + cfg.ID + "/telemetry"
	
	if cfg.Remote {
		// Mock foreign schema for Client-A
		remoteData := map[string]interface{}{
			"ts": time.Now().Unix(),
			"sensors": map[string]float64{
				"temp": temp,
				"vib":  vibe,
				"cur":  curr,
			},
		}
		data, _ = json.Marshal(remoteData)
		topic = "factory/line-1/" + cfg.ID + "/data"
	} else {
		data, _ = json.Marshal(payload)
	}

	client.Publish(topic, 1, false, data)

	// Requirement: 3% Duplicates (Page 3)
	if rand.Float32() < 0.03 {
		log.Printf("[%s] 🔁 Injecting Duplicate", cfg.ID)
		client.Publish(topic, 1, false, data)
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
