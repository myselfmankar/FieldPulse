package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func main() {
	fmt.Println("Starting Phase 2 Self-Verification...")

	payload := map[string]string{
		"device_id":    "final-sensor-test",
		"floor":        "3",
		"device_type":  "vibration",
		"firmware_ver": "1.0.1",
	}

	jsonBody, _ := json.Marshal(payload)

	fmt.Printf("Sending POST to Gateway (http://localhost:3000/v1/devices) with body: %s\n", string(jsonBody))

	resp, err := http.Post("http://localhost:3000/v1/devices", "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		fmt.Printf("Error: Failed to reach gateway. Is it running? %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response Code: %d\n", resp.StatusCode)
	fmt.Printf("Response Body: %s\n", string(body))

	if resp.StatusCode == 201 {
		fmt.Println("\nSUCCESS! Phase 2 is verified.")
	} else {
		fmt.Println("\nFAILED. Check gateway logs.")
	}
}
