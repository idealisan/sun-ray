package main

import (
	"testing"
)

// The manifest names the macOS disk image and carries the version of the
// release. The app reads its version from it, so the two cannot drift, but
// a manifest that lost its version would leave the app calling itself
// 0.0.0.
func TestManifestCarriesTheVersion(t *testing.T) {
	if appVersion == "" || appVersion == "0.0.0" {
		t.Fatalf("the manifest has no usable version, the app reports %q", appVersion)
	}
	if got := manifestVersion(); got != appVersion {
		t.Fatalf("appVersion is %q, the manifest says %q", appVersion, got)
	}
}
