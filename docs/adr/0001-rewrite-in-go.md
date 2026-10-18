---
status: accepted
---

# Muster 用 Go 重写，不在现有 Node 代码上演进

品牌升级为 Muster 后，第一版是凭据登记、额度总览和按凭据并排启动 Claude Code，以后还要在本地做一个小的池化代理，装 AsterGate 管不到的上游。我们决定用 Go 重写，理由有两条：一是新机器只需下载一个二进制就能恢复环境，不必先装 Node；二是本地池化需要的换号、会话粘性和额度窗口记录，AsterGate 里已经有现成的 Go 实现可以复用。

性能不是理由。实测 Node 冷启动约 50ms、空闲内存约 32MB；对一个用户、几个实例的流式转发来说，代理开销被上游延迟完全淹没。

## Considered Options

- **在 Node 上演进**：能留下锁、原子写、启动时的环境变量剥离、Windows 启动细节和 Web 服务安全防护，约一千行带测试的基础设施；`node:sqlite` 可以零依赖读取 Cursor 的登录状态。没有选它，是因为 profile 模型要退役、前端要重写，能留下的代码不多，而上面两条好处它都给不了。

## Consequences

- CONTRIBUTING 里"零依赖、只用 Node 标准库"的规则要改写为"产物是单个静态二进制；每引入一个依赖都要单独说明理由"。第一个依赖是 `modernc.org/sqlite`（纯 Go，不需要 cgo），用来只读打开 Cursor 的 `state.vscdb`。
- 现有 Node 实现在 Windows 上踩过的坑要在 Go 里重新处理，旧代码是参考：`.cmd` shim 要经 `cmd.exe` 启动、`where` 返回结果的筛选（`src/launch.mjs`），rename 遇到 EPERM 时重试，锁文件用 `link(2)` 原子创建（`src/store.mjs`）。
