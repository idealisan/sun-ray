// Command probe-ui measures what the mygo UI alone costs: it opens one
// window and nothing else, so a build of it shows the size and the
// memory the UI framework needs on its own.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "probe",
			Width:   880,
			Height:  620,
			Content: ui.View(func(c *ui.Context) { ui.Text(c, "probe") }),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
