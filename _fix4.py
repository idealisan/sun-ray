import io

# core.go: insert Setenv block (idempotent)
p = 'core.go'
s = open(p, encoding='utf-8').read()
if 'SUNRAY_ALLOW_INSECURE' not in s:
    anchor = '\tinst, err := core.New(coreConf)'
    assert anchor in s, 'anchor missing'
    ins = ('\t// The patched core honors this env in GetTLSConfig (allowInsecure was',
           '\t// removed from the upstream config format).',
           '\tif allowInsecure {',
           '\t\tos.Setenv("SUNRAY_ALLOW_INSECURE", "true")',
           '\t} else {',
           '\t\tos.Setenv("SUNRAY_ALLOW_INSECURE", "false")',
           '\t})
    s = s.replace(anchor, '\n'.join(ins), 1)
    open(p, 'w', encoding='utf-8', newline='\n').write(s)
    print('core.go patched')
else:
    print('core.go already patched')

# main.go: pass flag
p = 'main.go'
s = open(p, encoding='utf-8').read()
if 'a.cfg.AllowInsecure, a.addLog' not in s:
    s = s.replace('if err := a.core.Start(cfgJSON, a.addLog); err != nil {',
                  'if err := a.core.Start(cfgJSON, a.cfg.AllowInsecure, a.addLog); err != nil {', 1)
    open(p, 'w', encoding='utf-8', newline='\n').write(s)
    print('main.go patched')
else:
    print('main.go already patched')

# main_test.go: adjust call
p = 'main_test.go'
s = open(p, encoding='utf-8').read()
if 'a.core.Start(data, false, func(string) {})' not in s:
    s = s.replace('a.core.Start(data, func(string) {})', 'a.core.Start(data, false, func(string) {})', 1)
    open(p, 'w', encoding='utf-8', newline='\n').write(s)
    print('test patched')
else:
    print('test already patched')