# Muster：产品定位与第一版决策

> 状态：方向已定（2026-10-17～18 讨论），代码尚未开始。
> 术语以同目录 [CONTEXT.md](CONTEXT.md) 为准；为什么用 Go 重写见 [ADR 0001](../adr/0001-rewrite-in-go.md)。

## 1. 为什么要做

现在要同时用好几个工具：一个负责切换供应商配置，一个（就是 agentswap）负责并排启动不同配置的 Claude Code，还有一个负责快速配置和本地转发。工具之间互不相通。另外，公司和家里的电脑都在用 Claude Code，每到一台新机器都要重装、反复调 hooks 和配置。

目标是用一个工具把这些都收拢起来，最终完全替代它们。

## 2. 定位

> **所有 AI 订阅的额度一目了然，按凭据并排启动 Claude Code。**
> *Every AI subscription at a glance. Launch agents side by side.*

- **Agent first**：CLI 是一等入口，所有命令都支持 `--json`。前端能做的每一件事，agent 都能通过 CLI 完成。
- **精致的前端**：网页是完整的操作界面，和 CLI 用的是同一个核心库。功能永远先加在核心里，再分别接到两边。
- 产品名 **Muster**，命令 `muster`。

## 3. 第一版范围

### 3.1 登记

- **供应商 → 凭据**两层。每个凭据带着自己的模型映射（opus / sonnet / haiku 等别名各指向哪个模型），比如 AsterGate 里 Claude 池的 key 和 Codex 池的 key，映射就不一样。
- **订阅账号**是额度的归属单位。AsterGate 池子里的订阅账号，按 `account_id` 去重后只算一次。
- **本机登录**：Codex、Cursor、Grok 只用来看额度，额度从本机客户端已有的登录里读（`~/.codex/auth.json`、Cursor 的 `state.vscdb`、`~/.grok/auth.json`）。**只读，绝不刷新它们的 token**，否则会让对应的客户端被登出。所以一台机器只能看到在本机登录过的账号。

### 3.2 额度总览

- AsterGate：每把 key 调一次 `/me/usage`，合并去重。查询只读，不消耗额度。
- Codex、Cursor、Grok：每家一个互相隔离的小适配器。这几家的接口都不是官方公开的，某一家失败只影响它自己那一行。
- **statusline 第二排**由 `muster statusline` 输出，内容是：当前账号的套餐、5 小时和本周两个窗口的进度条及重置时间、这把 key 今天的花费；换号后显示"已换号 → 套餐"，保留 10 分钟。额度缓存和选择器、网页共用。Muster 写好之前，先用 `~/.claude/statusline.sh` 里 2026-10-17 写的 bash 版本。

### 3.3 启动

- 在 pane 里敲 `muster`，会弹出**交互式选择器**：每行一个凭据，带额度进度条；默认选中上一次用的；先显示缓存的额度，后台再刷新。选好凭据和模型后，就在当前 pane 里启动 Claude Code。
- `muster run <凭据> [-m 模型]` 不经过交互，给 agent 和脚本用。
- **分屏由你自己做**，Muster 不集成任何分屏工具。"主 agent 帮你开副 agent"的场景，由 agent skill 里写好的分屏命令来完成（比如 `herdr ... -- muster run <凭据>`）。
- **默认凭据**：不经过 Muster 启动的 agent（直接敲 `claude`、IDE 插件）使用的凭据，写在 `~/.claude/settings.json` 里属于供应商的那几项（`isAuthRoutingEnvKey` 划出的范围），只属于本机，不同步。经 Muster 启动的实例会通过 `--settings` 完整覆盖它：默认凭据里有、而当前凭据里没有的变量一律设成空字符串，避免漏进来。

### 3.4 多机同步

- 同步仓库是一个 GitHub 私有仓库，和 Muster 的代码仓库分开。
- **同步的内容**：
  1. **共享设置**：每个平台一份（Windows 一份、macOS 一份），同平台的机器共用；整个文件同步，只排除默认凭据那几项。两个平台之间不互相同步。
  2. **登记文件**：你手写、并明确登记过的单个文件（`muster track <路径>`），比如 statusline 脚本、自己写的 hook 脚本。Muster 不扫描目录。
  3. **装机清单**：每个平台一份。每个条目由安装命令、可选的更新命令和可选的检查命令组成；新机器按清单重装，`muster update` 按清单执行更新。Muster 只执行这些命令，不解析各家安装器的格式。插件写进共享设置的 `enabledPlugins` 和 `extraKnownMarketplaces`，由 Claude Code 自己安装。
  4. **凭据登记表**：其中的秘密用 age 的口令模式加密后放进仓库。新机器上执行 `muster sync init <仓库>`，输入一次口令就能全部恢复。
- **不同步的内容**：安装器装出来的文件（软链接的 skills、GSD 的文件等）、本机登录、会话历史和 auto memory、默认凭据。
- 本机的秘密存放在只有你自己能读的文件里（Unix 上是 0600 权限），不使用系统钥匙串。原因是默认凭据本来就必须明文写在 `settings.json` 里。

### 3.5 前端

- 执行 `muster web` 时按需启动，打开浏览器，不常驻后台。安全模型沿用现有设计：会话 token 放在 URL 片段里，再加 Host 和 Origin 校验。
- 页面：
  1. **额度总览**
  2. **凭据**：地址、key、模型映射，以及默认凭据设置
  3. **同步**：同步状态、冲突对比、装机清单、登记文件、一键更新
  4. **共享设置编辑器**：顶部切换 Windows 和 Mac。编辑本机所在平台的那份时，直接写本机的 `settings.json`，照样加锁、做 CAS 校验；编辑另一个平台的那份时，写同步仓库里的对应文件。继续遵守"投影而不是解析"的规则（见现有 `docs/architecture.md`）。
- 网页里不做启动。

## 4. 技术选型

- **Go 重写**（[ADR 0001](../adr/0001-rewrite-in-go.md)）：单个二进制，新机器下载即用；以后做本地池化时可以复用 AsterGate 的 Go 代码。性能不是选它的理由。
- 前端用 React 写成单页应用，构建产物嵌入二进制。构建期的依赖不进入运行时。
- 每引入一个依赖都要单独说明理由。目前确定要用的两个：`modernc.org/sqlite`，用来只读打开 Cursor 的登录数据；`age`，用来加密秘密。
- **分发**：CLI 二进制通过 install 脚本或 brew 安装。从命令行下载的二进制不会被打上隔离标记，所以第一版**不需要任何签名或证书**。
- **以后**：用 Wails 套一层桌面壳（托盘、开机自启），到那时再引入常驻进程（比如额度提醒），并加入 Apple Developer Program 做签名和公证。

## 5. 明确不做或往后放

- **本地网关和池化**：以后要做的是在本地起一个小池子，装 AsterGate 管不到的上游。到时候单独设计，复用 AsterGate 的 Go 代码。按模型分流也往后放。
- **退役 profile 整文件快照和 `use` 粘性切换**：启动只由"凭据 + 模型"决定。
- 内置分屏和布局文件（`muster up`）。
- 用 Claude Code 以外的 agent 启动（Codex CLI 等）。
- 从其他工具迁移配置的导入器。
- 常驻进程、模板渲染、解析安装器格式。

## 6. 迁移与仓库

- 原仓库改名为 `muster`，在本地 `muster` 分支上用 Go 重写；**开发完成之前不推送**。
- **不能把 npm 搞坏**：`agentswap@0.4.0` 一直可以安装。CI 只有在推送 `v*` 标签时才会改动 `latest`。真要推送的那一次，同时把 `publish.yml` 换成 Go 的发布流程，确保 npm 上永远不会出现 Go 代码的 agentswap 包。等 Muster 能替代 agentswap 之后，再执行 `npm deprecate agentswap` 并给 GitHub 仓库改名。
- CONTRIBUTING 和 AGENTS 按 Go 重写。"不点名其他工具"的规则保留。
- **会开源**：公开之前，要清理 git 历史里含有点名对比内容的旧调研文档（提交 `7b1e2b6`），这些文档已经移出仓库。

## 7. 待验证（实现前实测）

1. ✅ **已验证（2026-10-18，Claude Code 2.1.296）**：`--settings` 的 `env` 会逐个变量覆盖用户设置，设成空字符串等同于没有设置。实测时用户设置把 sonnet 映射到 `claude-sonnet-5-5[1M]`：不覆盖时用的是 `[1M]`；覆盖成 haiku 时用的是 `claude-haiku-5-5`；覆盖成 `""` 时回到内置的 `claude-sonnet-5-5`。这次只验证了模型映射这类变量。如果以后要接直连订阅（`CLAUDE_CODE_OAUTH_TOKEN`），还要另外验证 `ANTHROPIC_AUTH_TOKEN=""` 时，认证优先级是否也把它当成没有设置。
2. ✅ **已查明（skills 1.5.22）**：装机清单里的 skills 条目可以写成：安装 `npx skills add <来源> -g -a claude-code -s <名称> -y`，更新 `npx skills update -g -y`。这个版本**没有检查命令**，检查一栏留空；`npx skills ls -g --json` 只能列出已安装的 skills。
3. 额度接口（2026-10-18 用本机登录实测，只读，没有刷新 token）：
   - ✅ **Codex**：`GET https://chatgpt.com/backend-api/wham/usage`，带 `Authorization: Bearer <access_token>` 和 `ChatGPT-Account-Id: <account_id>`，两个值都来自 `~/.codex/auth.json`。返回 `plan_type`，以及 `rate_limit.primary_window` 和 `secondary_window`，每个窗口有 `used_percent`、`limit_window_seconds`、`reset_at`。实测 Pro 只有一个 7 天窗口，`secondary_window` 为 `null`；另外有 `credits.balance`。access token 有效期约 10 天，由 Codex CLI 负责刷新。
   - ✅ **Cursor**：Connect RPC 接口 `POST https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage` 和 `GetPlanInfo`，请求体为 `{}`，带 `Authorization: Bearer <cursorAuth/accessToken>`、`Content-Type: application/json`、`Connect-Protocol-Version: 1`。返回计费周期的起止时间，以及 `planUsage` 的 `limit` / `totalSpend` / `remaining`（单位看起来是美分）和 `totalPercentUsed`。`totalPercentUsed` 的单位和 `totalSpend / limit` 对不上，实现时要核对。`GetPlanInfo` 返回套餐名（实测是 Ultra）。
   - ⏳ **Grok**：Grok CLI 的 `/usage` 会显示账号额度（Usage limit），但它调用的接口要等本机 `grok login` 之后才能抓到。登录信息在 `~/.grok/auth.json`。
4. AsterGate 能不能把 Claude Code 的请求转给 Codex 池。这是在 Claude Code 里使用 Codex 池 key 的前提。目前手上只有 Claude 池的 key，还测不了。
5. 公司电脑上有没有 managed settings 覆盖 `--settings`。目前这台 Windows 机器上的 `C:/Program Files/ClaudeCode/managed-settings.json` 只设置了代理变量（`127.0.0.1:17971`），不影响 `ANTHROPIC_*` 的覆盖；公司电脑还需要单独检查。
6. 公司网络能不能访问 AsterGate。

## 8. 待定

- 同步怎么触发（手动执行 `muster sync`，还是启动时自动检查），以及冲突处理的具体交互。
- 旧命令 `aswap` 作为别名保留多久（暂定一个版本周期）。
- 本地池化的设计。
