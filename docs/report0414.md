# 报告：aiops-codex vs Codex 体验差异分析（2026-04-14）

## 问题现象

用户在 aiops-codex 工作台输入"查看A股行情"，模型回复"我这边当前拿不到可靠的实时指数源，没法负责任地直接报此刻的三大指数点位"。
而在 Codex 中同样的问题可以正常获取实时数据。

## 根因分析

### 1. 工具链路差异

| 维度 | Codex | aiops-codex |
|------|-------|-------------|
| 搜索方式 | 内置 `web_search_preview` 工具，直接调用 OpenAI Responses API 的原生 Bing 搜索 | `web_search` 函数工具，默认走 DuckDuckGo HTML 抓取 |
| 搜索质量 | Bing 搜索，结果丰富、实时性好 | DuckDuckGo HTML 抓取，经常被反爬限制，结果质量不稳定 |
| 搜索触发 | 模型直接调用 `web_search_preview`，搜索结果自动融入回复 | 模型需要先调用 `web_search` 函数工具，拿到结果后再组织回复 |
| 原生搜索 | Responses API 内置，模型可以在生成过程中随时触发搜索 | 仅在 `WEB_SEARCH_MODE=native` 时启用，当前默认是 `duckduckgo` |

### 2. 当前 ai-server 的搜索配置

从启动日志和代码分析：

- `WEB_SEARCH_MODE` 未设置 → 默认 `duckduckgo`
- 模型可用工具：`web_search`, `open_page`, `find_in_page`（DuckDuckGo 实现）
- `webSearchMode != "native"` → 不会启用 Responses API 原生搜索
- 模型通过 Chat Completions API（`/v1/chat/completions`）交互，不走 Responses API

### 3. 实际请求流程（从日志还原）

```
用户: "查看A股行情"
  ↓
buildChatRequest: model=gpt-5.4, tools=7, stream=true
  ↓ POST /v1/chat/completions (Chat Completions API)
模型第1轮: 调用 web_search 工具 → DuckDuckGo 搜索
  ↓
buildChatRequest: msgs=4 (加入搜索结果)
  ↓ POST /v1/chat/completions
模型第2轮: 调用 open_page 工具 → 抓取网页
  ↓
buildChatRequest: msgs=6
  ↓ POST /v1/chat/completions
模型第3轮: 生成回复文本（但搜索结果质量不够，模型判断数据不可靠）
  ↓
autoWebSearch 触发: POST /v1/responses (Responses API + native web_search)
  ↓ 代理到 chatgpt.com/backend-api/codex/responses → TLS 超时
  ↓
最终回复: "拿不到可靠的实时指数源"
```

### 4. 问题链条

1. **DuckDuckGo 搜索质量差**：HTML 抓取方式不稳定，金融实时数据抓取效果差
2. **autoWebSearch 回退失败**：当模型判断搜索结果不可靠时，`shouldAutoWebSearch` 触发了 Responses API 原生搜索作为回退，但代理到 `chatgpt.com` 的 TLS 握手超时
3. **模型保守策略**：system prompt 里强调"基于证据得出结论"，模型在搜索结果不充分时选择拒绝回答而不是猜测

### 5. Codex 为什么能成功

Codex 直接使用 OpenAI Responses API + 内置 `web_search_preview` 工具：
- 搜索由 OpenAI 服务端执行（Bing），不经过本地代理
- 搜索结果直接融入模型生成过程，不需要额外的工具调用轮次
- 搜索质量高，金融数据实时性好

## 解决方案

### 方案 A：启用原生搜索模式（推荐）

将 `WEB_SEARCH_MODE` 设为 `native`，让 ai-server 使用 Responses API 的原生搜索：

```bash
AIOPS_HTTP_ADDR=127.0.0.1:18080 \
USE_BIFROST=true \
LLM_PROVIDER=openai \
LLM_MODEL=gpt-5.4 \
LLM_API_KEY=sk-xxx \
LLM_BASE_URL=http://127.0.0.1:8317/v1 \
WEB_SEARCH_MODE=native \
HOST_AGENT_BOOTSTRAP_TOKEN=aiops-dev-token-2026 \
.data/bin/ai-server
```

前提：代理（8317）需要能稳定转发 `/v1/responses` 请求到 OpenAI。

### 方案 B：使用 Brave Search API

如果代理不稳定，可以用 Brave Search API 替代 DuckDuckGo：

```bash
WEB_SEARCH_MODE=brave \
BRAVE_API_KEY=BSA-xxx \
# ... 其他配置同上
```

Brave Search 质量远高于 DuckDuckGo HTML 抓取，且不依赖 Responses API。

### 方案 C：修复代理稳定性

当前代理（8317）转发 `/v1/responses` 到 `chatgpt.com/backend-api/codex/responses` 时 TLS 握手超时。
需要检查代理的上游连接配置，确保到 `chatgpt.com` 的 TLS 连接稳定。

## 其他发现

### system prompt 差异

aiops-codex 的 system prompt 强调"基于证据得出结论，不允许凭推测下结论"，这导致模型在搜索结果不充分时倾向于拒绝回答。Codex 没有这个约束，模型会更积极地使用搜索结果。

对于非运维场景（如查股票行情），这个约束过于严格。可以考虑：
- 在 system prompt 中区分"运维诊断"和"信息查询"场景
- 对信息查询类问题放宽证据要求

### autoWebSearch 机制

ai-server 有一个 `shouldAutoWebSearch` 回退机制：当模型回复包含"无法搜索"等拒绝短语时，自动触发 Responses API 原生搜索。这个机制设计合理，但因为代理不稳定导致回退也失败了。

### 工具数量差异

aiops-codex 给模型 7 个工具（ask_user_question, execute_readonly_query, execute_command, write_file, web_search, open_page, find_in_page），而 Codex 的工具集更精简。过多的工具可能分散模型的注意力。

## 建议优先级

1. **立即**：启动时加 `WEB_SEARCH_MODE=native` 环境变量
2. **短期**：修复代理到 chatgpt.com 的 TLS 稳定性
3. **中期**：system prompt 区分运维诊断和信息查询场景的证据要求
4. **长期**：考虑接入 Brave Search API 作为 DuckDuckGo 的替代
