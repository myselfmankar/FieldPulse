import subprocess
import time
import os
import sys

# Configuration
DSN = "https://1yfViphsk4DqdeFLUFZ_yA@api.uptrace.dev"
os.environ["UPTRACE_DSN"] = DSN
os.environ["UPTRACE_DEBUG"] = "1"

SERVICES = [
    ("Device Service", "go run ./cmd/device-service"),
    ("Gateway SL",     "go run ./cmd/gateway"),
    ("Ingest Worker",  "go run ./cmd/ingest-worker"),
    ("Alert Engine",   "go run ./cmd/alert-service"),
    ("Watchdog",       "go run ./cmd/watchdog"),
    ("MQTT Connector", "go run ./cmd/connector"),
    ("Simulator",      "go run ./cmd/simulator"),
]

processes = {}

def run_docker():
    print("[1/2] Starting Docker core...")
    try:
        subprocess.run(["docker", "compose", "up", "-d"], check=True)
        print("Waiting 5s for infra readiness...")
        time.sleep(5)
    except Exception as e:
        print(f"Docker Error: {e}")
        sys.exit(1)

def launch_services():
    print("[2/2] Launching backend services...")
    if not os.path.exists("logs"):
        os.makedirs("logs")
        
    for name, cmd in SERVICES:
        log_file = open(f"logs/{name.lower().replace(' ', '_')}.log", "w")
        # Start in background and pipe logs to files
        p = subprocess.Popen(
            cmd,
            shell=True,
            stdout=log_file,
            stderr=log_file,
            cwd=os.getcwd()
        )
        processes[name] = p
        time.sleep(0.5) # Small stagger

def check_health():
    print("\n" + "="*40)
    print("   SERVICE HEALTH REPORT")
    print("="*40)
    
    # Wait a moment for any immediate crashes
    time.sleep(3)
    
    all_ok = True
    for name, p in processes.items():
        # poll() returns None if the process is still running
        if p.poll() is None:
            print(f"  [OK] {name}")
        else:
            print(f"  [!!] {name} - FAILED (Check Uptrace for errors)")
            all_ok = False
            
    print("="*40)
    print(f"\nUptrace Dashboard: https://app.uptrace.dev")
    if all_ok:
        print("\nAll services started successfully in the background.")
    else:
        print("\nSome services failed to start. Verify your Go environment.")

if __name__ == "__main__":
    print(" === FieldPulse Background Launcher === ")
    
    run_docker()
    launch_services()
    check_health()
    
    try:
        print("\nPress Ctrl+C to stop all services and exit.")
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        print("\nStopping services...")
        for p in processes.values():
            p.terminate()
        print("Goodbye!")
