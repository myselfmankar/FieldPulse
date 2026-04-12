# Stage 1: Build the Go binaries
FROM golang:alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build all 8 services
RUN go build -o bin/gateway ./cmd/gateway/main.go
RUN go build -o bin/device-service ./cmd/device-service/main.go
RUN go build -o bin/telemetry-service ./cmd/telemetry-service/main.go
RUN go build -o bin/ingest-worker ./cmd/ingest-worker/main.go
RUN go build -o bin/alert-service ./cmd/alert-service/main.go
RUN go build -o bin/connector ./cmd/connector/main.go
RUN go build -o bin/watchdog ./cmd/watchdog/main.go
RUN go build -o bin/simulator ./cmd/simulator/main.go
RUN go build -o bin/register ./scripts/register/main.go

# Stage 2: Final lightweight image
FROM alpine:3.18

WORKDIR /app

# Install runtime dependencies (for mTLS and networking)
RUN apk add --no-cache ca-certificates

# Copy binaries from the builder
COPY --from=builder /app/bin/* /app/

# The CMD should be overridden in docker-compose for each service
CMD ["./gateway"]
