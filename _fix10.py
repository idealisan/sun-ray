p = 'main.go'
s = open(p, encoding='utf-8').read()

old_switch = ('switch col {\n'
\t\t\t\tcase 0:\n'
\t\t\t\t\tui.Text(c, s.displayName()).SingleLine()\n'
\t\t\t\tcase 1:\n'
\t\t\t\t\tui.Text(c, s.Protocol)\n'
\t\t\t\tcase 2:\n'
\t\t\t\t\tui.Text(c, s.Address).SingleLine()\n'
\t\t\t\tcase 3:\n'
\t\t\t\t\tui.Text(c, s.Port)\n'
\t\t\t\tcase 4:\n'
\t\t\t\t\tif s.ID == a.cfg.ActiveID {\n'
\t\t\t\t\t\tui.Text(c, "●").TextColor(t.Accent)\n'
\t\t\t\t\t}\n'
\t\t\t\t')

new_switch = ('switch col {\n'
\t\t\t\tcase 0:\n'
\t\t\t\t\tif s.ID == a.cfg.ActiveID {\n'
\t\t\t\t\t\tui.Text(c, "✓").TextColor(t.Accent)\n'
\t\t\t\t\t}\n'
\t\t\t\tcase 1:\n'
\t\t\t\t\tui.Text(c, s.displayName()).SingleLine()\n'
\t\t\t\tcase 2:\n'
\t\t\t\t\tui.Text(c, s.Protocol)\n'
\t\t\t\tcase 3:\n'
\t\t\t\t\tui.Text(c, s.Address).SingleLine()\n'
\t\t\t\tcase 4:\n'
\t\t\t\t\tui.Text(c, s.Port)\n'
\t\t\t\t')

assert old_switch in s, 'switch not found'
s = s.replace(old_switch, new_switch, 1)

old_cols = ('{Title: "名称", ID: "name", Sortable: true},\n'
\t\t\t{Title: "协议", ID: "protocol", Width: 100, Sortable: true},\n'
\t\t\t{Title: "地址", ID: "address", Sortable: true},\n'
\t\t\t{Title: "端口", ID: "port", Width: 80, Align: ui.End, Sortable: true},\n'
\t\t\t{Title: "当前", ID: "active", Width: 60, Align: ui.Center},\n')
new_cols = ('{Title: "当前", ID: "active", Width: 60, Align: ui.Center},\n'
\t\t\t{Title: "名称", ID: "name", Sortable: true},\n'
\t\t\t{Title: "协议", ID: "protocol", Width: 100, Sortable: true},\n'
\t\t\t{Title: "地址", ID: "address", Sortable: true},\n'
\t\t\t{Title: "端口", ID: "port", Width: 80, Align: ui.End, Sortable: true},\n')
assert old_cols in s, 'cols not found'
s = s.replace(old_cols, new_cols, 1)

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print('columns reordered, marker switched to check')