// Command probe-core measures what the xray-core library alone costs: it
// starts one instance with a freedom outbound, the same way the app
// does, and nothing else.
package main

import (
	"log"
	"strings"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
)

func main() {
	pb, err := serial.DecodeJSONConfig(strings.NewReader(`{"outbounds":[{"protocol":"freedom"}]}`))
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := pb.Build()
	if err != nil {
		log.Fatal(err)
	}
	inst, err := core.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := inst.Start(); err != nil {
		log.Fatal(err)
	}
	select {}
}
