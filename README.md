# printed-ray

一个基于 [Xray-core](https://github.com/xtls/xray-core) 的跨平台 GUI 客户端，使用 [MyGo](https://github.com/egoist/mygo) 作为桌面应用框架。

## 项目目标

把 Xray-core（代理内核）与 MyGo（GUI 壳）组合起来，做一个轻量、单二进制、低内存占用的 Xray 图形客户端。

## 技术选型

| 组件 | 说明 |
| --- | --- |
| [MyGo](https://github.com/egoist/mygo) | 桌面应用框架：纯 Go、无 cgo，支持 WebView 前端（WebView2/WebKitGTK/WKWebView）或 Go 原生 UI，前后端通过类型化 IPC 通信 |
| [Xray-core](https://github.com/xtls/xray-core) | 代理内核，以子进程方式启动，通过配置文件与本地 API 控制 |

架构思路：GUI 进程负责界面与配置管理，Xray-core 作为独立子进程运行，GUI 生成配置文件、启动/停止/监控内核进程，并通过 Xray 的 gRPC API 观测流量。

## 仓库结构

本仓库通过 git submodule 引用两个上游项目：

```
printed-ray/
├── mygo/        # MyGo 框架（子模块）
├── xray-core/   # Xray 内核（子模块）
└── (后续在此添加应用代码)
```

> 子模块默认是浅克隆（depth 1）。上游更新后，进入子模块目录 `git fetch --unshallow` 可获取完整历史。

## 快速开始

克隆本仓库（含子模块）：

```bash
git clone --recurse-submodules <本仓库地址>
cd printed-ray
# 若已克隆过：
git submodule update --init --recursive
```

构建 Xray-core（需要 Go 1.24+）：

```bash
cd xray-core
go build -o xray ./main
```

开发环境要求：

- Go 1.24+
- Node.js / Bun（若前端走 WebView 方案）
- 平台依赖：Windows 使用 WebView2（系统自带）；Linux 需要 WebKitGTK 开发库

## Roadmap

- [ ] 搭建 MyGo 应用骨架
- [ ] 集成 Xray-core 子进程管理（启动 / 停止 / 崩溃重启）
- [ ] 配置文件生成（入站 / 出站 / 路由规则）
- [ ] 节点管理与订阅导入
- [ ] 流量统计（Xray gRPC API）
- [ ] 系统代理设置（各平台）
- [ ] 多平台打包分发

## License

各上游项目遵循其自身许可证：MyGo 见 `mygo/LICENSE`，Xray-core 见 `xray-core/LICENSE`。
