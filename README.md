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
go build -o sun-ray.exe .

# 运行测试（headless 渲染，无需窗口）
go test ./...

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

## TUN 模式

TUN 模式接管**全部**流量，不依赖应用是否支持代理——服务器上跑、或不想逐个设置代理的程序（游戏、某些国产软件、容器）都能走代理。

- 全局设置里勾选「启用 TUN」，可调接口名、MTU、网关地址、是否连 IPv6 一起接管
- 需要**管理员/root 权限**：Linux 需要 `/dev/net/tun` 与 `CAP_NET_ADMIN`，Windows 需要管理员，macOS 需要 root。权限不足时界面会直接提示
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

发布新版：

```bash
git tag v0.1.0
git push origin v0.1.0
```

## 当前功能

- 入站：本机 SOCKS5 + HTTP 代理，端口可配，可允许局域网
- 出站协议：vless / vmess / trojan / shadowsocks / freedom，可配 TLS 与 SNI
- 启动 / 停止：直接调用 `core.Instance` 的函数，无子进程
- 实时流量速率（每秒采样 xray stats 计数器）
- 运行日志面板（xray 日志经 `log.RegisterHandler` 回调到 GUI）
- 关闭主窗口后留在菜单栏 / 任务栏托盘，右键菜单可重新打开或退出
- 延迟与速度测试：列表内联显示"延迟"与"速度"两列，工具栏「测试」按钮对选中项（未选中则全部）逐个测量
  - 延迟：经该节点访问 Cloudflare 204 端点的**首字节到达时间**（3 次取中位数，所有协议通用）
  - 速度：经该节点下载 Cloudflare 50MB，3 并发、限时 10 秒
  - 测量走**临时内核 + 随机回环端口**（`127.0.0.1`），不占用也不对外暴露用户配置的端口

## Roadmap

- [ ] 配置持久化（保存/加载服务器列表）
- [ ] 订阅导入
- [ ] 路由规则编辑（分流、黑白名单）
- [ ] 系统代理一键设置（各平台）
- [ ] 更多传输层：WebSocket / gRPC / REALITY
- [ ] 多平台打包分发

## License

各上游项目遵循其自身许可证：MyGo 见 `mygo/LICENSE`，Xray-core 见 `xray-core/LICENSE`。
