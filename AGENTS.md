# AGENTS.md

给在本仓库工作的 AI 代理与协作者的约定。违反下面的规则可能损坏用户的真实数据。

## 绝对禁止：开发 / 测试 / 调试时使用用户目录下的配置文件

**任何时候都不得读写 `~/sunray.json`（以及 `~/sunray.lock`）——那是用户正在使用的真实配置。**

这条是硬性要求，来自一次真实事故：测试点了一个复选框，`view()` 里的自动保存把测试夹具写进了 `~/sunray.json`，用户的服务器列表被替换成默认的空配置。

### 测试

- `TestMain`（`testmain_test.go`）已为**所有**测试把 `configFileOverride` 指向临时文件、把 `SUNRAY_INSTANCE_LOCK` 指向临时路径。新增测试**不要**把它改回空值或真实路径。
- 需要自己的配置文件时，用 `t.TempDir()` 或 `os.MkdirTemp`，并保存/恢复 `configFileOverride`，例如：

  ```go
  old := configFileOverride
  defer func() { configFileOverride = old }()
  configFileOverride = filepath.Join(t.TempDir(), "sunray.json")
  ```

- 断言"配置没有被写"要看临时文件，不要看 `~/sunray.json`。
- `saveConfig` 里有护栏：测试进程（`*.test`）若在 `configFileOverride` 为空时写配置会 **panic**。撞到它说明测试没隔离路径，去修测试，不要移除护栏。
- `a.saver.remember(cfg)` **不能**当作隔离手段：它只让首帧不写，配置一旦变化（点复选框、删除、导入）照样落盘。

### 开发与调试

- 运行程序一律显式传配置路径：

  ```bash
  go run . -config /tmp/sunray-dev.json
  ```

  或旧式的 `--config /tmp/sunray-dev.json`、`-config=/tmp/...`。
- 不要为了"图方便"省略 `-config`，也不要临时改 `configPath()` 的默认值来测试。
- 需要一份可用的配置做手工验证时，把用户配置**复制**到临时路径再用，不要直接在原文件上试。
- 用完的临时配置放在系统临时目录（`t.TempDir()` / `os.MkdirTemp`），不要留在仓库里。

## 配置文件与备份

- 用户明确要求：**不做备份机制**（不生成 `.bak`、不依赖备份文件）。加载失败时应保留原文件、弹窗让用户选择，而不是自动备份或覆盖。
- 加载失败（解析不了、缺少 `servers`）绝不写回原文件：`saver.blocked` 会拦住所有写入，直到用户选择"创建全新配置"。

## 单实例

- `~/Library/Application Support/sun-ray/instance.lock` 是单实例锁，不是日志文件。位于应用数据目录，不放家目录根下。
- 锁文件必须独立于配置文件：保存是 `tmp + rename`，会换 inode，锁在配置本身会失效。
- 释放靠进程退出（`flock` / `LockFileEx`），不需要也不应该手动删锁文件。

## 提交与发布

- 提交信息参考最近 10 条的风格（`feat:` / `fix:` / `chore:` / `docs:`），不要 `Co-authored-by` trailer。
- 功能完成即提交推送，并发 release 触发 CI（版本号在 `mygo.json` 的 `version`）。

## 验证习惯

- 跑测试前后各取一次 `shasum ~/sunray.json`，确认没变。这是本仓库最容易发生的破坏。
