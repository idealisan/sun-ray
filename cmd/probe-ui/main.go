// Command probe-ui measures what the mygo UI alone costs: it opens one
// window and nothing else, so a build of it shows the size and the
// memory the UI framework needs on its own. The window size comes from
// PROBE_W and PROBE_H, to see how the memory follows it.
package main

import (
	"log"
	"os"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func envInt(name string, fallback int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func main() {
	w := envInt("PROBE_W", 880)
	h := envInt("PROBE_H", 620)
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "probe",
			Width:   w,
			Height:  h,
			Content: ui.View(func(c *ui.Context) { ui.Text(c, "probe") }),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
