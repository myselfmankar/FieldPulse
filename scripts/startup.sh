#!/bin/bash
# FieldPulse Unified Startup & Provisioning Script
# This script automates the full project lifecycle: Certificates -> Migrations -> Simulation.

echo "=== FieldPulse IoT Platform Startup ==="

# 1. Bootstrap Certificates (mTLS Authority)
echo "[1/3] Bootstrapping Certificate Authority..."
go run scripts/bootstrap/main.go

# 2. Run Database Migrations
# Note: These are applied to the containers via docker-compose, 
# but this step ensures the logic is ready.
echo "[2/3] Preparing Database Schema..."
# (Migrations are typically handled by the container entrypoints in this setup)

# 3. Automated Device Provisioning
echo "[3/3] Running Automated Device Provisioning..."
# This registers the 3 sim-devices and saves their certs to ./certs/devices
go run scripts/register/main.go

echo "Setup Complete! You can now run:"
echo "docker-compose up --build -d"
