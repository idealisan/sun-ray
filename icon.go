package main

import _ "embed"

// Icons are drawn as SVG in assets/ and rendered to PNG by
// scripts/render-icons.sh (macOS sips). The app icon is also packaged into
// the macOS bundle by mygo build; the tray icons are embedded here.

//go:embed assets/icon.png
var appIconPNG []byte

//go:embed assets/tray-template.png
var trayTemplatePNG []byte

//go:embed assets/tray-color.png
var trayColorPNG []byte
