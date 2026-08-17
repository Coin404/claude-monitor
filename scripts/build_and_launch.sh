#!/bin/bash
set -e
cd "$(dirname "$0")/.."

echo "==> Preparing app bundle structure..."
mkdir -p "Novascope.app/Contents/MacOS"
mkdir -p "Novascope.app/Contents/Resources"

echo "==> Copying icon..."
if [ -f "assets/Novascope.icns" ]; then
    cp "assets/Novascope.icns" "Novascope.app/Contents/Resources/Novascope.icns"
elif [ -f "icon/Novascope.icns" ]; then
    cp "icon/Novascope.icns" "Novascope.app/Contents/Resources/Novascope.icns"
else
    echo "==> WARNING: No icon found, app will use default"
fi

echo "==> Creating Info.plist..."
cat > "Novascope.app/Contents/Info.plist" << 'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>claude-monitor</string>
    <key>CFBundleIconFile</key>
    <string>Novascope</string>
    <key>CFBundleIdentifier</key>
    <string>com.novascope.monitor</string>
    <key>CFBundleName</key>
    <string>Novascope</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0</string>
    <key>CFBundleVersion</key>
    <string>1.0</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

echo "==> Building claude-monitor..."
rm -f "Novascope.app/Contents/MacOS/claude-monitor" "Novascope.app/Contents/MacOS/novascope-panel-helper" "Novascope.app/Contents/MacOS/novascope-settings-helper"
go build -o "Novascope.app/Contents/MacOS/claude-monitor" .
echo "==> Build OK"

echo "==> Building sessions panel helper..."
swiftc -o "Novascope.app/Contents/MacOS/novascope-panel-helper" "helpers/sessions_panel.swift"
echo "==> Sessions panel helper OK"

echo "==> Building settings window helper..."
swiftc -o "Novascope.app/Contents/MacOS/novascope-settings-helper" "helpers/settings_window.swift"
echo "==> Settings helper OK"

echo "==> Killing old process..."
pkill -f "Novascope.app/Contents/MacOS/claude-monitor" 2>/dev/null || true
pkill -f "claude-monitor/claude-monitor" 2>/dev/null || true
pkill -f "novascope-panel-helper" 2>/dev/null || true
sleep 1

echo "==> Launching..."
open "Novascope.app"
echo "==> Done"
