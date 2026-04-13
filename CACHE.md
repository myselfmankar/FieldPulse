# FieldPulse Cache Strategy (Redis)

This document defines the caching and transient data storage patterns used across the FieldPulse microservices architecture.

## Overview
Redis is utilized as a low-latency, in-memory data store for three high-frequency patterns: **Deduplication**, **Alert Suppression**, and **Maintenance Silencing**.

---

## 1. Deduplication (Exactly-Once Ingestion)
Located in `ingest-worker`. This ensures that network retries from the field don't create duplicate database entries.

*   **What is cached**: A unique hash composed of `device_id` + `unix_timestamp`.
*   **TTL Strategy**: **10 Minutes**. 
*   **Key**: `dedup:{device_id}:{timestamp}`
*   **Failure Behavior**: 
    - **Soft Fail**: If Redis is unavailable, the system logs the error and **continues to persist** the data to TimescaleDB. 
    - **Trade-off**: Data Over-provisioning (duplicates) is preferred over Data Loss.

---

## 2. Alert Suppression (Rate Limiting)
Located in `alert-service`. This prevents "Alert Fatigue" from noisy sensors.

*   **What is cached**: An "Active Alert" flag for a specific device.
*   **TTL Strategy**: **5 Minutes**.
*   **Key**: `alert_active:{device_id}`
*   **Failure Behavior**:
    - **Fail Safe**: If Redis is unavailable, the service defaults to **sending the alert** (safety first). 

---

## 3. Maintenance Silencing (Override)
Located in `alert-service` and `gateway`. Allows operators to manually mute a sensor.

*   **What is cached**: A simple boolean flag.
*   **TTL Strategy**: **10 Minutes (Default)**.
*   **Key**: `silence:{device_id}`
*   **Failure Behavior**:
    - **Open Access**: If Redis is down, the system defaults to "Not Silenced" (Un-muted).
