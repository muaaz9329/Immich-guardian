#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="$SCRIPT_DIR/.guardian.pid"
LOG_FILE="$SCRIPT_DIR/guardian.log"

# ── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[guardian]${NC} $1"; }
warn() { echo -e "${YELLOW}[guardian]${NC} $1"; }
err()  { echo -e "${RED}[guardian]${NC} $1"; exit 1; }

# ── Check PID file ────────────────────────────────────────────────────────────
if [ ! -f "$PID_FILE" ]; then
    warn "no PID file found — guardian may not be running"
    exit 0
fi

PID=$(cat "$PID_FILE")

if ! kill -0 "$PID" 2>/dev/null; then
    warn "guardian is not running (stale PID $PID)"
    rm -f "$PID_FILE"
    exit 0
fi

# ── Graceful shutdown (SIGTERM → wait → SIGKILL) ──────────────────────────────
log "stopping guardian (PID $PID)..."
kill -TERM "$PID"

# Wait up to 10 seconds for graceful shutdown
for i in $(seq 1 10); do
    if ! kill -0 "$PID" 2>/dev/null; then
        break
    fi
    sleep 1
done

# Force kill if still alive
if kill -0 "$PID" 2>/dev/null; then
    warn "guardian didn't stop gracefully, force killing..."
    kill -KILL "$PID"
    sleep 1
fi

rm -f "$PID_FILE"
log "guardian stopped"
log "logs saved at $LOG_FILE"