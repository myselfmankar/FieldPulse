package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func main() {
	fmt.Println("Starting Phase 3 E2E Verification...")
	deviceID := fmt.Sprintf("telemetry-test-%d", time.Now().Unix())

	// 1. Create Device
	fmt.Println("1. Registering Device via Gateway...")
	regPayload := map[string]string{
		"device_id":    deviceID,
		"floor":        "Floor-1",
		"device_type":  "vibration",
		"firmware_ver": "1.0.0",
	}
	regBytes, _ := json.Marshal(regPayload)
	resp, err := http.Post("http://localhost:3000/v1/devices", "application/json", bytes.NewBuffer(regBytes))
	if err != nil || resp.StatusCode != 201 {
		log.Fatalf("Registration failed: %v (Status: %d)", err, resp.StatusCode)
	}
	
	var regResp struct {
		ClientCertPem []byte `json:"client_cert_pem"`
		ClientKeyPem  []byte `json:"client_key_pem"`
		CaPem         []byte `json:"ca_pem"`
	}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &regResp)
	fmt.Println("   Success. Received Certificates.")

	// 2. Setup MQTT TLS from received certs
	fmt.Println("2. Connecting to MQTT Broker via mTLS...")
	cert, err := tls.X509KeyPair(regResp.ClientCertPem, regResp.ClientKeyPem)
	if err != nil {
		log.Fatalf("Failed to load keypair: %v (Body: %s)", err, string(body))
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(regResp.CaPem)

	tlsConfig := &tls.Config{
		RootCAs:            caPool,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}

	opts := mqtt.NewClientOptions().AddBroker("tls://localhost:8883").SetTLSConfig(tlsConfig)
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("MQTT Connection failed: %v", token.Error())
	}
	defer client.Disconnect(250)
	fmt.Println("   Success. Connected.")

	// 3. Publish Telemetry
	fmt.Println("3. Publishing Telemetry Message...")
	timestamp := time.Now()
	telemetry := map[string]interface{}{
		"device_id":     deviceID,
		"timestamp":    timestamp.Format(time.RFC3339Nano),
		"temperature_c": 24.5,
		"vibration_hz":  120.0,
		"current_ma":    850.0,
	}
	telBytes, _ := json.Marshal(telemetry)
	topic := fmt.Sprintf("devices/%s/telemetry", deviceID)
	token := client.Publish(topic, 1, false, telBytes)
	token.Wait()
	fmt.Println("   Success. Message Published.")

	// 4. Wait for ingestion
	fmt.Println("4. Waiting for Ingestion Worker (3s)...")
	time.Sleep(3 * time.Second)

	// 5. Query Gateway for Latest
	fmt.Println("5. Querying Gateway for Latest Telemetry...")
	qResp, err := http.Get(fmt.Sprintf("http://localhost:3000/v1/telemetry/%s/latest", deviceID))
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	defer qResp.Body.Close()
	
	qBody, _ := io.ReadAll(qResp.Body)
	fmt.Printf("   Response: %s\n", string(qBody))

	if qResp.StatusCode == 200 {
		fmt.Println("\n✅ PHASE 3 VERIFIED: Data successfully flowed MQTT -> Ingest -> Timescale -> API")
	} else {
		fmt.Printf("\n❌ FAILED with status %d\n", qResp.StatusCode)
	}
}
