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

### Why gRPC for Internal Service Talk?
**Decision**: All service-to-service communication is via gRPC/Protobuf.
**Reasoning**:
- **Type Safety**: Protobuf acts as a "single source of truth." If the Device Service changes its response, the API Gateway will fail to compile, preventing runtime bugs.
- **Performance**: Binary serialization is faster and smaller than JSON over HTTP/1.1.
- **Multiplexing**: Uses HTTP/2, allowing multiple requests over a single connection.
