package device

import (
	"context"
	"fmt"
	"os"

	pb "github.com/myselfmankar/FieldPulse/proto/device"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct {
	pb.UnimplementedDeviceServiceServer
	repo      *Repository
	caCert    []byte
	caKey     []byte
	brokerURL string
}

func NewService(repo *Repository, caCertPath, caKeyPath, brokerURL string) (*Service, error) {
	cert, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}

	key, err := os.ReadFile(caKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA key: %w", err)
	}

	return &Service{
		repo:      repo,
		caCert:    cert,
		caKey:     key,
		brokerURL: brokerURL,
	}, nil
}

func (s *Service) CreateDevice(ctx context.Context, req *pb.CreateDeviceRequest) (*pb.CreateDeviceResponse, error) {
	// 1. Check if device already exists (Validation requirement #1)
	existing, _ := s.repo.GetDevice(ctx, req.DeviceId)
	if existing != nil {
		return nil, status.Errorf(codes.AlreadyExists, "device %s already exists", req.DeviceId)
	}

	// 2. Generate mTLS Bundle (Security requirement #2, #3)
	bundle, err := GenerateDeviceBundle(req.DeviceId, s.caCert, s.caKey)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to provision certificates: %v", err)
	}

	// 3. Persist to Postgres (Persistence requirement #4)
	dev := &Device{
		ID:             req.DeviceId,
		Floor:          req.Floor,
		Type:           req.DeviceType,
		FirmwareVer:    req.FirmwareVer,
		Status:         "active",
		CertificatePEM: string(bundle.ClientCert),
	}

	if err := s.repo.CreateDevice(ctx, dev); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to store device record: %v", err)
	}

	// 4. Return Bundle (Requirement #5)
	return &pb.CreateDeviceResponse{
		DeviceId:      req.DeviceId,
		BrokerUrl:     s.brokerURL,
		ClientCertPem: bundle.ClientCert,
		ClientKeyPem:  bundle.ClientKey,
		CaPem:         bundle.CACert,
	}, nil
}

func (s *Service) GetDevice(ctx context.Context, req *pb.GetDeviceRequest) (*pb.Device, error) {
	d, err := s.repo.GetDevice(ctx, req.DeviceId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	return &pb.Device{
		DeviceId:    d.ID,
		Floor:       d.Floor,
		DeviceType:  d.Type,
		FirmwareVer: d.FirmwareVer,
		Status:      d.Status,
	}, nil
}

func (s *Service) ListDevices(ctx context.Context, req *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
	devices, err := s.repo.ListDevices(ctx, req.Floor, req.Status)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list devices: %v", err)
	}

	var pbDevices []*pb.Device
	for _, d := range devices {
		pbDevices = append(pbDevices, &pb.Device{
			DeviceId:    d.ID,
			Floor:       d.Floor,
			DeviceType:  d.Type,
			FirmwareVer: d.FirmwareVer,
			Status:      d.Status,
		})
	}

	return &pb.ListDevicesResponse{Devices: pbDevices}, nil
}

func (s *Service) UpdateDeviceStatus(ctx context.Context, req *pb.UpdateDeviceStatusRequest) (*pb.Device, error) {
	if err := s.repo.UpdateStatus(ctx, req.DeviceId, req.Status); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update status: %v", err)
	}

	return s.GetDevice(ctx, &pb.GetDeviceRequest{DeviceId: req.DeviceId})
}

func (s *Service) SetThreshold(ctx context.Context, req *pb.SetThresholdRequest) (*pb.Threshold, error) {
	t := &Thresholds{
		DeviceID:   req.DeviceId,
		TempMax:    float64(req.TempMax),
		VibeMax:    float64(req.VibeMax),
		CurrentMax: float64(req.CurrentMax),
	}

	if err := s.repo.UpdateThresholds(ctx, t); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to set thresholds: %v", err)
	}

	return &pb.Threshold{
		DeviceId:   req.DeviceId,
		TempMax:    req.TempMax,
		VibeMax:    req.VibeMax,
		CurrentMax: req.CurrentMax,
	}, nil
}

func (s *Service) GetThreshold(ctx context.Context, req *pb.GetThresholdRequest) (*pb.Threshold, error) {
	t, err := s.repo.GetThresholds(ctx, req.DeviceId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get thresholds: %v", err)
	}

	return &pb.Threshold{
		DeviceId:   t.DeviceID,
		TempMax:    float32(t.TempMax),
		VibeMax:    float32(t.VibeMax),
		CurrentMax: float32(t.CurrentMax),
	}, nil
}
