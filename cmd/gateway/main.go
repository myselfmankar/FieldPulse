package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	pb "github.com/myselfmankar/FieldPulse/proto/device"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type Gateway struct {
	deviceClient pb.DeviceServiceClient
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

	conn, err := grpc.Dial(deviceSvcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	gw := &Gateway{
		deviceClient: pb.NewDeviceServiceClient(conn),
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

	// 4. Start Server
	log.Fatal(app.Listen(":3000"))
}

func (gw *Gateway) HandleCreateDevice(c *fiber.Ctx) error {
	var req pb.CreateDeviceRequest
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

	resp, err := gw.deviceClient.GetDevice(ctx, &pb.GetDeviceRequest{DeviceId: id})
	if err != nil {
		return mapgRPCErrorToFiber(err)
	}

	return c.JSON(resp)
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
