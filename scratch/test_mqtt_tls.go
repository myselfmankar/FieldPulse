package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func main() {
	// 1. Load CA cert
	caCert, err := os.ReadFile("certs/ca.crt")
	if err != nil {
		log.Fatal(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// 2. Load Client cert/key
	cert, err := tls.LoadX509KeyPair("certs/client.crt", "certs/client.key")
	if err != nil {
		log.Fatal(err)
	}

	// 3. Setup TLS Config
	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: false, // MANDATORY requirement to keep this false
		ServerName:         "localhost",
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker("ssl://localhost:8883")
	opts.SetClientID("infra-test-client")
	opts.SetTLSConfig(tlsConfig)
	opts.SetConnectTimeout(5 * time.Second)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Printf("Connection error: %v\n", token.Error())
		os.Exit(1)
	}

	fmt.Println("SUCCESS: Connected to Mosquitto via mTLS on port 8883!")
	client.Disconnect(250)
}
