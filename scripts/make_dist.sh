#!/bin/bash
set -e
cd "$(dirname "$0")/.."

VERSION="1.0"
DIST_DIR="dist"
APP_NAME="Novascope.app"
ZIP_NAME="Novascope-v${VERSION}.zip"

echo "==> Cleaning previous build..."
rm -rf "${DIST_DIR}" "${APP_NAME}"

echo "==> Creating app bundle structure..."
mkdir -p "${APP_NAME}/Contents/MacOS"
mkdir -p "${APP_NAME}/Contents/Resources"

echo "==> Building claude-monitor..."
go build -o "${APP_NAME}/Contents/MacOS/claude-monitor" .
echo "==> Go binary OK"

echo "==> Building stats helper..."
swiftc -o "${APP_NAME}/Contents/MacOS/novascope-stats-helper" "helpers/stats_window.swift"
echo "==> Stats helper OK"

echo "==> Building sessions panel helper..."
swiftc -o "${APP_NAME}/Contents/MacOS/novascope-panel-helper" "helpers/sessions_panel.swift"
echo "==> Sessions panel helper OK"

echo "==> Creating Info.plist..."
cat > "${APP_NAME}/Contents/Info.plist" << 'PLIST'
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
    <string>VERSION_STRING</string>
    <key>CFBundleVersion</key>
    <string>VERSION_STRING</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST
sed -i '' "s/VERSION_STRING/${VERSION}/g" "${APP_NAME}/Contents/Info.plist"

echo "==> Copying icon..."
if [ -f "assets/Novascope.icns" ]; then
    cp "assets/Novascope.icns" "${APP_NAME}/Contents/Resources/Novascope.icns"
    echo "==> Icon copied from assets/"
elif [ -f "Novascope.app/Contents/Resources/Novascope.icns" ]; then
    cp "Novascope.app/Contents/Resources/Novascope.icns" "${APP_NAME}/Contents/Resources/Novascope.icns"
    echo "==> Icon copied from existing build"
else
    echo "==> WARNING: No icon found, app will use default"
fi

echo "==> Ad-hoc code signing..."
codesign --force --deep -s - "${APP_NAME}"

echo "==> Creating distribution package..."
mkdir -p "${DIST_DIR}"
ditto -c -k --keepParent "${APP_NAME}" "${DIST_DIR}/${ZIP_NAME}"

echo "==> Done: ${DIST_DIR}/${ZIP_NAME}"
ls -lh "${DIST_DIR}/${ZIP_NAME}"
