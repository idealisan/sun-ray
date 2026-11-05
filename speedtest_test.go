package main

import (
	"testing"
	"time"
)

func TestFormatLatency(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "-"},
		{500 * time.Microsecond, "500µs"},
		{45 * time.Millisecond, "45ms"},
		{2 * time.Second, "2000ms"},
	}
	for _, c := range cases {
		if got := formatLatency(c.in); got != c.want {
			t.Errorf("formatLatency(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatSpeed(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "-"},
		{512, "512.0 B/s"},
		{2048, "2.0 KB/s"},
		{5 * 1024 * 1024, "5.0 MB/s"},
	}
	for _, c := range cases {
		if got := formatSpeed(c.in); got != c.want {
			t.Errorf("formatSpeed(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMedianDuration(t *testing.T) {
	if got := medianDuration(nil); got != 0 {
		t.Errorf("medianDuration(nil) = %v, want 0", got)
	}
	samples := []time.Duration{50 * time.Millisecond, 10 * time.Millisecond, 30 * time.Millisecond}
	if got := medianDuration(samples); got != 30*time.Millisecond {
		t.Errorf("medianDuration = %v, want 30ms", got)
	}
}

func TestFreeLoopbackPort(t *testing.T) {
	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	if port < 1024 || port > 65535 {
		t.Errorf("port %d out of range", port)
	}
}
