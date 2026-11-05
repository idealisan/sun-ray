# 开发规范

## 代码结构

| 文件 | 职责 |
| --- | --- |
| `core.go` | 配置数据模型、生成 xray JSON、内嵌内核的启停 / 日志 / 流量 |
| `protocols.go` | 协议注册表：每个出站协议的 UI 字段与 settings 构建 |
| `main.go` | MyGo 原生 UI：主窗口、服务器列表、编辑器窗口 |
| `persistence.go` | `~/sunray.json` 的读写与防抖保存 |
| `assets.go` | 内嵌 geo 资产 |

新增出站协议时，只在 `protocols.go` 的 `protocolRegistry` 里加一项；UI 与配置构建不需要改动。

## 构建与测试

```bash
export GOPROXY=https://goproxy.cn,direct   # 网络受限时
go build -o sun-ray .
go test ./...
```

提交前至少跑一遍 `gofmt -w *.go`、`go vet ./...`、`go test ./...`。

按协议的端到端测试在 `integration/`，用 `integration` build tag 隔离：

```bash
go test -tags integration -count=1 -v -timeout 10m ./integration
```

它会构建 `-tags nogui` 的无界面二进制，在本机为每个协议启动一个 xray-core 上游，再用 `curl` 通过混合入站访问本地 HTTP origin。上游统一用 freedom 的 `redirect` 指到 origin，所以不需要公网。无界面进程用 `-test-mode` 启动，打印 `SUNRAY_READY host:port` 后等待信号。新增协议时在 `integration/integration_test.go` 的用例表里加一项即可。

macOS 的 `.app` 用 MyGo 的打包命令生成，配置在 `mygo.json`：

```bash
go run github.com/egoist/mygo/cmd/mygo build -platform darwin/arm64 -o dist
```

图标源文件是 `assets/*.svg`；改动后用 `scripts/render-icons.sh`（macOS `sips`）重新生成 PNG，再打包。

## 配置持久化

配置保存在 `~/sunray.json`，结构为：

```json
{
  "global": { "listenPort": "10808", "allowLAN": false, "sniffing": true, "logLevel": "info", "routeCN": false, "routeAds": false },
  "servers": [ { "id": "…", "name": "…", "protocol": "vless", "address": "…", "port": "443" } ],
  "activeId": "…"
}
```

- `global` 是全局设置，所有服务器共享；`servers` 只存每条服务器的出站字段。
- 不做旧格式迁移。读到旧格式或损坏文件时回退到默认配置，并且不会在启动时立即覆盖磁盘上的旧文件。
- 测试里构造 `app` 后调用 `a.saver.remember(a.cfg)`，避免测试写用户的 `~/sunray.json`。

## xray-core 子模块与补丁

应用依赖 `patches/xray-core-embedded-assets.patch`（内嵌 geo 资产 + `allowInsecure` 支持）。

```bash
git submodule update --init --recursive
git -C xray-core apply ../patches/xray-core-embedded-assets.patch
```

- 修改子模块后要重新生成补丁，并确认它能对干净的上游提交正向应用。
- 不要提交子模块指针的本地改动；补丁才是仓库里的事实来源。
