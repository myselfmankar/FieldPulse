package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	devicepb "github.com/myselfmankar/FieldPulse/proto/device"
	telemetrypb "github.com/myselfmankar/FieldPulse/proto/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	"github.com/redis/go-redis/v9"
)

type Gateway struct {
	deviceClient    devicepb.DeviceServiceClient
	telemetryClient telemetrypb.TelemetryServiceClient
	rdb             *redis.Client
}

func main() {
	// 1. Connect to Backend Services (gRPC)
	brokerURL := os.Getenv("INTERNAL_BROKER_URL")
	if brokerURL == "" {
		brokerURL = "mqtts://127.0.0.1:8883"
	}

	deviceSvcAddr := os.Getenv("DEVICE_SERVICE_ADDR")
	if deviceSvcAddr == "" {
		deviceSvcAddr = "127.0.0.1:50051"
	}

	telemetrySvcAddr := os.Getenv("TELEMETRY_SERVICE_ADDR")
	if telemetrySvcAddr == "" {
		telemetrySvcAddr = "127.0.0.1:50052"
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "127.0.0.1:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisURL})

	dConn, err := grpc.Dial(deviceSvcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect device svc: %v", err)
	}
	defer dConn.Close()

	tConn, err := grpc.Dial(telemetrySvcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect telemetry svc: %v", err)
	}
	defer tConn.Close()

	gw := &Gateway{
		deviceClient:    devicepb.NewDeviceServiceClient(dConn),
		telemetryClient: telemetrypb.NewTelemetryServiceClient(tConn),
		rdb:             rdb,
	}

	// 2. Initialize GoFiber
	app := fiber.New(fiber.Config{
		ErrorHandler: customErrorHandler,
	})

	app.Use(logger.New())

	// 3. Routes
	v1 := app.Group("/v1")
	
	v1.Post("/devices", gw.HandleCreateDevice)
	v1.Get("/devices/:id", gw.HandleGetDevice)

	v1.Get("/telemetry/:id", gw.HandleQueryTelemetry)
	v1.Get("/telemetry/:id/latest", gw.HandleGetLatestTelemetry)

	v1.Post("/devices/:id/thresholds", gw.HandleSetThresholds)
	v1.Get("/devices/:id/thresholds", gw.HandleGetThresholds)
	v1.Post("/devices/:id/silence", gw.HandleSetSilence)

	// 4. Start Server
	log.Fatal(app.Listen(":3000"))
}

func (gw *Gateway) HandleCreateDevice(c *fiber.Ctx) error {
	var req devicepb.CreateDeviceRequest
	if err := protojson.Unmarshal(c.Body(), &req); err != nil {
		log.Printf("Protobuf JSON unmarshal error: %v | Raw Body: %s", err, string(c.Body()))
		return fiber.NewError(http.StatusBadRequest, "Invalid JSON body: "+err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := gw.deviceClient.CreateDevice(ctx, &req)
	if err != nil {
		return mapgRPCErrorToFiber(err)
	}

	return c.Status(http.StatusCreated).JSON(resp)
}

func (gw *Gateway) HandleGetDevice(c *fiber.Ctx) error {
	id := c.Params("id")
	
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := gw.deviceClient.GetDevice(ctx, &devicepb.GetDeviceRequest{DeviceId: id})
	if err != nil {
		return mapgRPCErrorToFiber(err)
	}

	return c.JSON(resp)
}

func (gw *Gateway) HandleQueryTelemetry(c *fiber.Ctx) error {
	id := c.Params("id")
	res := c.Query("resolution", "1m")
	
	// Default: Last 24 hours
	endTime := time.Now()
	startTime := endTime.Add(-24 * time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := gw.telemetryClient.QueryTelemetry(ctx, &telemetrypb.QueryTelemetryRequest{
		DeviceId:   id,
		Resolution: res,
		StartTime:  timestamppb.New(startTime),
		EndTime:    timestamppb.New(endTime),
	})
	if err != nil {
		return mapgRPCErrorToFiber(err)
	}

	return c.JSON(resp)
}

func (gw *Gateway) HandleGetLatestTelemetry(c *fiber.Ctx) error {
	id := c.Params("id")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := gw.telemetryClient.GetLatestReading(ctx, &telemetrypb.GetLatestReadingRequest{
		DeviceId: id,
	})
	if err != nil {
		return mapgRPCErrorToFiber(err)
	}

	return c.JSON(resp)
}

func (gw *Gateway) HandleSetThresholds(c *fiber.Ctx) error {
	id := c.Params("id")
	var req devicepb.SetThresholdRequest
	if err := protojson.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(http.StatusBadRequest, "Invalid JSON body")
	}
	req.DeviceId = id

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := gw.deviceClient.SetThreshold(ctx, &req)
	if err != nil {
		return mapgRPCErrorToFiber(err)
	}

	return c.JSON(resp)
}

func (gw *Gateway) HandleGetThresholds(c *fiber.Ctx) error {
	id := c.Params("id")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := gw.deviceClient.GetThreshold(ctx, &devicepb.GetThresholdRequest{DeviceId: id})
	if err != nil {
		return mapgRPCErrorToFiber(err)
	}

	return c.JSON(resp)
}

func (gw *Gateway) HandleSetSilence(c *fiber.Ctx) error {
	id := c.Params("id")
	durationStr := c.Query("duration", "10m")
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return fiber.NewError(http.StatusBadRequest, "Invalid duration format (e.g. 10m, 1h)")
	}

	// REQUIREMENT: Manual Silence via Redis (Page 2)
	err = gw.rdb.Set(context.Background(), "silence:"+id, "1", duration).Err()
	if err != nil {
		return fiber.NewError(http.StatusInternalServerError, "Failed to set silence in cache")
	}

	return c.JSON(fiber.Map{
		"message":  "Device alerts silenced",
		"id":       id,
		"duration": duration.String(),
	})
}

// Technical Requirement: Error Code Mapping (gRPC -> HTTP)
func mapgRPCErrorToFiber(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return fiber.NewError(http.StatusInternalServerError, err.Error())
	}

	switch st.Code() {
	case codes.NotFound:
		return fiber.NewError(http.StatusNotFound, st.Message())
	case codes.AlreadyExists:
		return fiber.NewError(http.StatusConflict, st.Message())
	case codes.InvalidArgument:
		return fiber.NewError(http.StatusBadRequest, st.Message())
	default:
		return fiber.NewError(http.StatusInternalServerError, st.Message())
	}
}

func customErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	return c.Status(code).JSON(fiber.Map{
		"error": err.Error(),
	})
}
