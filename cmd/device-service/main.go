package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/myselfmankar/FieldPulse/internal/device"
	pb "github.com/myselfmankar/FieldPulse/proto/device"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	// 1. Load Configuration
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/device_registry"
	}

	brokerURL := os.Getenv("INTERNAL_BROKER_URL")
	if brokerURL == "" {
		brokerURL = "mqtts://localhost:8883"
	}

	caCertPath := "certs/ca.crt"
	caKeyPath := "certs/ca.key"

	// 2. Initialize Database Pool (pgx/v5)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer dbPool.Close()

	// 3. Setup gRPC Server
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	
	// Initialize Repository and Service
	repo := device.NewRepository(dbPool)
	deviceSvc, err := device.NewService(repo, caCertPath, caKeyPath, brokerURL)
	if err != nil {
		log.Fatalf("failed to initialize device service: %v", err)
	}

	pb.RegisterDeviceServiceServer(s, deviceSvc)
	reflection.Register(s) // For testing with grpcurl

	// 4. Graceful Shutdown (Concurrency Discipline)
	go func() {
		log.Println("Device Service listening on :50051")
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down Device Service...")
	
	// GracefulStop waits for all active RPCs to finish
	stopped := make(chan struct{})
	go func() {
		s.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		log.Println("Shutdown complete")
	case <-time.After(5 * time.Second):
		log.Println("Shutdown timed out, forcing stop")
		s.Stop()
	}
}
