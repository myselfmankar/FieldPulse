# FieldPulse 🚀
### Industrial IoT Backend Platform

---

## 👋 About Me
Hi, I'm **Vaishnav Mankar**.  
This project was a significant milestone for me. Before this assignment, I had zero experience with the **Go** programming language. Over the course of this project, I've learned to build high-performance distributed systems, manage observability with OpenTelemetry, and secure IoT communication with mTLS—all using Go. 

While it was a steep learning curve, I've made every effort to implement a robust, industry-standard platform. This is the technical assignment provided by [Golain.io](https://www.golain.io/)!

---

## 🏗️ Architecture Design

```mermaid
graph TD
    Client[HTTP/REST Client] -->|GoFiber| Gateway[API Gateway]
    
    subgraph "Core Services (Go + gRPC)"
        Gateway -->|gRPC| DeviceSvc[Device Service]
        Gateway -->|gRPC| TelemetrySvc[Telemetry Service]
        
        DeviceSvc -->|SQLBoiler| Postgres[(PostgreSQL Registry)]
        TelemetrySvc -->|pgx + Raw SQL| Timescale[(TimescaleDB)]
        
        AlertSvc[Alert Service] -->|gRPC| DeviceSvc
    end
    
    subgraph "Messaging & State"
        InternalMQTT((Mosquitto mTLS))
        RemoteMQTT((Mosquitto Plain))
        Redis[(Redis)]
        
        Simulator -->|Pub| InternalMQTT
        Connector -->|Sub Remote| RemoteMQTT
        Connector -->|Pub Internal| InternalMQTT
        
        InternalMQTT --> TelemetrySvc
        InternalMQTT --> AlertSvc
        
        TelemetrySvc & AlertSvc & Connector --- Redis
    end
    
    subgraph "Observability"
        AllServices[All Services] -->|OTLP| Uptrace[Uptrace]
    end
```
### **Detailed Data Flow & Network Architecture**
```mermaid
graph LR
    SIM["Simulator"] -->|mTLS:8883| MQTT["Mosquitto Internal"]
    SIM2["Simulator --mode-remote"] -->|tcp:1883| RMQTT["Mosquitto Remote"]
    RMQTT -->|Connector subscribes| CONN["Connector"]
    CONN -->|Re-publish mTLS:8883| MQTT
    MQTT -->|Subscribe| IW["Ingest Worker"]
    MQTT -->|Subscribe| ALERT["Alert Service"]
    IW -->|INSERT| TSDB["TimescaleDB :5433"]
    IW -->|Dedup| REDIS["Redis :6379"]
    ALERT -->|gRPC| DS["Device Service :50051"]
    ALERT -->|Silence check| REDIS
    DS -->|SQL| PG["PostgreSQL :5432"]
    WD["Watchdog"] -->|Query| TSDB
    WD -->|gRPC| DS
    GW["Gateway :3000"] -->|gRPC| DS
    GW -->|gRPC| TS["Telemetry Service :50052"]
    TS -->|SQL| TSDB
```


---

## 🛠️ Technical Implementation

### **1. TimescaleDB Performance Stack**
Our telemetry data uses advanced TimescaleDB features for long-term scalability:
*   **Hypertable**: The `telemetry` table is partitioned by `time` for high-velocity ingestion.
*   **Compression**: Data older than **7 days** is automatically compressed, reducing storage by ~90%.
*   **Retention**: Data older than **90 days** is automatically dropped to keep storage costs predictable.
*   **Continuous Aggregates**: An hourly view (`telemetry_hourly`) is materialized automatically for high-performance querying of historical trends.

### **2. Redis Infrastructure**
Redis manages transient system states:
*   **Deduplication**: 10m TTL lock on sensor packets to ensure "Exactly Once" processing.
*   **Alert Suppression**: 5m TTL window to prevent notification fatigue.
*   **Maintenance**: 10m (configurable) manual override to mute noisy sensors.
*   *See [CACHE.md](./CACHE.md) for detailed failure behavior and TTL strategies.*

### **3. Observability & Tracing**
The system is fully instrumented with **OpenTelemetry**.
*   **Trace Propagation**: Context is carried from the **API Gateway** $\rightarrow$ **Backend Services** and even across the **MQTT Message Bus**.
*   **Uptrace**: All traces are exported to Uptrace Cloud for real-time visualization.

---

## 🚀 One-Click Deployment
1. Build and start all 11 containers:
   ```bash
   docker-compose up --build
   ```
2. The `init-provisioner` will automatically bootstrap the system, register devices, and start the simulation.
