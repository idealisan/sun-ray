import io

# 1) protocols.go: drop allowInsecure from tls settings (xray removed the feature)
p = 'protocols.go'
s = open(p, encoding='utf-8').read()
s = s.replace('tls := map[string]any{"serverName": sni, "allowInsecure": c.AllowInsecure}',
              'tls := map[string]any{"serverName": sni}')
open(p, 'w', encoding='utf-8', newline='\n').write(s)

# 2) core.go: Start gains allowInsecure; env var drives the patched core
p = 'core.go'
s = open(p, encoding='utf-8').read()
s = s.replace('"encoding/json"\n\t"errors"', '"encoding/json"\n\t"errors"\n\t"os"', 1)
s = s.replace('func (x *xrayCore) Start(cfgJSON []byte, onLog func(string)) error {',
              'func (x *xrayCore) Start(cfgJSON []byte, allowInsecure bool, onLog func(string)) error {', 1)
anchor = '\tinst, err := core.New(coreConf)'
ins = ('\t// The patched core honors this env in GetTLSConfig (allowInsecure was',
       '\t// removed from the upstream config format).',
       '\tif allowInsecure {',
       '\t\tos.Setenv("SUNRAY_ALLOW_INSECURE", "true")',
       '\t} else {',
       '\t\tos.Setenv("SUNRAY_ALLOW_INSECURE", "false")',
       '\t}',
       anchor)
s = s.replace(anchor, ins, 1)
open(p, 'w', encoding='utf-8', newline='\n').write(s)

# 3) main.go: pass the flag
p = 'main.go'
s = open(p, encoding='utf-8').read()
s = s.replace('if err := a.core.Start(cfgJSON, a.addLog); err != nil {',
              'if err := a.core.Start(cfgJSON, a.cfg.AllowInsecure, a.addLog); err != nil {', 1)
open(p, 'w', encoding='utf-8', newline='\n').write(s)

# 4) main_test.go: adjust two call sites
p = 'main_test.go'
s = open(p, encoding='utf-8').read()
s = s.replace('a.core.Start(data, func(string) {})', 'a.core.Start(data, false, func(string) {})', 1)
open(p, 'w', encoding='utf-8', newline='\n').write(s)

print('all edits done')