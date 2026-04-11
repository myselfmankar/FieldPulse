# FieldPulse - Full Stack Launcher
# Runs all services in separate PowerShell windows
# Usage: .\scripts\start-all.ps1

$root = "D:\GoLain.io"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   FieldPulse Industrial IoT Platform  " -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# --- Step 1: Infrastructure Check ---
Write-Host "[1/6] Checking Docker infrastructure..." -ForegroundColor Yellow
$containers = docker ps --format "{{.Names}}" 2>&1
$required = @("fieldpulse-postgres", "fieldpulse-timescaledb", "fieldpulse-redis", "fieldpulse-mosquitto", "fieldpulse-mosquitto-remote")
$missing = $required | Where-Object { $containers -notcontains $_ }

if ($missing.Count -gt 0) {
    Write-Host "  Starting missing containers: $($missing -join ', ')" -ForegroundColor Yellow
    docker compose -f "$root\docker-compose.yml" up -d
    Write-Host "  Waiting 5s for containers to be ready..." -ForegroundColor Gray
    Start-Sleep 5
} else {
    Write-Host "  All Docker containers already running." -ForegroundColor Green
}

Write-Host ""

# --- Helper: Open a named PowerShell window ---
function Open-ServiceWindow {
    param(
        [string]$Title,
        [string]$Command,
        [string]$Color = "DarkBlue"
    )
    Write-Host "  Starting: $Title" -ForegroundColor Green
    Start-Process powershell -ArgumentList `
        "-NoExit", "-Command", `
        "`$host.UI.RawUI.WindowTitle = '$Title'; `$host.UI.RawUI.BackgroundColor = '$Color'; Clear-Host; Write-Host '=== $Title ===' -ForegroundColor Cyan; Write-Host ''; Set-Location '$root'; $Command"
}

# --- Step 2: Core Backend Services ---
Write-Host "[2/6] Starting core backend services..." -ForegroundColor Yellow

Open-ServiceWindow `
    -Title "Device Service (gRPC :50051)" `
    -Command "go run cmd/device-service/main.go" `
    -Color "DarkBlue"

Start-Sleep 2

Open-ServiceWindow `
    -Title "Gateway (REST API :3000)" `
    -Command "go run cmd/gateway/main.go" `
    -Color "DarkGreen"

Start-Sleep 2

Open-ServiceWindow `
    -Title "Ingest Worker (Telemetry Pipeline)" `
    -Command "go run cmd/ingest-worker/main.go" `
    -Color "DarkMagenta"

Start-Sleep 2

# --- Step 3: Alert Engine & Watchdog ---
Write-Host "[3/6] Starting Alert Engine & Watchdog..." -ForegroundColor Yellow

Open-ServiceWindow `
    -Title "Alert Engine" `
    -Command "go run cmd/alert-service/main.go" `
    -Color "DarkRed"

Start-Sleep 2

Open-ServiceWindow `
    -Title "Watchdog (Heartbeat Monitor)" `
    -Command "go run cmd/watchdog/main.go" `
    -Color "DarkCyan"

Start-Sleep 2

# --- Step 4: Outbound Connector ---
Write-Host "[4/6] Starting Outbound MQTT Connector..." -ForegroundColor Yellow

Open-ServiceWindow `
    -Title "Outbound Connector (Bridge)" `
    -Command "go run cmd/connector/main.go" `
    -Color "Black"

# Wait for connector to connect (it compiles and connects in ~5s)
Write-Host "  Waiting 8s for connector to establish connections..." -ForegroundColor Gray
Start-Sleep 8

# --- Step 5: Simulator ---
Write-Host "[5/6] Registering bridge test devices & starting Simulator..." -ForegroundColor Yellow

# Register the 3 bridge devices silently (ignore already-exists errors)
@("000","001","002") | ForEach-Object {
    $id = "client-a-sim-device-$_"
    $body = "{`"device_id`":`"$id`",`"floor`":`"Factory`",`"device_type`":`"bridge-sim`",`"firmware_ver`":`"1.0.0`"}"
    try {
        Invoke-RestMethod -Uri http://localhost:3000/v1/devices -Method POST -ContentType "application/json" -Body $body -ErrorAction SilentlyContinue | Out-Null
        Write-Host "  Registered: $id" -ForegroundColor Gray
    } catch { Write-Host "  Already exists: $id" -ForegroundColor Gray }
}

Open-ServiceWindow `
    -Title "Simulator (Remote Mode - External Factory)" `
    -Command "go run cmd/simulator/main.go -mode-remote=true -broker-url=tcp://localhost:1883 -n=3" `
    -Color "DarkYellow"

# --- Done ---
Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  All services launched!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Windows opened:" -ForegroundColor White
Write-Host "    [Blue]    Device Service  (gRPC Registry)"
Write-Host "    [Green]   Gateway         (REST API on :3000)"
Write-Host "    [Magenta] Ingest Worker   (Telemetry pipeline)"
Write-Host "    [Red]     Alert Engine    (Threshold alerting)"
Write-Host "    [Cyan]    Watchdog        (Heartbeat monitor)"
Write-Host "    [Black]   Connector       (External broker bridge)"
Write-Host "    [Yellow]  Simulator       (Remote factory devices)"
Write-Host ""
Write-Host "  Test the API:" -ForegroundColor White
Write-Host "    Invoke-RestMethod http://localhost:3000/v1/devices" -ForegroundColor Gray
Write-Host ""
Write-Host "  Stop everything:" -ForegroundColor White
Write-Host "    Get-CimInstance Win32_Process -Filter `"name='go.exe'`" | Stop-Process -Force" -ForegroundColor Gray
Write-Host ""
