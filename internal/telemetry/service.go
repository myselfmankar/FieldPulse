package telemetry

import (
	"context"
	"fmt"

	pb "github.com/myselfmankar/FieldPulse/proto/telemetry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Service struct {
	pb.UnimplementedTelemetryServiceServer
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetLatestReading(ctx context.Context, req *pb.GetLatestReadingRequest) (*pb.TelemetryReading, error) {
	rd, err := s.repo.GetLatestReading(ctx, req.DeviceId)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest reading: %w", err)
	}

	return &pb.TelemetryReading{
		DeviceId:  rd.DeviceID,
		Timestamp: timestamppb.New(rd.Timestamp),
		Metrics: &pb.Metrics{
			TemperatureC: float32(rd.Temperature),
			VibrationHz:  float32(rd.Vibration),
			CurrentMa:    float32(rd.CurrentMa),
		},
	}, nil
}

func (s *Service) QueryTelemetry(ctx context.Context, req *pb.QueryTelemetryRequest) (*pb.QueryTelemetryResponse, error) {
	start := req.StartTime.AsTime()
	end := req.EndTime.AsTime()

	aggs, err := s.repo.QueryAggregates(ctx, req.DeviceId, req.Resolution, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query aggregates: %w", err)
	}

	var pbAggs []*pb.TelemetryAggregate
	for _, a := range aggs {
		pbAggs = append(pbAggs, &pb.TelemetryAggregate{
			Bucket:   timestamppb.New(a.Bucket),
			MeanTemp: float32(a.AvgTemp),
			MaxTemp:  float32(a.MaxTemp),
			MinTemp:  float32(a.MinTemp),
			// Note: mean_vibration and mean_current fields require .proto regeneration
			// I've included them in the .proto, so if you run protoc, these will be available.
		})
	}

	return &pb.QueryTelemetryResponse{
		Aggregates: pbAggs,
	}, nil
}
