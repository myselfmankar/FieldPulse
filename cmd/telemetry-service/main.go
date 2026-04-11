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
	"github.com/myselfmankar/FieldPulse/internal/telemetry"
	pb "github.com/myselfmankar/FieldPulse/proto/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	// 1. Load Configuration
	// Note: Telemetry Service connects to TimescaleDB (fieldpulse-timescaledb:5433)
	dbURL := os.Getenv("TELEMETRY_DB_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:password@127.0.0.1:5433/telemetry_db?sslmode=disable"
	}

	// 2. Initialize Database Pool
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to telemetry database: %v", err)
	}
	defer dbPool.Close()

	// 3. Setup gRPC Server
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	
	// Initialize Repository and Service
	repo := telemetry.NewRepository(dbPool)
	telemetrySvc := telemetry.NewService(repo)

	pb.RegisterTelemetryServiceServer(s, telemetrySvc)
	reflection.Register(s)

	// 4. Graceful Shutdown
	go func() {
		log.Println("Telemetry Service listening on :50052")
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down Telemetry Service...")
	
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
