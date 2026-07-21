#!/bin/bash
set -e
cd "$(dirname "$0")/.."

echo "==> Building claude-monitor..."
go build -o "Novascope.app/Contents/MacOS/claude-monitor" .
echo "==> Build OK"

echo "==> Building stats helper..."
swiftc -o "Novascope.app/Contents/MacOS/novascope-stats-helper" "helpers/stats_window.swift"
echo "==> Stats helper OK"

echo "==> Building sessions panel helper..."
swiftc -o "Novascope.app/Contents/MacOS/novascope-panel-helper" "helpers/sessions_panel.swift"
echo "==> Sessions panel helper OK"

echo "==> Killing old process..."
pkill -f "Novascope.app/Contents/MacOS/claude-monitor" 2>/dev/null || true
pkill -f "claude-monitor/claude-monitor" 2>/dev/null || true
pkill -f "novascope-panel-helper" 2>/dev/null || true
sleep 1

echo "==> Launching..."
open "Novascope.app"
echo "==> Done"
