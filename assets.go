package main

// assets.go embeds the geo assets into the binary and serves them to
// xray-core from memory through filesystem.AssetProvider (a small patch
// in patches/ applied to the xray-core submodule). No files are ever
// written to the user disk and no extra files ship with the app.

import (
	"bytes"
	"embed"
	"io"

	"github.com/xtls/xray-core/common/platform/filesystem"
)

//go:embed assets/geoip.dat assets/geosite.dat
var assetFS embed.FS

// assetData holds the embedded geo tables, keyed by the file name xray
// asks for. geoip.dat carries the CN ranges, geosite.dat the CN and
// category-ads-all domain lists (extracted from the upstream v2fly
// files down to just what the routing presets need).
var assetData = map[string][]byte{
	"geoip.dat":   mustAsset("assets/geoip.dat"),
	"geosite.dat": mustAsset("assets/geosite.dat"),
}

func mustAsset(name string) []byte {
	data, err := assetFS.ReadFile(name)
	if err != nil {
		panic("embedded asset missing: " + err.Error())
	}
	return data
}

func init() {
	filesystem.AssetProvider = func(file string) (io.ReadCloser, int64, bool) {
		data, ok := assetData[file]
		if !ok {
			return nil, 0, false // fall through to the on-disk lookup
		}
		return io.NopCloser(bytes.NewReader(data)), int64(len(data)), true
	}
}
