#!/bin/sh
# Renders the SVG sources into the PNGs the app and its tray use.
# Needs macOS (sips). Run from anywhere.
set -eu
cd "$(dirname "$0")/.."

sips -s format png assets/icon.svg --out assets/icon.png >/dev/null
sips -s format png assets/tray-template.svg --out assets/tray-template.png >/dev/null
sips -s format png assets/tray-color.svg --out assets/tray-color.png >/dev/null
