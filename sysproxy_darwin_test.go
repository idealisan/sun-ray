//go:build darwin

package main

import "testing"

func TestSamePACURL(t *testing.T) {
	base := "http://127.0.0.1:10809/pac"
	if !samePACURL(base+"?t=1", base+"?t=2") {
		t.Error("the token should not matter")
	}
	if samePACURL(base, "http://127.0.0.1:10810/pac") {
		t.Error("another port is another server")
	}
	if samePACURL("", base) {
		t.Error("an empty URL matches nothing")
	}
}
