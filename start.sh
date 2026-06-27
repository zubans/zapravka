#!/bin/bash
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[start]${NC} $1"; }
warn() { echo -e "${YELLOW}[warn]${NC} $1"; }
error() { echo -e "${RED}[error]${NC} $1"; }

check_command() {
  command -v "$1" >/dev/null 2>&1
}

log "Checking dependencies..."

MISSING=""
if ! check_command go; then
  MISSING="$MISSING go"
fi
if ! check_command node; then
  MISSING="$MISSING node"
fi
if ! check_command npm; then
  MISSING="$MISSING npm"
fi
if ! check_command git; then
  MISSING="$MISSING git"
fi

if [[ -n "$MISSING" ]]; then
  error "Missing required tools:$MISSING"
  echo "Run ./install.sh first to install everything automatically."
  exit 1
fi

log "Go: $(go version)"
log "Node: $(node --version), npm: $(npm --version)"

# Остановить старые, если есть
if [ -f "$ROOT/backend.pid" ]; then
  kill $(cat "$ROOT/backend.pid") 2>/dev/null || true
  rm -f "$ROOT/backend.pid"
fi
if [ -f "$ROOT/frontend.pid" ]; then
  kill $(cat "$ROOT/frontend.pid") 2>/dev/null || true
  rm -f "$ROOT/frontend.pid"
fi

# Установить frontend зависимости, если нужно
if [ ! -d "$ROOT/frontend/node_modules" ]; then
  warn "frontend/node_modules not found, running npm install..."
  cd "$ROOT/frontend"
  npm install
fi

# Собрать backend
cd "$ROOT/backend"
log "Building backend..."
go build -o zapravka .

# Backend: собираем бинарник и запускаем его напрямую
log "Starting backend on http://localhost:8081"
nohup "$ROOT/backend/zapravka" > "$ROOT/backend.log" 2>&1 &
echo $! > "$ROOT/backend.pid"

# Frontend
log "Starting frontend on http://localhost:5173"
cd "$ROOT/frontend"
nohup npm run dev > "$ROOT/frontend.log" 2>&1 &
echo $! > "$ROOT/frontend.pid"

sleep 2

# Проверить, что сервисы поднялись
BACKEND_OK=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:8081/api/stations?lat=55.7558&lon=37.6173&radius=3000" 2>/dev/null || echo "000")
FRONTEND_OK=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:5173/" 2>/dev/null || echo "000")

if [[ "$BACKEND_OK" == "200" && "$FRONTEND_OK" == "200" ]]; then
  log "Both services are up and running!"
  log "Frontend: http://localhost:5173"
  log "Backend:  http://localhost:8081"
else
  warn "Services may not be fully ready yet (backend: $BACKEND_OK, frontend: $FRONTEND_OK)"
  warn "Check logs: $ROOT/backend.log, $ROOT/frontend.log"
fi
