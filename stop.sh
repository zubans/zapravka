#!/bin/bash

ROOT="$(cd "$(dirname "$0")" && pwd)"

if [ -f "$ROOT/backend.pid" ]; then
  kill $(cat "$ROOT/backend.pid") 2>/dev/null || true
  rm -f "$ROOT/backend.pid"
  echo "Backend stopped"
fi
if [ -f "$ROOT/frontend.pid" ]; then
  kill $(cat "$ROOT/frontend.pid") 2>/dev/null || true
  rm -f "$ROOT/frontend.pid"
  echo "Frontend stopped"
fi
