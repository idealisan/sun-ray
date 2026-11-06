package main

import (
	"strings"
	"unicode"
)

const (
	dnsPresetSystem     = "system"
	dnsPresetAli        = "ali"
	dnsPresetAliDoH     = "ali-doh"
	dnsPresetCloudflare = "cf"
	dnsPresetCFDoH      = "cf-doh"
	dnsPresetCustom     = "custom"
)

var dnsPresetValues = []string{
	dnsPresetSystem, dnsPresetAli, dnsPresetAliDoH,
	dnsPresetCloudflare, dnsPresetCFDoH, dnsPresetCustom,
}

var dnsPresetLabels = []string{
	"系统默认",
	"阿里 DNS（223.5.5.5 / 223.6.6.6）",
	"阿里加密 DNS（DoH）",
	"Cloudflare DNS（1.1.1.1 / 1.0.0.1）",
	"Cloudflare 加密 DNS（DoH）",
	"自定义",
}

// dnsServersFor returns the upstream servers of the chosen preset. An empty
// result means the system resolver is used as-is.
func dnsServersFor(g *globalConfig) []string {
	switch g.DNSPreset {
	case dnsPresetAli:
		return []string{"223.5.5.5", "223.6.6.6"}
	case dnsPresetAliDoH:
		return []string{"https://dns.alidns.com/dns-query"}
	case dnsPresetCloudflare:
		return []string{"1.1.1.1", "1.0.0.1"}
	case dnsPresetCFDoH:
		return []string{"https://cloudflare-dns.com/dns-query"}
	case dnsPresetCustom:
		return splitDNSServers(g.DNSCustom)
	default:
		return nil
	}
}

func splitDNSServers(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || unicode.IsSpace(r)
	})
	if len(fields) == 0 {
		return nil
	}
	servers := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			servers = append(servers, field)
		}
	}
	return servers
}

// buildDNSConfig builds the xray dns section. A localhost server is kept for
// local names with skipFallback, so public queries only use the selected
// upstream while .local/.lan/dotless names still resolve locally.
func buildDNSConfig(g *globalConfig) map[string]any {
	servers := dnsServersFor(g)
	if len(servers) == 0 {
		return nil
	}
	list := []any{map[string]any{"address": "localhost", "skipFallback": true}}
	for _, addr := range servers {
		list = append(list, map[string]any{"address": addr})
	}
	return map[string]any{"servers": list, "queryStrategy": "UseIP"}
}
