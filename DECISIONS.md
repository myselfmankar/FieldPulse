# System Design Decisions

This document justifies the architectural choices made for the FieldPulse IoT Platform.

## 1. Security & Identity

### Why Backend-Provisioned Certs over Device-Generated CSRs?
**Decision**: The backend acts as the sole CA and generates the full keypair/cert bundle for the device.
**Reasoning**: 
- **Simplicity**: Sensor devices are often low-power and lack the compute/libraries to generate secure RSA/ECDSA keys and CSRs reliably.
- **Zero-Touch Provisioning**: The operator receives a single bundle to flash. No back-and-forth handshake is needed during the manufacturing/initialization phase.
- **Control**: The backend has immediate proof of what was issued and can store the public certificate for revocation checks without waiting for a CSR upload.

### Key Algorithm: Why ECDSA P-256 over RSA 2048?
**Decision**: Use ECDSA (P-256 curve).
**Reasoning**:
- **Performance**: ECDSA P-256 offers the same security level as RSA 3072 but with significantly smaller keys and faster signing operations.
- **Bandwidth**: Smaller certificates mean less overhead during the TLS handshake, which is critical for IoT devices that might be on low-bandwidth or metered connections.
- **Modern Standards**: ECDSA is the modern standard for new IoT deployments (Industry 4.0).

### Why `use_identity_as_username true`?
**Decision**: Enforced in Mosquitto.
**Reasoning**:
- **Anti-Impersonation**: This forces the MQTT client username to match the Common Name (CN) of the certificate. 
- Even if a Malicious Device A gets some other generic credentials, it cannot publish as "Device B" unless it has Device B's private key. The broker validates the identity at the "Door" (TLS layer).

## 2. Infrastructure & Storage

### Why Postgres + TimescaleDB (Separated)?
**Decision**: PostgreSQL for Registry, TimescaleDB for Telemetry.
**Reasoning**:
- **Registry (Postgres)**: Device metadata (floor, status, thresholds) is relational and changes rarely. We need ACID compliance and standard B-Tree indexing.
- **Telemetry (TimescaleDB)**: Telemetry is "Append-Only" high-volume data. Timescale's **Hypertables** automatically partition data by time, ensuring that inserts remain fast even when we have millions of rows. 
- **Efficiency**: We use Timescale's **Compression Policy** (>7 days) to reduce storage costs by up to 90% while keeping data queryable.

## 3. Communication Protocols

### "Proto-First" Architecture
**Decision**: Protobuf is the source of truth for all internal and external data structures.
**Reasoning**:
- **Modern gRPC Standard**: As per company feedback, the system prioritizes gRPC for robustness and efficiency. 
- **Type Safety**: Using Protobuf generation prevents drift between the API Gateway and backend services.
- **Binary Efficiency**:Internal gRPC calls use HTTP/2 multiplexing and binary serialization, significantly reducing latency compared to REST.

### Hybrid API Gateway (JSON Bridge)
**Decision**: The Gateway supports both modern Protobuf-backed logic and a resilient JSON fallback.
**Reasoning**:
- **Interoperability**: While the internal core is pure gRPC, the external Gateway provides a bridge for legacy systems or simple `curl` testing that may not have gRPC client capabilities.
- **Resiliency**: The "Robust Parsing" layer cleans up manual shell inputs (like unquoted JSON keys) to prevent trivial failures during manual testing cycles, while still mapping accurately to the official Protobuf fields.

## 4. Business Logic & Timing (5-10-15 Distribution)

### Requirement Alignment
**Decision**: Explicitly implemented the 5m/10m/15m timing distribution from the assignment specifications.
**Reasoning**:
- **5 Minute Silencing (Alert Service)**: Optimized for noisy sensor environments. We store an "active alert" state in Redis with a 5-minute TTL to prevent "alert fatigue" without losing critical data.
- **10 Minute Silence (Gateway Default)**: The manual silence command defaults to 10 minutes, providing a standard maintenance window for operators as suggested by project norms.
- **15 Minute Watchdog (Reliability)**: Devices are marked "SILENT" only after 15 minutes of inactivity. This account for network jitter or transient outages while still providing a strict reliability guarantee via immediate MQTT event publishing (`device/silent/{id}`).

## 5. Observability

### Choice: OpenTelemetry + Uptrace
**Decision**: Full distributed tracing instrumentation across all services.
**Reasoning**:
- **End-to-End Visibility**: In a distributed event-driven system (MQTT -> Connector -> Worker -> DB), simple logs are insufficient. Tracing allows us to follow a single sensor reading through the entire pipeline.
- **Performance Profiling**: We can pinpoint exactly which part of the ingestion pipeline or which SQL query is causing latency.
- **Service Dependency Graph**: Automatically generated to prove the architectural integrity of the distributed system.
