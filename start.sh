#!/bin/bash
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"

# Остановить старые, если есть
if [ -f "$ROOT/backend.pid" ]; then
  kill $(cat "$ROOT/backend.pid") 2>/dev/null || true
  rm -f "$ROOT/backend.pid"
fi
if [ -f "$ROOT/frontend.pid" ]; then
  kill $(cat "$ROOT/frontend.pid") 2>/dev/null || true
  rm -f "$ROOT/frontend.pid"
fi

# Backend: собираем бинарник и запускаем его напрямую
cd "$ROOT/backend"
go build -o zapravka .
nohup "$ROOT/backend/zapravka" > "$ROOT/backend.log" 2>&1 &
echo $! > "$ROOT/backend.pid"
echo "Backend started on http://localhost:8081 (PID $(cat "$ROOT/backend.pid"))"

# Frontend
cd "$ROOT/frontend"
nohup npm run dev > "$ROOT/frontend.log" 2>&1 &
echo $! > "$ROOT/frontend.pid"
echo "Frontend started on http://localhost:5173 (PID $(cat "$ROOT/frontend.pid"))"
