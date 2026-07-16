#!/bin/bash
set -e
cd "$(dirname "$0")/.."

echo "==> Building claude-monitor..."
go build -o "Novascope.app/Contents/MacOS/claude-monitor" .
echo "==> Build OK"

echo "==> Killing old process..."
pkill -f "Novascope.app/Contents/MacOS/claude-monitor" 2>/dev/null || true
pkill -f "claude-monitor/claude-monitor" 2>/dev/null || true
sleep 1

echo "==> Launching..."
open "Novascope.app"
echo "==> Done"
