package main

import (
	"testing"
)

// Every protocol the app offers must be registered in the core: the core
// refuses to start with a protocol it does not know, so this catches a
// missing registration.
func TestCoreStartsForEveryProtocol(t *testing.T) {
	for _, proto := range protocolList {
		c := defaultServerConfig()
		c.Protocol = proto
		c.Address, c.Port, c.Secret = "127.0.0.1", "1", "testsecret"
		c.PeerKey = "peerpublickey"
		c.Security = "tls"
		c.SNI = "example.com"
		g := defaultGlobalConfig()
		g.ListenPort = "39450"
		data, err := c.buildJSON(&g)
		if err != nil {
			t.Errorf("protocol %s: build: %v", proto, err)
			continue
		}
		inst, err := startInstance(data)
		if err != nil {
			t.Errorf("protocol %s: start: %v", proto, err)
			continue
		}
		inst.Close()
	}
}
