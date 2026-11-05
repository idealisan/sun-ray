package main

// version.go names the running build.

import (
	"runtime/debug"
)

// appVersion is the released version of the app.
const appVersion = "0.17.5"

// versionString names the version and, when the binary was built from a
// git checkout, the commit and date it came from.
func versionString() string {
	v := appVersion
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
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
	if len(revision) >= 7 {
		v += " (" + revision[:7] + ")"
	}
	if len(built) >= 10 {
		v += " " + built[:10]
	}
	return v
}
