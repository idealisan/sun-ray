# Sun Ray

一个简单的 [Xray-core](https://github.com/xtls/xray-core) GUI 客户端，使用 [MyGo](https://github.com/egoist/mygo) 的原生 UI（Native UI）绘制界面。

**单进程架构**：Xray-core 以 Go 库的形式直接嵌入 GUI 程序，两者编译为同一个二进制，通过函数调用通信 —— 不经过命令行、不起子进程。

## 架构

```
┌─────────────────── 单一进程 / 单一二进制 ───────────────────┐
│                                                            │
│  GUI（mygo 原生 UI）        xray-core（库，本进程内）        │
│  ┌──────────────────┐      ┌───────────────────────────┐   │
│  │ 配置表单          │──┐   │ core.New(config)          │   │
│  │ 启动 / 停止按钮    │  ├──►│ instance.Start()/Close()  │   │
│  │ 流量速率显示       │  │   │ SOCKS/HTTP 入站 + 出站     │   │
│  │ 日志面板          │◄─┘   │                           │   │
│  └──────────────────┘      └───────────────────────────┘   │
│        ▲ 函数调用通信（无命令行、无子进程）                    │
│        ├── 启动/停止：core.Instance 的方法调用               │
│        ├── 日志：log.RegisterHandler 回调进 GUI             │
│        └── 流量：stats.Manager 计数器每秒采样               │
└────────────────────────────────────────────────────────────┘
```

关键依赖关系：

| 组件 | 说明 |
| --- | --- |
| [MyGo](https://github.com/egoist/mygo) | 桌面应用框架：纯 Go、无 cgo，原生 GPU 绘制 UI，无 WebView、无 HTML/JS |
| [Xray-core](https://github.com/xtls/xray-core) | 以 Go 库嵌入（`core.New`），通过 `_ "main/distro/all"` 注册全部协议 |

## 仓库结构

两个上游项目以 git submodule 引入，本仓库的应用代码通过 Go `replace` 指令直接引用本地子模块：

```
sun-ray/
├── main.go       # GUI：mygo 原生 UI（窗口、表单、按钮、日志）
├── core.go       # xray-core 进程内封装：配置生成、启停、日志、流量
├── protocols.go  # 各协议的出站/传输层/安全层配置生成
├── dns.go        # DNS 预设（系统 / 阿里 / Cloudflare / DoH / 自定义）
├── sysproxy*.go  # 系统代理（手动 / PAC）各平台实现
├── speedtest.go  # 测速的客户端逻辑（延迟 + 下载）
├── gui_speedtest.go # 测速的临时内核调度与结果展示
├── subscription.go # 订阅拉取、分享链接解析与节点导入
├── tun*.go       # TUN 模式与特权 helper
├── main_test.go  # headless UI 测试 + 内核启停冒烟测试
├── integration/  # 按协议端到端测试（本地上游 + curl）
├── go.mod        # replace ./mygo、./xray-core
├── mygo/         # 子模块
└── xray-core/    # 子模块
```

## 开发

要求：Go 1.27+（旧版本会自动下载工具链）。

```bash
git clone --recurse-submodules <本仓库地址>
cd sun-ray
# 若已克隆过：
git submodule update --init --recursive

# 构建单二进制（GUI + 内核一体）
# Windows 必须加 -H windowsgui：Go 默认链接控制台子系统，
# 双击时会弹出一个控制台窗口；这个链接参数把它改成 GUI 子系统。
go build -ldflags "-H windowsgui" -o sun-ray.exe .

# 运行测试（headless 渲染，无需窗口）
go test ./...

# 开发/调试时用独立的配置文件，避免覆盖 ~/sunray.json
go run . -config /tmp/sunray-dev.json

# 按协议端到端测试：本地启动上游，curl 通过混合入站访问本地 origin
go test -tags integration -count=1 -v -timeout 10m ./integration
```

### macOS 打包

普通用户不应该在终端里启动程序，所以 macOS 用 MyGo 的打包命令生成可双击的 `.app`：

```bash
go run github.com/egoist/mygo/cmd/mygo build -platform darwin/arm64 -o dist
```

产物在 `dist/darwin-arm64/`：`Sun Ray.app`（以及 `.dmg`）。应用名、Bundle ID、版本和图标在 `mygo.json` 中配置。图标源文件是 `assets/icon.svg` 与 `assets/tray-*.svg`，PNG 由 `scripts/render-icons.sh` 用 `sips` 渲染。

没有配置 Developer ID 时是 ad-hoc 签名，从网上下载后首次打开需要右键 → 打开（或到「系统设置 → 隐私与安全性」里允许）。

Windows 直接双击编译出的 `.exe` 即可；Linux 仍使用普通二进制。

## 内嵌 geo 资产

路由预设需要 geoip/geosite 数据。本项目把它们（只提取 \`CN\` 与 \`category-ads-all\`，共约 400KB）直接嵌入二进制，并通过 \`filesystem.AssetProvider\` 从内存提供给 xray-core——单文件分发，用户机器零额外文件。

上游 xray-core 尚无该注入点，仓库附带一个最小补丁 \`patches/xray-core-embedded-assets.patch\`（约 10 行），本地与 CI 在 checkout 子模块后应用：

\`\`\`bash
git -C xray-core apply ../patches/xray-core-embedded-assets.patch
\`\`\`

若更新子模块，需确认补丁仍可应用（必要时重新生成）。

### 每周自动更新 geo 数据

`.github/workflows/geo-update.yml` 每周二（UTC 03:00）取上游最新数据并重建 `assets/geoip.dat` 与 `assets/geosite.dat`：

- `geoip.dat` 来自 `v2fly/geoip` 每次 release 的 `cn.dat`（本身只含 CN 段）
- `geosite.dat` 来自 `v2fly/domain-list-community` 的 `dlc.dat`，按 protobuf 线格式只保留 `CN` 与 `CATEGORY-ADS-ALL` 两条，维持约 400KB

更新脚本是 `scripts/update-geo-assets.py`（纯 Python 标准库，无 pip 依赖），本地也可手动运行：

```bash
python3 scripts/update-geo-assets.py
```

数据有变化时，workflow 把新文件提交到 master，打 `v<版本>-<7位commit hash>-<年月日>` 标签（如 `v0.19.0-bf79793-20261110`），再触发 `build.yml` 三平台编译并发布 GitHub Release。程序里 `-version` / 日志 / 界面的版本串采用同一格式（去掉开头的 `v`）。数据未变化则整周跳过，不产生多余提交与 Release。

## TUN 模式

TUN 模式接管**全部**流量，不依赖应用是否支持代理——服务器上跑、或不想逐个设置代理的程序（游戏、某些国产软件、容器）都能走代理。

- 全局设置里勾选「启用 TUN」，可调接口名、MTU、网关地址、是否连 IPv6 一起接管
- **主界面不需要以管理员身份运行**：启动 TUN 时，同一个可执行文件会以隐藏参数 `-tun-helper` 启动一个临时特权进程，由系统弹出授权框（macOS 用 `osascript`，Linux 用 `pkexec`/polkit，Windows 用 UAC）。取消授权就回到普通模式
- 特权 helper 只运行「TUN 入站 → `127.0.0.1` SOCKS 出站」，**不读取服务器地址、UUID、密码**；主进程把代理和直连出站绑定到物理网卡（`sockopt.interface`），所以不会自己绕回 TUN
- 只改服务器、路由、日志等设置时 helper 保持运行，只重启主内核；改 TUN 接口名、MTU、网关、IPv6 或监听端口时才重新请求授权
- 开启 TUN 后**不再修改系统代理**（TUN 已接管全部流量），本地 SOCKS/HTTP 端口仍然保留，方便排查
- xray 会用自己的出站接口避开 TUN（`autoOutboundsInterface`），不会把自己绕进死循环
- **不做系统 DNS 接管**（`autoSystemDnsToGateway`）：它在 Linux 上依赖 systemd-resolved 且条件苛刻，容易出现解析死锁。DNS 查询仍走系统解析器（可能被运营商看到域名）
- TUN 是 L3 代理而不是真 VPN：连到不存在的主机也会先握手成功再断开，ICMP Echo 由本地生成
- 崩溃时可能残留路由：Linux 上可 `ip route del default dev sunray0` 清理

## 无界面版（Linux）

`sun-ray-nogui-linux-amd64` / `-arm64` 是同一份代码的**无界面构建**：跑的是同一个 xray-core、读同一份 `sunray.json`，只是没有界面。它为「在电脑上用 GUI 配好，拷到服务器上跑」的场景而生：

```bash
# 在 GUI 机器上配好，把 ~/sunray.json 拷到服务器
scp ~/sunray.json server:/etc/sunray.json

# 在服务器上跑
sun-ray-nogui-linux-amd64 -config /etc/sunray.json
```

- 配置格式与 GUI 完全相同（同一个 `sunray.json`），服务器上也**不需要额外的 .dat 文件**：geo 资产已经内嵌在二进制里
- 日志直接打到标准输出（systemd 下就是 journal），`SIGINT`/`SIGTERM` 会优雅停止内核
- `-test` 跑一次延迟与速度测量就退出，`-version` 打印版本
- `-test-mode` 启动后打印 `SUNRAY_READY host:port` 并等待信号，供集成测试使用
- 无界面版**不碰系统代理与环境变量**（那些是给图形机上的浏览器/终端用的设置）

systemd 单元示例：

```ini
[Unit]
Description=Sun Ray
After=network-online.target

[Service]
ExecStart=/usr/local/bin/sun-ray-nogui-linux-amd64 -config /etc/sunray.json
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## CI 与发布

GitHub Actions（`.github/workflows/build.yml`）：

- push 到 master / PR：三平台（Linux amd64 / Windows amd64 / macOS arm64）编译 + Linux 上跑测试，产物存为 workflow artifact
- 推送 `v*` 标签：三平台编译后自动创建 GitHub Release，并把二进制挂到 Release 资产上
- 每周二定时刷新内嵌 geo 数据，提交后打带日期的标签并自动走正式 Release（见「每周自动更新 geo 数据」）

发布新版：

```bash
git tag v0.1.0
git push origin v0.1.0
```

## 当前功能

### 入站与出站

- 入站：SOCKS5 与 HTTP 共用一个混合端口（自动识别客户端协议），端口可配，可允许局域网
- 出站协议：vless / vmess / trojan / shadowsocks / wireguard / hysteria2 / masque / freedom
- 传输层：tcp / websocket / gRPC / httpupgrade；安全层：none / TLS / REALITY
- 启动 / 停止：直接调用 `core.Instance` 的函数，无子进程
- 实时流量速率（每秒采样 xray stats 计数器）

### 系统代理与路由

- 系统代理一键设置：手动模式 / PAC 模式 / 不修改，退出或切换时自动清理
- 代理环境变量：「设置代理环境变量」复选框仅 Windows 可用（写入当前用户的环境变量）；macOS 和 Linux 上该复选框禁用，因为系统无法替已打开的终端设置环境变量。任何平台都可点「复制环境变量命令」按钮，把能在当前终端里粘贴执行的命令复制到剪贴板——macOS / Linux 是 shell 的 `export`，Windows 是 PowerShell 的 `$env:`
- 路由预设：绕过大陆（geosite:cn / geoip:cn 直连）、屏蔽广告（geosite:category-ads-all）
- 域名解析策略：默认关闭，即不解析列表之外的域名（AsIs）——国内站靠域名规则直连，翻墙域名没有本地 DNS 开销、不受 DNS 污染影响。勾选「解析列表外的域名，并根据 IP 进一步路由」后切换为 IPIfNonMatch，分流判定更全面，但每个翻墙域名都要先做一次本地 DNS 查询
- DNS 预设：系统默认 / 阿里 DNS / 阿里 DoH / Cloudflare / Cloudflare DoH / 自定义。这里的 DNS 只为分流判定服务，翻墙流量的域名始终由远端服务器解析；上面的开关关闭时这一栏会整体禁用，因为此时不生效

### 服务器列表与订阅

- 服务器管理：新增、编辑、复制、删除，支持多选批量操作与列表排序
- 分享链接导入 / 导出：
  - 右键菜单「复制为 URL」把节点导出为分享链接；多选时（且右键行在选中集内）一次导出整个选中集，每行一条
  - 工具栏「导入」一键读取剪贴板，支持明文分享链接（单行 / 多行）和整段 Base64 编码的订阅内容；每个节点作为未分类的新节点追加，重复的也直接追加
  - 有社区标准的协议（VLESS / VMess / Trojan / Shadowsocks / Hysteria2 / Socks / HTTP）按通行格式导出，可直接粘到 v2rayN、sing-box、Clash 等客户端；没有标准链接的协议（WireGuard / Masque / Freedom / Blackhole / DNS / Loopback）用 `sunray://` + Base64URL(JSON) 私有格式，保证在 Sun Ray 内复制粘贴不丢字段
  - 注意：分享链接和 `sunray://` 里都含明文凭据（UUID / 密码 / 私钥），Base64 只是编码不是加密，转发前请当成密码对待
- 分组筛选：按订阅分组或未分组过滤，工具栏显示计数
- 「设为当前」「编辑」作用于光标所在行；批量操作与测速跟随选中行 ∩ 当前筛选，永远与列表 UI 一致
- 订阅管理：新增、编辑、复制、删除和批量删除；从订阅 URL 拉取 Base64 或明文节点链接（VLESS / VMess / Trojan / Shadowsocks / Hysteria2），更新时保留同名节点的测量结果
- 关闭主窗口后留在菜单栏 / 任务栏托盘，右键菜单可重新打开或退出
- 退出方式：托盘菜单的「退出」是**完全退出**（代理停止、托盘图标消失）；macOS 上的 **Command+Q 和 Dock 栏的「退出」只把窗口收起到状态栏**，代理继续运行、状态栏图标保留（系统无法区分这两者，所以它们行为一致）

### 测速与日志

- 延迟与速度测试：列表内联显示"延迟"与"速度"两列
  - 延迟：经该节点访问 Cloudflare 204 端点的**首字节到达时间**（3 次取中位数，所有协议通用）
  - 速度：经该节点下载 Cloudflare 50MB，3 并发、限时 10 秒
  - 测量走**临时内核 + 随机回环端口**（`127.0.0.1`），不占用也不对外暴露用户配置的端口
  - 右键菜单的「测试」在多选时测整个选中集；测速目标永远不超过当前筛选后可见的列表
- 运行日志面板：级别跟随全局设置的日志级别，保留最近 200 行，避免无限增长

### 配置文件

配置保存在 `~/sunray.json`。**加载失败时绝不覆盖原文件**：文件存在但解析不了（内容截断、格式不对、缺少 servers 字段），应用会弹出模态提示，说明校验失败的原因，让你选择：

- **退出**——文件原样保留，你可以自己修好它再启动
- **创建全新配置**——只有选了这个，应用才会用一份全新的配置替换它

在做出选择之前，应用不会向这个文件写入任何东西，代理也不会启动。写入失败（比如磁盘满了、目录没有写权限）会弹一个警告，告知这次没有保存成功。

### 单实例

**全局只允许运行一个 Sun Ray**：GUI、无界面版、打包的 `.app` 和开发时的 `go run .` 共用同一把锁，因为读写同一份 `~/sunray.json`、抢同一批端口，两个实例会互相覆盖状态。锁放在 `~/sunray.lock`：macOS / Linux 用 `flock`，Windows 用 `LockFileEx`，进程崩溃或退出都由系统自动释放，不会留下需要手动清理的锁。

第二次启动不会打开新窗口，而是让已运行的实例把主窗口显示出来，然后自己退出。`-test`、`-test-mode`（集成测试）和 `-version` 这类一次性命令不参与加锁，否则并行的集成测试会互相锁死。

## Roadmap

- [x] 配置持久化（`~/sunray.json` 保存/加载服务器列表）
- [x] 订阅导入（常见分享链接）
- [x] 系统代理一键设置（macOS / Windows / Linux，手动与 PAC）
- [x] 更多传输层与安全层：WebSocket / gRPC / HTTPUpgrade / REALITY
- [x] 多平台打包分发（CI 三平台 + GitHub Release）
- [ ] 路由规则编辑（自定义分流、黑白名单）

## License

各上游项目遵循其自身许可证：MyGo 见 `mygo/LICENSE`，Xray-core 见 `xray-core/LICENSE`。
