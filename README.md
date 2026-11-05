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
