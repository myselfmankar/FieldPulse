package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type RegisterRequest struct {
	DeviceID    string `json:"device_id"`
	DeviceType  string `json:"device_type"`
	Floor       string `json:"floor"`
	FirmwareVer string `json:"firmware_ver"`
}

type RegisterResponse struct {
	DeviceID      string `json:"device_id"`
	BrokerURL     string `json:"broker_url"`
	ClientCertPem []byte `json:"client_cert_pem"`
	ClientKeyPem  []byte `json:"client_key_pem"`
	CaPem         []byte `json:"ca_pem"`
}

func main() {
	devices := []RegisterRequest{
		{DeviceID: "sim-device-001", DeviceType: "vibration_sensor", Floor: "Floor-1", FirmwareVer: "1.0.1"},
		{DeviceID: "sim-device-002", DeviceType: "temp_sensor", Floor: "Floor-2", FirmwareVer: "1.0.1"},
		{DeviceID: "sim-device-003", DeviceType: "current_sensor", Floor: "Floor-3", FirmwareVer: "1.0.1"},
	}

	gatewayURL := os.Getenv("GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:3000/v1/devices"
	}

	certsDir := os.Getenv("CERTS_DIR")
	if certsDir == "" {
		certsDir = "certs"
	}

	apiKey := os.Getenv("GATEWAY_API_KEY")
	if apiKey == "" {
		apiKey = "fieldpulse-secret-2026"
	}

	for _, dev := range devices {
		fmt.Printf("Registering %s...\n", dev.DeviceID)

		var resp *http.Response
		var err error
		success := false

		// RETRY LOOP: Handle temporary DNS or service unavailability
		for attempt := 1; attempt <= 5; attempt++ {
			payload, _ := json.Marshal(dev)
			req, _ := http.NewRequest("POST", gatewayURL, bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", apiKey)

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err = client.Do(req)

			if err == nil && (resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict) {
				success = true
				break
			}

			if resp != nil {
				resp.Body.Close()
			}
			log.Printf("  [Attempt %d/5] Backend not ready (%v). Retrying in 5s...", attempt, err)
			time.Sleep(5 * time.Second)
		}

		if !success {
			log.Printf("❌ Failed to register %s after 5 attempts. Skipping.", dev.DeviceID)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusConflict {
			fmt.Printf("Device %s already exists. Using existing record.\n", dev.DeviceID)
		}

		var regResp RegisterResponse
		if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
			log.Printf("Failed to decode response for %s: %v", dev.DeviceID, err)
			continue
		}

		// Save certificates
		basePath := filepath.Join(certsDir, "devices", dev.DeviceID)
		if err := os.MkdirAll(basePath, 0755); err != nil {
			log.Printf("Failed to create dir for %s: %v", dev.DeviceID, err)
			continue
		}

		saveFile(filepath.Join(basePath, "client.crt"), regResp.ClientCertPem)
		saveFile(filepath.Join(basePath, "client.key"), regResp.ClientKeyPem)
		saveFile(filepath.Join(basePath, "ca.crt"), regResp.CaPem)

		fmt.Printf("✅ %s registered successfully. Certs saved to %s\n", dev.DeviceID, basePath)
	}
}

func saveFile(path string, data []byte) {
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("Error saving file %s: %v", path, err)
	}
}
