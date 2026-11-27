package main

import "testing"

// The displayed rate must span three ticks, not just the latest one: a short
// burst stays in the average for three seconds and then drops out. A counter
// reset (core restart) must not contribute a negative sample.
func TestSpeedWindow(t *testing.T) {
	var w speedWindow
	cases := []struct {
		total int64
		want  int64
	}{
		{0, 0},      // first tick only establishes the baseline
		{100, 100},  // one second at 100 B/s
		{500, 250},  // (100+400)/2 while the window is still filling
		{700, 233},  // (100+400+200)/3, window full
		{1300, 400}, // (600+400+200)/3, the 100-sample drops out
		{1300, 266}, // (600+0+200)/3
		{50, 200},   // (600+0+0)/3, the reset is clamped to zero
	}
	for i, c := range cases {
		if got := w.sample(c.total); got != c.want {
			t.Fatalf("sample %d (total %d) = %d, want %d", i, c.total, got, c.want)
		}
	}
}
