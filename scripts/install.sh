#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
APP="${SCRIPT_DIR}/Novascope.app"

echo "========================================"
echo "  Novascope - Claude Code Monitor"
echo "========================================"
echo ""

if [ ! -d "${APP}" ]; then
    echo "ERROR: Novascope.app not found next to this script."
    echo "Make sure install.sh is in the same folder as Novascope.app"
    exit 1
fi

echo "==> Removing quarantine flag..."
xattr -cr "${APP}"

echo "==> Launching Novascope..."
open "${APP}"

echo ""
echo "========================================"
echo "  Done!"
echo ""
echo "  Novascope runs in the MENU BAR (top-right)."
echo "  Look for the colored dot icon."
echo ""
echo "  If you don't see it, check:"
echo "  1. Is Claude Code running? The dot turns"
echo "     gray when no Claude session is active."
echo "  2. System Settings > Control Center >"
echo "     Menu Bar Only apps"
echo ""
echo "  To auto-start on login:"
echo "  Drag Novascope.app to:"
echo "  System Settings > General > Login Items"
echo "========================================"