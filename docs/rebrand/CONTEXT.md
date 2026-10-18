# Muster：领域词汇

> 状态：讨论稿，随 `vision.md` 的讨论更新。代码里仍是 profile 模型；开始实现时本文件移到仓库根目录。

统一登记多来源的凭据，一目了然地看额度，并按凭据启动（单个或并排）Claude Code 实例。

## 登记

**供应商（Provider）**：
一个上游服务，例如官方订阅、某家中转、AsterGate。决定连到哪里、怎么认证、怎么查额度。
_Avoid_: 渠道、平台、profile

**凭据（Credential）**：
供应商下的一个 token 或一个订阅账号的登录；一个**供应商**有多个**凭据**。它决定能用哪些模型（例如 AsterGate 里分到 Codex 池的 key 只能用 Codex 模型），并带着自己的模型映射（opus / sonnet / haiku 等别名各指向哪个模型）。直连的凭据对应一个**订阅账号**；网关的凭据（AsterGate key）能用到它池子里的若干个**订阅账号**。
_Avoid_: token（与用量的 token 混淆）、key、渠道

**订阅账号（Subscription account）**：
一个有套餐（如 Max、Pro）的上游账号，**额度**归它所有。同一个订阅账号可能被直连凭据用到，也可能在 AsterGate 的池子里被多个 key 用到，总览里只算一次。也可以只为了看额度而登记，不用于任何**启动**（例如 Grok、Cursor、Codex 账号）。
_Avoid_: 账号（不加限定时）、号

**本机登录（Local login）**：
某个平台自己的客户端在这台机器上的登录状态（例如 Codex CLI、Cursor、Grok CLI 的登录）。只用于读取对应**订阅账号**的**额度**，只读，从不刷新或改写；所以一台机器只能看到在本机登录过的账号。
_Avoid_: 授权、会话

## 启动

**启动（Launch）**：
选一个**凭据**和一个模型，得到一个**实例**。启动之间只在凭据和模型上不同。
_Avoid_: 切换、profile

**实例（Instance）**：
由一次**启动**产生的一个正在运行的 Claude Code 进程；多个实例可以并排运行，互不影响。
_Avoid_: 会话、pane

**共享设置（Shared settings）**：
所有**实例**共用、各机器之间同步的那部分 agent 设置（hooks、权限、MCP、插件等），即 agent 设置文件里除**默认凭据**那几项之外的全部内容。它不含任何秘密。每个平台一份（Windows 一份、macOS 一份），同平台的机器共用一份；两份之间互不同步。
_Avoid_: profile、base、模板

**默认凭据（Default credential）**：
这台机器上不经过 Muster 启动的 agent（直接敲 `claude`、IDE 插件）所用的**凭据**。它写在 agent 设置文件里属于供应商的那几项中，只属于本机，从不同步；经 Muster **启动**的**实例**会完整覆盖它，不让它的任何一项漏进去。
_Avoid_: 当前凭据、全局凭据、active

## 同步

**登记文件（Tracked file）**：
用户手写、明确登记要同步的单个文件，例如 statusline 脚本、自写的 hook 脚本。和**共享设置**一样每个平台一份。没有登记的文件一律不同步，安装器装出来的文件也不在其列。
_Avoid_: 资产、dotfile

**装机清单（Setup list）**：
每个平台一份的条目列表。每个条目对应一样装出来的东西（一组 skills、GSD、插件市场等），由安装命令、可选的更新命令和可选的检查命令组成。新机器按清单重装，更新时按清单执行更新命令；Muster 只执行这些命令，不理解它们装了什么。
_Avoid_: 依赖、包、bootstrap 脚本

## 监控

**额度（Quota）**：
一个**订阅账号**在一个或多个周期窗口里用掉了多少比例，每个窗口有自己的重置时间。窗口的种类和长度以平台实际返回为准：例如 Claude 是 5 小时和每周，Codex Pro 目前只有一个 7 天窗口，Grok 是每周的 credits，Cursor 是计费月里的包含用量。**凭据**本身没有额度。
_Avoid_: 余量、配额（与网关的配额轮转混淆）

**用量（Usage）**：
已经消耗的 token 数或费用，按**凭据**（例如一把 AsterGate key 今天花了多少）或**实例**统计。
_Avoid_: 额度

## Flagged ambiguities

- **"token"** 同时指"凭据"和"消耗单位"。统一说法：秘密叫**凭据**，消耗叫**用量**。
- **"渠道"** 是 AsterGate 内部的概念；在本项目里，一个 AsterGate 渠道对应一个**凭据**（AsterGate 按 key 分渠道）。
- **"profile"** 在现有产品里指一份完整的 settings 快照。新方向里不再有这个概念：供应商信息归**凭据**，其余设置归**共享设置**。不要用 profile 称呼凭据或启动。

## Example dialogue

> **Dev**："我想并排开两个 Claude Code，一个走订阅 A，一个走 AsterGate 的渠道 X。"
> **Domain expert**："那是两次**启动**：一次选订阅 A 这个**凭据**加 Opus，一次选渠道 X 那个 key 对应的**凭据**加它能用的模型。两个**实例**的 hooks 和 MCP 一样，都来自**共享设置**。"
> **Dev**："渠道 X 那个 key 还剩多少？"
> **Domain expert**："key 本身没有**额度**。额度看它池子里那几个**订阅账号**的 5 小时和每周窗口；这把 key 今天花了多少是它的**用量**。"
