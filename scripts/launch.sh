#!/bin/bash
cd "$(dirname "$0")/.."

echo "==> Killing old process..."
pkill -f "Claude Monitor.app/Contents/MacOS/claude-monitor" 2>/dev/null || true
pkill -f "claude-monitor/claude-monitor" 2>/dev/null || true
sleep 1

echo "==> Launching..."
open "Claude Monitor.app"
echo "==> Done"
