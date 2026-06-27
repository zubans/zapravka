#!/bin/bash
set -e

# Убедимся, что Go из /usr/local/go доступен, даже если PATH ещё не обновлён
if [ -x /usr/local/go/bin/go ]; then
  export PATH="/usr/local/go/bin:$PATH"
fi

ROOT="$(cd "$(dirname "$0")" && pwd)"
export CACHE_DB_PATH="$ROOT/zapravka_cache.db"

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

# Читаем порт frontend из .env
FRONTEND_PORT=80
if [ -f "$ROOT/frontend/.env" ]; then
  FRONTEND_PORT=$(grep '^VITE_PORT=' "$ROOT/frontend/.env" | cut -d= -f2 | tr -d '[:space:]' || echo "80")
fi
FRONTEND_PORT=${FRONTEND_PORT:-80}

BACKEND_PORT=${BACKEND_PORT:-8081}

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
log "Frontend port: $FRONTEND_PORT"
log "Backend port: $BACKEND_PORT"

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

# Backend: слушает 0.0.0.0
export HOST=0.0.0.0
export PORT=$BACKEND_PORT
log "Starting backend on http://0.0.0.0:$BACKEND_PORT"
nohup "$ROOT/backend/zapravka" > "$ROOT/backend.log" 2>&1 &
echo $! > "$ROOT/backend.pid"

# Frontend: слушает 0.0.0.0, порт из .env
log "Starting frontend on http://0.0.0.0:$FRONTEND_PORT"
cd "$ROOT/frontend"
nohup npm run dev > "$ROOT/frontend.log" 2>&1 &
echo $! > "$ROOT/frontend.pid"

sleep 3

# Проверить, что сервисы поднялись
BACKEND_OK=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:$BACKEND_PORT/api/stations?lat=55.7558&lon=37.6173&radius=3000" 2>/dev/null || echo "000")
FRONTEND_OK=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:$FRONTEND_PORT/" 2>/dev/null || echo "000")

if [[ "$BACKEND_OK" == "200" && "$FRONTEND_OK" == "200" ]]; then
  log "Both services are up and running!"
  log "Frontend: http://0.0.0.0:$FRONTEND_PORT"
  log "Backend:  http://0.0.0.0:$BACKEND_PORT"
else
  warn "Services may not be fully ready yet (backend: $BACKEND_OK, frontend: $FRONTEND_OK)"
  warn "Check logs: $ROOT/backend.log, $ROOT/frontend.log"
  if [[ "$FRONTEND_PORT" -lt 1024 && $EUID -ne 0 ]]; then
    warn "Ports below 1024 require root. Run as root or change VITE_PORT in frontend/.env"
  fi
fi
