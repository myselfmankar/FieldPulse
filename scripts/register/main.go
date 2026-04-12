package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

	gatewayURL := "http://localhost:3000/v1/devices"

	for _, dev := range devices {
		fmt.Printf("Registering %s...\n", dev.DeviceID)
		
		payload, _ := json.Marshal(dev)
		resp, err := http.Post(gatewayURL, "application/json", bytes.NewBuffer(payload))
		if err != nil {
			log.Printf("Failed to register %s: %v", dev.DeviceID, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("Failed to register %s. Status: %d, Body: %s", dev.DeviceID, resp.StatusCode, string(body))
			continue
		}

		if resp.StatusCode == http.StatusConflict {
			fmt.Printf("Device %s already exists. Skipping certificate save.\n", dev.DeviceID)
			continue
		}

		var regResp RegisterResponse
		if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
			log.Printf("Failed to decode response for %s: %v", dev.DeviceID, err)
			continue
		}

		// Save certificates
		basePath := filepath.Join("certs", "devices", dev.DeviceID)
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
