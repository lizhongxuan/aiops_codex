# AI Chat 改造成 Codex 原生风格方案

## 1. 目标

把当前 `AI chat` 从“原始卡片流”改造成更接近 Codex app 的产品形态：

- 主线程更像自然对话，而不是技术卡片堆叠
- 一轮处理分成 `过程` 和 `结果` 两层
- `过程` 默认折叠，`结果` 默认展开
- `plan` 和 `background agents` 变成输入框上方的轻量控件
- 输入框成为页面主控件，比例、留白、圆角、按钮样式更接近 Codex 原生

一句话目标：

`让用户一眼看到最终答案，想看过程时再展开。`

---

## 2. 当前问题

基于当前实现，主要问题有 4 类：

### 2.1 信息结构不对

当前 [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue) 直接把原始 `cards` 一条条渲染，再额外插入：

- activity summary
- thinking
- plan dock
- approval overlay
- terminal dock

这会导致页面更像“调试面板”，不像一个自然进行中的 assistant thread。

### 2.2 过程与结果混在一起

当前：

- 文件浏览
- 搜索
- 命令痕迹
- 思考态
- plan
- 最终回答

都直接出现在用户面前。

Codex 原生更像是：

- 最终回答常驻
- 过程被整理成一个可折叠块

### 2.3 组件粒度太底层

当前 [CardItem.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/CardItem.vue) 是按 `card.type` 直接渲染：

- `MessageCard`
- `PlanCard`
- `NoticeCard`
- `ProcessLineCard`
- `ResultSummaryCard`

这意味着“存储结构”几乎直接决定“呈现结构”，缺少一层面向 UI 的重组。

### 2.4 Composer 不像页面主控件

当前 [Omnibar.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue) 还是偏工具化输入框：

- 尺寸偏小
- 卡片感偏强
- 与上方 plan/agents 的关系不够紧密

而 Codex 原生输入框是页面重心之一。

---

## 3. 目标体验

AI chat 页面改造后，应该有这样的阅读顺序：

1. 看到用户刚问了什么
2. 看到 assistant 最终给了什么回答
3. 如果当前还在运行，看到一条轻量的运行状态
4. 如果想看过程，再展开 `已处理 xx 秒`
5. plan / background agents 始终贴在输入框上方，像输入区的一部分

视觉上：

- 文本优先
- 卡片减弱
- 控件变轻
- 留白更大
- 输入框更像 Codex 原生 composer

---

## 4. 设计原则

### 4.1 过程折叠，结论展开

默认展开：

- 用户消息
- assistant 最终回复
- 报错
- 审批
- 当前仍在运行的状态

默认折叠：

- 文件浏览痕迹
- 搜索痕迹
- shell / terminal 痕迹
- 已完成 plan 过程
- background agents 运行日志
- 已完成的 thinking/progress

### 4.2 不直接展示内部实现

避免在用户主线程里出现：

- “正在判断路由”
- “这是简单对话”
- “准备派发 worker”
- 过多技术解释性 notice

这些内容如果需要，应该进入 `过程层`，并且默认折叠。

### 4.3 组件不再 1:1 映射存储卡片

不要让 `card.type` 决定最终 UI 结构。

应改成：

- 原始 cards -> turn formatter -> 展示用 view model -> 组件树

---

## 5. 新的信息架构

### 5.1 页面结构

- `ThreadSurface`
  - `TurnGroup`
    - user message
    - process group
    - final response

- `ComposerDock`
  - inline plan widget
  - background agents widget
  - omnibar

### 5.2 一轮 turn 的结构

建议新增统一 view model：

```json
{
  "id": "turn-1",
  "status": "running | completed | failed",
  "elapsedLabel": "已处理 53s",
  "userMessage": {
    "text": "查看主机是否安装了 nginx"
  },
  "processGroup": {
    "collapsedByDefault": true,
    "summary": "已浏览 2 个文件，运行 1 个搜索，检查 1 台主机",
    "items": [
      { "type": "search", "text": "Searched ..." },
      { "type": "file", "text": "Read ..." },
      { "type": "command", "text": "command -v nginx" }
    ]
  },
  "finalMessage": {
    "text": "当前按 server-local 检查结果：nginx 未安装。"
  },
  "widgets": {
    "plan": null,
    "agents": null
  }
}
```

---

## 6. 核心重构：turn formatter

### 6.1 新增文件

建议新增：

- [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)

### 6.2 职责

把当前 store 中的原始 cards 重新组织成更接近 Codex 展示的数据结构。

输入：

- `store.snapshot.cards`
- `store.runtime.activity`
- `store.runtime.turn`
- `pending approvals`
- `active plan`
- `terminal dock state`

输出：

- `turns`
- `activeWidgets`
- `composerContext`

### 6.3 格式化规则

#### A. 消息归类

按语义归类：

- `user`
- `assistant_final`
- `assistant_progress`
- `thinking`
- `plan`
- `agent_status`
- `search_trace`
- `file_trace`
- `command_trace`
- `error`
- `approval`
- `system_notice`

#### B. 过程归并

将多条低价值日志合成摘要：

- 多条 `Read xxx` -> `已浏览 N 个文件`
- 多条 `Search xxx` -> `已运行 N 个搜索`
- 多条 `command` -> `已执行 N 个命令`

#### C. 可折叠性

下列 items 进入 process group：

- `thinking`
- `search_trace`
- `file_trace`
- `command_trace`
- `agent_status`
- `system_notice`（非关键）

下列 items 不进入折叠：

- `user`
- `assistant_final`
- `error`
- `approval`

#### D. 生命周期判断

- `running` turn：process group 默认展开或半展开
- `completed` turn：process group 默认折叠
- `failed` turn：错误展开，过程折叠

---

## 7. 组件改造方案

### 7.1 ChatPage

文件：

- [web/src/pages/ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)

改造目标：

- 不再直接遍历原始 `cards`
- 改为遍历 `formattedTurns`
- 顶部 activity summary 逻辑迁入 process group
- runtime plan dock 不再孤立悬浮，改为 composer 上方 widget

### 7.2 新增 TurnGroup 组件

建议新增：

- [web/src/components/chat/ChatTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatTurnGroup.vue)

职责：

- 渲染一整轮对话
- 组织 user / process / final 三层

### 7.3 新增 ProcessGroup 组件

建议新增：

- [web/src/components/chat/ChatProcessGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatProcessGroup.vue)

职责：

- 显示 `已处理 53s`
- 支持折叠/展开
- 展开后渲染：
  - 浏览记录
  - 搜索记录
  - 命令记录
  - process 级摘要

### 7.4 MessageCard

文件：

- [web/src/components/MessageCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/MessageCard.vue)

改造目标：

- assistant 消息去卡片化
- 更像文本线程
- markdown 排版更舒服

建议样式：

- assistant 正文 `17px - 18px`
- `line-height: 1.8`
- 段间距明显
- 列表、代码、引用块使用更轻的视觉层级

### 7.5 Omnibar

文件：

- [web/src/components/Omnibar.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue)

改造目标：

- 更像 Codex app 底部输入器
- 成为页面主视觉之一

建议样式：

- `max-width: 920px - 980px`
- `min-height: 110px - 130px`
- `border-radius: 28px - 32px`
- 输入文字 `16px - 17px`
- 发送按钮黑色圆按钮

### 7.6 PlanCard

文件：

- [web/src/components/PlanCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/PlanCard.vue)

改造目标：

- 不再像传统卡片
- 像输入框上方的轻量 widget

保留信息：

- 总任务数
- 已完成数
- 简短步骤列表

去掉：

- 大量边框层级
- 过重标题
- 非必要视觉装饰

### 7.7 Background Agents Widget

建议复用已有思路，统一成更轻的折叠控件：

- `N background agents`
- 展开后每个 agent 一行
- 状态和 `Open` 入口

---

## 8. 样式方向

### 8.1 文本

- assistant 正文优先
- 用户 bubble 次之
- 控件标题更轻
- 技术 trace 文本更淡

### 8.2 留白

- turn 与 turn 间距更大
- message 与 message 间距更大
- process group 与 final response 要有清晰分隔

### 8.3 圆角与阴影

- 卡片减弱
- 控件统一更大的圆角
- 阴影更薄

### 8.4 颜色

- 主体仍以白底、灰阶、深色文字为主
- 强调色只用于：
  - 当前运行
  - 风险
  - 审批
  - 错误

---

## 9. 交互策略

### 9.1 折叠逻辑

- 当前活动 turn：process group 默认展开
- 已完成 turn：process group 默认折叠
- 用户手动展开后记住本地状态

### 9.2 自动摘要

当过程很多时，折叠标题不是简单写“处理中”，而是写：

- `已处理 53s`
- `已浏览 2 个文件，执行 1 个命令`
- `已检查 1 台主机`

### 9.3 plan 与 composer 的关系

plan widget 不出现在主线程正文里，而是粘在输入框上方：

- 当前 turn active 时展示
- turn 完成后仍可保留一段时间或缩成 summary

---

## 10. 建议的实施阶段

### P0：结构层

目标：

- 先建立正确的信息架构

任务：

- 新建 `chatTurnFormatter.js`
- 新建 `ChatTurnGroup.vue`
- 新建 `ChatProcessGroup.vue`
- `ChatPage.vue` 改为基于 formatted turns 渲染

### P1：视觉层

目标：

- 让页面一眼接近 Codex 原生

任务：

- 重做 `MessageCard.vue`
- 重做 `Omnibar.vue`
- 轻量化 `PlanCard.vue`
- 统一 background agents widget

### P2：打磨层

目标：

- 清理噪音

任务：

- 去掉实现导向 notice
- 合并重复 progress
- 优化空态
- 补自动化截图对比

---

## 11. 验收标准

做到下面这些，基本就接近 Codex app 的体验了：

- 第一眼看到的是最终答案，而不是过程日志
- 过程信息存在，但默认被折叠成一条摘要
- 输入框足够大，像页面主控件
- plan 和 background agents 像 composer 的一部分
- assistant 回复更像文本线程，而不是技术卡片
- 折叠区展开后信息仍然整齐、有顺序、可扫读

---

## 12. 具体文件改造清单

重点文件：

- [web/src/pages/ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
- [web/src/components/MessageCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/MessageCard.vue)
- [web/src/components/Omnibar.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue)
- [web/src/components/PlanCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/PlanCard.vue)
- [web/src/components/CardItem.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/CardItem.vue)

建议新增：

- [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
- [web/src/components/chat/ChatTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatTurnGroup.vue)
- [web/src/components/chat/ChatProcessGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatProcessGroup.vue)
- [web/src/components/chat/ChatComposerDock.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatComposerDock.vue)

---

## 13. 最终建议

不要再先继续修局部样式。

正确顺序应该是：

1. 先做 `turn formatter`
2. 再做 `过程折叠 / 结果展开`
3. 最后再把 `message + composer + widgets` 做成更像 Codex 原生

否则页面很容易继续陷入：

- 样式改了一点
- 结构没变
- 结果还是不像 Codex

