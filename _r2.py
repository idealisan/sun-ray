# --- speedtest.go: store the result ---
p = 'speedtest.go'
s = open(p, encoding='utf-8').read()
old = """\t\t\tst := &testState{latency: latency, speed: speed}
\t\t\tif err != nil {
\t\t\t\tst.failed = true
\t\t\t\tst.err = err.Error()
\t\t\t}"""
new = """\t\t\tst := &testState{latency: latency, speed: speed}
\t\t\tif err != nil {
\t\t\t\tst.failed = true
\t\t\t\tst.err = err.Error()
\t\t\t} else {
\t\t\t\t// Keep the measurement, so the list still shows it after a
\t\t\t\t// restart and the servers can be compared.
\t\t\t\tif a.cfg.Results == nil {
\t\t\t\t\ta.cfg.Results = map[string]serverResult{}
\t\t\t\t}
\t\t\t\ta.cfg.Results[s.ID] = serverResult{
\t\t\t\t\tLatencyMs: int(latency.Milliseconds()),
\t\t\t\t\tSpeedBps:  speed,
\t\t\t\t\tTestedAt:  time.Now().Unix(),
\t\t\t\t}
\t\t\t}"""
assert old in s, 'test loop anchor'
s = s.replace(old, new, 1)
open(p, 'w', encoding='utf-8', newline='\n').write(s)
print('speedtest.go updated')

# --- main.go: cells read the stored result ---
p = 'main.go'
s = open(p, encoding='utf-8').read()
old = """\t\t\t\tcase 5:
\t\t\t\t\tst := a.testStateOf(s.ID)
\t\t\t\t\tswitch {
\t\t\t\t\tcase st == nil:
\t\t\t\t\t\tui.Text(c, "-")
\t\t\t\t\tcase st.running:
\t\t\t\t\t\tui.Text(c, "测试中…")
\t\t\t\t\tcase st.failed:
\t\t\t\t\t\tui.Text(c, "失败")
\t\t\t\t\tdefault:
\t\t\t\t\t\tui.Text(c, formatLatency(st.latency))
\t\t\t\t\t}
\t\t\t\tcase 6:
\t\t\t\t\tst := a.testStateOf(s.ID)
\t\t\t\t\tif st == nil || st.failed || st.speed <= 0 {
\t\t\t\t\t\tui.Text(c, "-")
\t\t\t\t\t} else {
\t\t\t\t\t\tui.Text(c, "↓ "+formatSpeed(st.speed))
\t\t\t\t\t}"""
new = """\t\t\t\tcase 5:
\t\t\t\t\tst := a.testStateOf(s.ID)
\t\t\t\t\tswitch {
\t\t\t\t\tcase st != nil && st.running:
\t\t\t\t\t\tui.Text(c, "测试中…")
\t\t\t\t\tcase st != nil && st.failed:
\t\t\t\t\t\tui.Text(c, "失败")
\t\t\t\t\tdefault:
\t\t\t\t\t\tif r, ok := a.cfg.Results[s.ID]; ok {
\t\t\t\t\t\t\tui.Text(c, formatLatency(time.Duration(r.LatencyMs)*time.Millisecond))
\t\t\t\t\t\t} else {
\t\t\t\t\t\t\tui.Text(c, "-")
\t\t\t\t\t\t}
\t\t\t\t\t}
\t\t\t\tcase 6:
\t\t\t\t\tst := a.testStateOf(s.ID)
\t\t\t\t\tswitch {
\t\t\t\t\tcase st != nil && st.running:
\t\t\t\t\t\tui.Text(c, "测试中…")
\t\t\t\t\tcase st != nil && st.failed:
\t\t\t\t\t\tui.Text(c, "-")
\t\t\t\t\tdefault:
\t\t\t\t\t\tif r, ok := a.cfg.Results[s.ID]; ok && r.SpeedBps > 0 {
\t\t\t\t\t\t\tui.Text(c, "↓ "+formatSpeed(r.SpeedBps))
\t\t\t\t\t\t} else {
\t\t\t\t\t\t\tui.Text(c, "-")
\t\t\t\t\t\t}
\t\t\t\t\t}"""
assert old in s, 'cells anchor'
s = s.replace(old, new, 1)

# updateServer: a changed endpoint makes the stored result stale
old = """\t\t\ts.ID = id
\t\t\ta.cfg.Servers[i] = s
\t\t\treturn"""
new = """\t\t\told := a.cfg.Servers[i]
\t\t\ts.ID = id
\t\t\ta.cfg.Servers[i] = s
\t\t\t// A changed endpoint makes the stored measurement stale.
\t\t\tif old.Address != s.Address || old.Port != s.Port || old.Protocol != s.Protocol {
\t\t\t\tdelete(a.cfg.Results, id)
\t\t\t}
\t\t\treturn"""
assert old in s, 'updateServer anchor'
s = s.replace(old, new, 1)

# deleteServers: drop the results of the removed servers
old = """\tfor _, id := range ids {
\t\tdrop[id] = true
\t}"""
new = """\tfor _, id := range ids {
\t\tdrop[id] = true
\t\tdelete(a.cfg.Results, id)
\t}"""
assert old in s, 'deleteServers anchor'
s = s.replace(old, new, 1)

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print('main.go updated')