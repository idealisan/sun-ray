package main

// version.go names the running build.

import (
	_ "embed"
	"encoding/json"
	"runtime/debug"
	"strings"
)

// mygoManifest is the manifest MyGo builds the macOS app bundle from. It
// names the disk image and carries the version of the release, so the app
// reads its version from here: one place to change, and the bundle and
// the binary cannot disagree.
//
//go:embed mygo.json
var mygoManifest []byte

// appVersion is the released version of the app.
var appVersion = manifestVersion()

// manifestVersion reads the version out of the manifest.
func manifestVersion() string {
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(mygoManifest, &manifest); err != nil || manifest.Version == "" {
		return "0.0.0"
	}
	return manifest.Version
}

// versionString names the version and, when the binary was built from a
// git checkout, the short commit hash and the date it came from. This is
// also the shape of the weekly geo build's release tag, so a binary and
// its release agree: 0.19.0-bf79793-20261110.
func versionString() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return appVersion
	}
	var revision, built string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.time":
			built = setting.Value
		}
	}
	return formatVersion(appVersion, revision, built)
}

// formatVersion appends the short commit hash and the build date (YYYYMMDD)
// to the manifest version. A hash or timestamp that is too short is ignored,
// so a build outside a git checkout still reports just the version.
func formatVersion(version, revision, built string) string {
	v := version
	if len(revision) >= 7 {
		v += "-" + revision[:7]
	}
	if len(built) >= 10 {
		v += "-" + strings.ReplaceAll(built[:10], "-", "")
	}
	return v
}
