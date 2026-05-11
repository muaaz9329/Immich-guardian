#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$SCRIPT_DIR/guardian"
PID_FILE="$SCRIPT_DIR/.guardian.pid"
LOG_FILE="$SCRIPT_DIR/guardian.log"
ENV_FILE="$SCRIPT_DIR/.env"

# ── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[guardian]${NC} $1"; }
warn() { echo -e "${YELLOW}[guardian]${NC} $1"; }
err()  { echo -e "${RED}[guardian]${NC} $1"; exit 1; }

# ── Already running? ──────────────────────────────────────────────────────────
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        warn "guardian is already running (PID $PID)"
        warn "run ./stop.sh first if you want to restart"
        exit 1
    else
        warn "stale PID file found, cleaning up..."
        rm -f "$PID_FILE"
    fi
fi

# ── Load .env ─────────────────────────────────────────────────────────────────
if [ ! -f "$ENV_FILE" ]; then
    err ".env file not found. Copy .env.example to .env and fill in your values."
fi

set -a
source "$ENV_FILE"
set +a
log "loaded .env"

# ── Check required envs ───────────────────────────────────────────────────────
REQUIRED=("PUSHOVER_APP_KEY" "PUSHOVER_USER_KEY" "PHOTOS_PATH" "SSD_MOUNT")
for var in "${REQUIRED[@]}"; do
    if [ -z "${!var}" ]; then
        err "required env var $var is not set in .env"
    fi
done

# ── Check dependencies ────────────────────────────────────────────────────────
command -v go     >/dev/null 2>&1 || err "go is not installed"
command -v ffmpeg >/dev/null 2>&1 || err "ffmpeg is not installed. Run: brew install ffmpeg"
command -v docker >/dev/null 2>&1 || err "docker is not installed or not running"

# ── Build ─────────────────────────────────────────────────────────────────────
log "building..."
cd "$SCRIPT_DIR"
go build -o "$BINARY" ./cmd/main 2>&1 | tee -a "$LOG_FILE"
log "build successful → $BINARY"

# ── Launch ────────────────────────────────────────────────────────────────────
log "starting guardian..."
log "logs → $LOG_FILE"

nohup "$BINARY" >> "$LOG_FILE" 2>&1 &
PID=$!
echo $PID > "$PID_FILE"

# Give it a moment to make sure it didn't immediately crash
sleep 1
if ! kill -0 "$PID" 2>/dev/null; then
    rm -f "$PID_FILE"
    err "guardian crashed on startup. Check logs: tail -f $LOG_FILE"
fi

log "guardian running (PID $PID)"
log "tail logs: tail -f $LOG_FILE"