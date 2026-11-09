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

// The weekly geo builds tag their releases v<version>-<short hash>-<date>,
// and the binary has to report the very same string.
func TestFormatVersion(t *testing.T) {
	cases := []struct {
		name, version, revision, built, want string
	}{
		{
			name:     "git checkout",
			version:  "0.19.0",
			revision: "bf79793bed73f88cbe28934b6e5e7f58b5d14613",
			built:    "2026-11-10T03:12:00Z",
			want:     "0.19.0-bf79793-20261110",
		},
		{
			name:    "no vcs info",
			version: "0.19.0",
			want:    "0.19.0",
		},
		{
			name:     "date without a full hash",
			version:  "0.19.0",
			revision: "abc",
			built:    "2026-11-10T03:12:00Z",
			want:     "0.19.0-20261110",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatVersion(tc.version, tc.revision, tc.built); got != tc.want {
				t.Fatalf("formatVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}
