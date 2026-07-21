#!/bin/bash
set -e

APP="Novascope.app"

echo "==> Installing Novascope..."

# Move to Applications
if [ -d "/Applications/${APP}" ]; then
    echo "Removing old version..."
    rm -rf "/Applications/${APP}"
fi
cp -R "${APP}" /Applications/

# Remove quarantine flag (Gatekeeper bypass)
xattr -cr "/Applications/${APP}"

echo "==> Launching Novascope..."
open "/Applications/${APP}"

echo "==> Done! Novascope is running in the menu bar."
echo "    If you don't see the icon, check the menu bar."
