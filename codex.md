# Codex 原生二进制程序能力梳理 (跟当前项目无关)

截至 2026-03-23，本文基于 OpenAI 官方公开文档整理 `codex` 本地二进制程序的原生能力，并对比哪些能力无法通过“纯网页版本”复现，以及原生二进制程序是如何工作的。

这里的“Codex 二进制程序”主要指本地安装后的 `codex` 可执行文件，也就是 Codex CLI。需要说明的是，这个二进制本身还包含一些相关子命令，例如：

- 启动 CLI：`codex`
- 启动桌面 App：`codex app`
- 启动 App Server：`codex app-server`
- 非交互执行：`codex exec`
- Cloud 任务：`codex cloud`
- MCP 管理：`codex mcp`
- 把 Codex 自己作为 MCP Server 暴露：`codex mcp-server`
- 本地 sandbox 调试：`codex sandbox`
- 会话恢复：`codex resume`
- 会话分叉：`codex fork`
- 登录登出：`codex login` / `codex logout`
- 功能开关：`codex features`
- shell completion：`codex completion`

来源：

- [Codex CLI](https://developers.openai.com/codex/cli)
- [Command line options](https://developers.openai.com/codex/cli/reference)

## 1. Codex 原生二进制程序有哪些原生功能

下面按能力域来整理，而不是只罗列命令。

## 1.1 本地交互式 TUI

`codex` 启动后进入全屏终端 UI。这个 UI 不是简单聊天窗口，而是一个本地 agent 工作台。

原生能力包括：

- 直接读取当前仓库和工作目录
- 在本地工作区中编辑文件
- 在本地终端中执行命令
- 在执行前展示计划，并允许你内联审批/拒绝
- 显示语法高亮的 markdown 代码块和 diff
- 支持主题切换 `/theme`
- 支持草稿历史，`Up/Down` 可恢复之前输入过的 prompt
- 支持截图/图片作为输入
- 支持 `/clear`、`/copy`、`Ctrl+L`、`Ctrl+C`

这是 Codex 原生体验里最核心的部分，本质上是“本地代码上下文 + 本地终端 + 审批流 + 聊天”合在一起。

来源：

- [Codex CLI features](https://developers.openai.com/codex/cli/features)
- [Slash commands in Codex CLI](https://developers.openai.com/codex/cli/slash-commands)

## 1.2 本地会话持久化、恢复、分叉

CLI 会把 transcript 和会话状态保存在本地，因此有：

- `codex resume`
- `codex resume --last`
- `codex resume --all`
- `codex resume <SESSION_ID>`
- `codex fork`

这意味着用户可以在同一台机器上反复回到某个历史线程，而且保留原始 transcript、计划历史、审批记录。

来源：

- [Codex CLI features](https://developers.openai.com/codex/cli/features)
- [Command line options](https://developers.openai.com/codex/cli/reference)

## 1.3 本地文件系统与项目上下文发现

Codex CLI 会自动发现本地项目结构，并叠加本地配置。

原生能力包括：

- 以当前目录或项目根作为工作区
- 自动识别项目根，默认通过 `.git`
- 读取 `~/.codex/config.toml`
- 读取项目级 `.codex/config.toml`
- 读取 `AGENTS.md` / `AGENTS.override.md`
- 支持自定义 fallback instruction 文件名
- 支持 `CODEX_HOME`
- 支持 profile 叠加配置

这类能力很重要，因为它让 Codex 不是“只看到你这一次输入”，而是还能吃到本地 repo 的持久规则与约定。

来源：

- [Config basics](https://developers.openai.com/codex/config-basic)
- [Advanced Configuration](https://developers.openai.com/codex/config-advanced)
- [Custom instructions with AGENTS.md](https://developers.openai.com/codex/guides/agents-md)

## 1.4 本地命令执行、OS 级 sandbox、审批策略

这是网页最难复制的部分之一。

Codex 本地默认使用 OS 级 sandbox 和审批策略控制模型生成命令：

- 默认本地运行时网络关闭
- 默认写权限限制在当前 workspace
- 支持 `read-only`
- 支持 `workspace-write`
- 支持 `danger-full-access`
- 支持 `--ask-for-approval untrusted | on-request | never`
- 支持 `--full-auto`
- 支持 granular approval policy
- 支持 protected paths，例如 `.git`、`.codex`、`.agents`
- 支持 `codex sandbox` 在同等策略下执行任意命令
- 支持 `codex execpolicy` 检查策略规则

这不是前端 UI 层的“按钮确认”而已，而是本地进程真实受 OS 约束。

来源：

- [Agent approvals & security](https://developers.openai.com/codex/agent-approvals-security)
- [Command line options](https://developers.openai.com/codex/cli/reference)

## 1.5 原生 Slash Commands

CLI 有一整套键盘优先的原生命令，不需要离开当前会话：

- `/permissions`
- `/sandbox-add-read-dir`
- `/agent`
- `/apps`
- `/clear`
- `/compact`
- `/copy`
- `/diff`
- `/exit`
- `/experimental`
- `/feedback`
- `/init`
- `/logout`
- `/mcp`
- `/mention`
- `/model`
- `/fast`
- `/plan`
- `/personality`
- `/ps`
- `/fork`
- `/resume`
- `/new`
- `/quit`
- `/review`
- `/status`
- `/debug-config`
- `/statusline`
- `/approvals` 仍可用，但已退到 alias

这些命令不是“额外插件”，而是 CLI 原生工作流的一部分。

来源：

- [Slash commands in Codex CLI](https://developers.openai.com/codex/cli/slash-commands)

## 1.6 MCP、Skills、Subagents

Codex 原生二进制不只是一个聊天程序，它本身就是一个可扩展 agent runtime。

### MCP

原生支持：

- STDIO MCP servers
- Streamable HTTP MCP servers
- Bearer Token 认证
- OAuth 认证
- `codex mcp add/remove/login`
- TUI 中 `/mcp` 查看 MCP 状态

特别关键的是，CLI 原生支持本地进程型的 STDIO MCP Server。

来源：

- [Model Context Protocol](https://developers.openai.com/codex/mcp)
- [Command line options](https://developers.openai.com/codex/cli/reference)

### Skills

原生支持：

- 技能目录
- `SKILL.md`
- 可选 scripts / references / assets
- 显式触发：`$skill-name`
- 隐式触发：根据 description 匹配

来源：

- [Agent Skills](https://developers.openai.com/codex/skills)

### Subagents

原生支持：

- 显式请求后生成 subagents
- 并行执行多个子代理
- 主代理统一汇总结果
- `/agent` 切换到某个 agent thread
- 自定义不同模型/不同职责的 agent

来源：

- [Subagents](https://developers.openai.com/codex/subagents)

## 1.7 非交互自动化与 CI/CD

`codex exec` 是原生二进制非常关键的一部分。

原生能力包括：

- 非交互执行任务
- 可用于 CI、计划任务、脚本编排
- 默认只把最终结果打印到 `stdout`
- 过程流输出到 `stderr`
- `--json` 输出 JSONL 事件流
- `--output-schema` 输出结构化 JSON
- `--output-last-message`
- `--ephemeral`
- `exec resume`
- API Key 模式非常适合自动化

这使它不仅是一个“人用的聊天工具”，还是一个“可被脚本调用的 agent 命令行程序”。

来源：

- [Non-interactive mode](https://developers.openai.com/codex/noninteractive)
- [Command line options](https://developers.openai.com/codex/cli/reference)

## 1.8 认证、配置、模型提供方、OSS 模式

原生支持：

- ChatGPT 登录
- API Key 登录
- Device Auth
- 本地凭证缓存
- `config.toml`
- profiles
- feature flags
- 自定义 model providers
- 代理/网关型 provider
- `--oss` 本地 provider
- Ollama / LM Studio 一类本地模型 provider

来源：

- [Authentication](https://developers.openai.com/codex/auth)
- [Config basics](https://developers.openai.com/codex/config-basic)
- [Advanced Configuration](https://developers.openai.com/codex/config-advanced)

## 1.9 其他二进制原生命令

文档里还明确列出了这些原生命令：

- `codex app`
- `codex app-server`
- `codex apply`
- `codex cloud`
- `codex completion`
- `codex debug app-server send-message-v2`
- `codex mcp-server`

这些属于“二进制程序自带功能面”，即便它们不是 CLI TUI 主路径的一部分。

来源：

- [Command line options](https://developers.openai.com/codex/cli/reference)

## 2. 哪些能力没办法通过网页版本复现

这里我分三层说：

1. 纯网页绝对做不到
2. 纯网页做不到，但加本地代理/本地 companion app 可以逼近
3. 网页能做，但体验很难完全等价

下面的“网页版本”优先指纯浏览器架构。如果你允许用户机器上再装一个本地 agent，则有些能力可以补回来。

## 2.1 纯网页绝对做不到的

### A. 直接访问用户本机当前工作目录

浏览器天然不能像本地 CLI 一样：

- 直接读取用户当前 shell 所在目录
- 直接遍历用户本地仓库
- 直接在用户本机修改文件
- 直接读取本机 `.git`、`.codex`、`AGENTS.md`

除非用户另外安装本地 agent、浏览器扩展或桌面 helper。

这是浏览器安全模型决定的，不是前端 UI 做得再像就能解决。

### B. 直接运行用户本机 shell 并受 OS sandbox 约束

CLI 的原生命令执行是在本机进程里发生的，并由 OS 级 sandbox 保护。纯网页无法：

- 在本机直接拉起 shell
- 用 macOS Seatbelt / Linux Landlock 去限制命令
- 对本机 writable roots 做真实限制
- 保护本机 `.git`、`.codex`、`.agents`

网页可以实现“审批卡片”，但这和“OS 实际拦截命令”不是一回事。

### C. 本地 STDIO MCP Server

纯网页无法像 CLI 一样直接启动本地进程型 MCP server，例如：

- `npx xxx-mcp`
- 一个本地 Python 脚本
- 一个本地二进制工具

因为浏览器本身不能任意启动本机进程。

### D. Shell 原生命令行集成

纯网页无法原生复现这些 CLI 属性：

- `codex exec | jq`
- `codex completion`
- shell alias / login shell
- `stdout` / `stderr` / JSONL 管道语义
- 用退出码参与脚本编排

这些是命令行程序的宿主能力，不是 Web UI 能自然拥有的。

### E. 本地 OSS Provider

纯网页无法直接复现：

- `--oss`
- 连接本机 Ollama / LM Studio
- 使用本地模型 provider

因为浏览器不能天然安全地接入用户电脑上的本地模型服务。

## 2.2 纯网页做不到，但加本地 agent 可以逼近的

这部分不是绝对不可能，而是已经不再是“纯网页”。

### A. 本地仓库读写

如果你部署一个本地 companion agent，网页可以间接做到：

- 读本地文件
- 写本地文件
- 跑本地命令
- 发现 `AGENTS.md`
- 发现 `.codex/config.toml`

但这时真正提供能力的已经不是浏览器，而是本地守护进程。

### B. 本地审批与本地安全边界

网页可以复刻审批卡片，但要想逼近原生 CLI 的安全边界，仍需要本地 agent 去真正执行：

- sandbox
- path allowlist
- network allow/deny
- protected paths

否则网页层审批只能算“产品逻辑约束”，不是“系统约束”。

### C. 会话恢复与本地 transcript

网页可以自己保存历史会话，但原生 CLI 的恢复点是和本机目录、会话文件、本地 transcript 紧密绑定的。要完全等价，也需要本地存储层和本地目录上下文。

## 2.3 网页能做，但很难做到等价体验的

### A. 聊天与审批

网页当然可以做聊天框和审批卡片，但原生 CLI 的强项是：

- 键盘优先
- 命令式操作快
- slash commands 反馈直接
- 终端、diff、审批、状态都在一个 TUI 里

网页能模仿，但手感通常还是更“重”。

### B. Diff / Review

网页可以展示 diff，也可以做 review，但 CLI / App 的原生体验和本地 Git、终端、工作区是同一个上下文。网页如果跑在远端环境，用户很容易产生“这不是我本地实际工作区”的割裂感。

### C. MCP、Skills、Subagents 可视化

网页可以复刻一套 UI，但要做到和原生一样顺滑，需要把：

- agent thread
- slash commands
- MCP 状态
- skill 触发
- subagent 切换

都重做一遍。工程量不小，而且仍然难有命令行的低摩擦。

## 3. 哪些能力是网页版天然更合适的

为了避免误解，也要反过来说一下。

官方 Codex web 本来就更适合：

- 云端后台长任务
- GitHub 仓库接入
- 在 OpenAI 管理的 cloud environment 里跑任务
- 统一的环境模板、setup script、internet access 策略
- 任务完成后直接给出 diff 和 PR

这和本地 CLI 是两套强项。

来源：

- [Codex web](https://developers.openai.com/codex/cloud)
- [Cloud environments](https://developers.openai.com/codex/cloud/environments)
- [Agent internet access](https://developers.openai.com/codex/cloud/internet-access)

## 4. 原生 Codex 二进制程序是如何工作的

下面这部分是基于官方文档的工程化理解，我会把“事实”和“推断”分开说。

## 4.1 官方文档明确说明的工作方式

### 启动阶段

Codex 启动时会先确定这些东西：

- 当前工作目录
- 项目根
- 配置层
- 认证方式
- sandbox 模式
- approval policy
- model / provider
- 是否启用 MCP、skills、subagents、feature flags

它会从多层配置里合并有效值：

1. CLI flags / `--config`
2. profile
3. 项目级 `.codex/config.toml`
4. 用户级 `~/.codex/config.toml`
5. 系统级配置
6. 内置默认值

同时它还会发现 `AGENTS.md` 和项目指令文件，把这些作为 agent 的长期上下文。

### 运行阶段

在本地交互模式下，Codex 会进入 TUI 循环：

- 接收你的 prompt / 图片
- 读取项目文件
- 需要时运行命令
- 需要时修改文件
- 遇到策略要求时向你申请审批
- 把 reasoning、计划、命令执行、文件变更、tool 调用流式展示出来

如果是 `codex exec`，则进入非交互模式：

- 按既定 sandbox 和 approval policy 自动运行
- 最终输出结果到 `stdout`
- 过程事件流到 `stderr` 或 JSONL

### 工具扩展阶段

如果配置了 MCP / skills / subagents：

- MCP 为 Codex 提供额外工具和上下文
- skills 为 Codex 提供任务级工作流
- subagents 用于并行拆分子任务

### 会话落盘阶段

本地交互会话会保存在本机，因此后续可以：

- `resume`
- `fork`
- 查看 session id
- 延续审批和上下文

## 4.2 基于文档的工程推断

下面几条是“从文档可以合理推断出的内部工作方式”，不是官方逐字表述：

### A. 它本质上是一个本地 agent runtime

我的判断是，`codex` 二进制并不只是“包装了一个聊天请求”，而是一个本地 agent 宿主：

- 它负责上下文装配
- 负责本地工具执行
- 负责安全策略
- 负责 transcript 持久化
- 负责和模型/API/认证交互

也就是说，真正的“原生体验”来自这个本地 runtime，而不是单独某个聊天页面。

### B. 它的关键优势来自“离本地环境很近”

Codex CLI 最强的地方，不是 UI，而是它天然紧贴：

- 本地仓库
- 本地 shell
- 本地文件系统
- 本地 sandbox
- 本地配置
- 本地 Git 状态

所以网页要想复现 CLI，不是先做聊天框，而是要先补一个“本地运行时”。

### C. Web 和 CLI 的差异本质上是宿主环境差异

官方文档也能看出：

- CLI / App / IDE Extension 的安全模型依赖本地 OS 级机制
- Web 的安全模型依赖云容器和环境配置

所以两者不是单纯 UI 形态不同，而是运行宿主不同：

- CLI 是 local runtime
- Web 是 cloud runtime

这也是为什么很多“原生能力”不是网页做不好，而是根本不属于浏览器宿主。

## 5. 对你现在这个项目的直接启示

结合你正在设计的网页版 AI 运维平台，我的建议是：

### 5.1 不要把目标定成“100% 复刻原生 Codex CLI”

这件事不现实，因为纯网页不可能完全拿到：

- 本机文件系统
- 本机 shell
- OS sandbox
- 本地 STDIO MCP
- 命令行管道语义

更合理的目标是：

- 复刻对用户最有价值的体验
- 接受宿主环境不同带来的能力边界
- 在远程主机侧引入一个轻量 `Host Agent`，由它主动向业务 `App-Server` 建立双向 `gRPC` 长连接

### 5.2 你的网页版应该重点复刻这些

- 聊天流中的审批卡片
- 实时终端日志回显
- 会话恢复
- 结构化远程工具
- diff / 配置文件 patch 预览
- 审计与回放
- thread 绑定 terminal session
- Host Agent 主动回连的主机接入模型

### 5.3 如果以后想进一步靠近原生体验

你需要引入本地 companion agent，而不只是前端页面：

- 页面只负责 UI
- 本地 agent 负责文件系统、shell、sandbox、MCP stdio、session storage

没有这一层，本质上就只能做“远程 Codex 控制台”，做不到“本地原生 Codex”。

## 6. 结论

一句话总结：

`codex` 原生二进制程序的核心，不是“一个终端聊天界面”，而是“一个运行在本机、可读写本地工程、可受 OS sandbox 约束、可被脚本化、可扩展 MCP/skills/subagents 的 agent runtime”。

因此，纯网页版本最难复现的不是 UI，而是下面这几类原生能力：

- 本机文件系统与仓库直连
- 本机 shell 与 OS sandbox
- 本地 STDIO MCP
- 命令行脚本化与 JSONL/pipe/exit code 语义
- 本地配置层、AGENTS.md、session files
- 本地 provider / `--oss`

如果你的目标是 AI 运维网页平台，那么更好的方向不是硬复刻 CLI，而是把：

- 审批
- 终端
- 结构化工具
- 审计
- 线程与终端绑定

做到足够顺滑。

## 7. 参考资料

- [Codex CLI](https://developers.openai.com/codex/cli)
- [Codex CLI features](https://developers.openai.com/codex/cli/features)
- [Command line options](https://developers.openai.com/codex/cli/reference)
- [Slash commands in Codex CLI](https://developers.openai.com/codex/cli/slash-commands)
- [Authentication](https://developers.openai.com/codex/auth)
- [Agent approvals & security](https://developers.openai.com/codex/agent-approvals-security)
- [Model Context Protocol](https://developers.openai.com/codex/mcp)
- [Agent Skills](https://developers.openai.com/codex/skills)
- [Subagents](https://developers.openai.com/codex/subagents)
- [Config basics](https://developers.openai.com/codex/config-basic)
- [Advanced Configuration](https://developers.openai.com/codex/config-advanced)
- [Custom instructions with AGENTS.md](https://developers.openai.com/codex/guides/agents-md)
- [Non-interactive mode](https://developers.openai.com/codex/noninteractive)
- [Codex web](https://developers.openai.com/codex/cloud)
- [Cloud environments](https://developers.openai.com/codex/cloud/environments)
- [Agent internet access](https://developers.openai.com/codex/cloud/internet-access)
