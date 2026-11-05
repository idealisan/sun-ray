# --- core.go: sniffing always on, no destination rewrite ---
p = 'core.go'
s = open(p, encoding='utf-8').read()
old = '\tSniffing   bool   `json:"sniffing"`   // sniff connection domains, needed for domain routing\n'
assert old in s, 'sniffing field'
s = s.replace(old, '', 1)

old = 'return globalConfig{ListenPort: "10808", Sniffing: true, LogLevel: "info"}'
new = 'return globalConfig{ListenPort: "10808", LogLevel: "info"}'
assert old in s, 'defaultGlobalConfig'
s = s.replace(old, new, 1)

old = """\tsniffing := map[string]any{"enabled": false, "destOverride": []string{"http", "tls"}}
\tif g.Sniffing {
\t\tsniffing["enabled"] = true
\t}"""
new = """\t// Sniffing is always on: without it the domain routing rules
\t// (geosite) have nothing to match on. routeOnly keeps the sniffed
\t// domain for routing alone and never rewrites the destination, and
\t// metadataOnly avoids waiting for the whole handshake, so the
\t// connection keeps its original address and the added delay stays
\t// negligible.
\tsniffing := map[string]any{
\t\t"enabled":      true,
\t\t"destOverride": []string{"http", "tls", "quic"},
\t\t"metadataOnly": true,
\t\t"routeOnly":    true,
\t}"""
assert old in s, 'sniffing build'
s = s.replace(old, new, 1)
open(p, 'w', encoding='utf-8', newline='\n').write(s)
print('core.go: sniffing always on')

# --- main.go: drop the toggle ---
p = 'main.go'
s = open(p, encoding='utf-8').read()
old = '\t\t\t\tui.Checkbox(c, &g.Sniffing, "流量嗅探（域名分流需要）")\n'
assert old in s, 'sniffing checkbox'
s = s.replace(old, '', 1)
open(p, 'w', encoding='utf-8', newline='\n').write(s)
print('main.go: toggle removed')

# --- speedtest.go: temp core uses the same settings ---
p = 'speedtest.go'
s = open(p, encoding='utf-8').read()
old = 'g := &globalConfig{ListenPort: tempPortString(port), Sniffing: false, LogLevel: "info"}'
new = 'g := &globalConfig{ListenPort: tempPortString(port), LogLevel: "info"}'
assert old in s, 'temp global config'
s = s.replace(old, new, 1)
open(p, 'w', encoding='utf-8', newline='\n').write(s)
print('speedtest.go updated')