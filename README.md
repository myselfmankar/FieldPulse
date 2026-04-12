# FieldPulse
Industrial IoT Backend Platform

# architecture (v2 - more focus on Open Source Lib)
# Architecture Design (Mermaid)

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

