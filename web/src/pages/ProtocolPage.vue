<script setup>
import { useRouter } from "vue-router";
import { protocolContext, protocolLanes, protocolNodes, toneClass } from "../data/opsWorkspace";

const router = useRouter();

function openTerminal(hostId) {
  router.push(`/terminal/${hostId}`);
}
</script>

<template>
  <section class="ops-page">
    <div class="ops-page-inner">
      <header class="ops-page-header">
        <div>
          <h2 class="ops-page-title">主 Agent 协议工作台</h2>
          <p class="ops-page-subtitle">用户需求、黑板 DAG、多主机子 Agent 与审批流在同一视图内协同。</p>
        </div>
      </header>

      <div class="ops-scope-bar">
        <div class="ops-scope-left">
          <span class="ops-pill is-info">Fleet · web-cluster</span>
          <span class="ops-pill">策略: 分批并行</span>
          <span class="ops-pill">审批: 批次审批</span>
          <span class="ops-pill is-purple">经验包: nginx-reload</span>
        </div>
        <div class="ops-actions">
          <button class="ops-button primary">打开终端</button>
        </div>
      </div>

      <div class="ops-grid ops-grid-protocol">
        <article class="ops-card">
          <div class="ops-card-header">
            <div>
              <h3 class="ops-card-title">任务上下文</h3>
              <p class="ops-card-subtitle">用户需求、planner 摘要与最近输入产物</p>
            </div>
          </div>

          <div class="ops-context-card">
            <span class="ops-subcard-label">用户需求</span>
            <p class="ops-detail-copy">{{ protocolContext.request }}</p>
          </div>

          <div class="ops-context-card is-planner">
            <span class="ops-subcard-label">主 Agent 规划摘要</span>
            <pre class="ops-preformatted">{{ protocolContext.planner }}</pre>
          </div>

          <div class="ops-context-card">
            <span class="ops-subcard-label">已附加上下文</span>
            <ul class="ops-list">
              <li v-for="item in protocolContext.attachments" :key="item">{{ item }}</li>
            </ul>
          </div>

          <div class="ops-card-actions">
            <button class="ops-button ghost">修改范围</button>
            <button class="ops-button ghost">重新规划</button>
            <button class="ops-button primary">开始执行</button>
          </div>
        </article>

        <article class="ops-card">
          <div class="ops-card-header">
            <div>
              <h3 class="ops-card-title">黑板 DAG</h3>
              <p class="ops-card-subtitle">节点状态、依赖关系、审批阻塞与失败回退路径</p>
            </div>
          </div>

          <div class="ops-dag-shell">
            <div class="ops-dag-canvas">
              <svg class="ops-dag-svg" viewBox="0 0 540 540" aria-hidden="true">
                <path d="M200 62 L250 62" />
                <path d="M270 92 L270 154" />
                <path d="M248 226 L126 294" />
                <path d="M270 226 L252 294" />
                <path d="M292 226 L414 294" />
                <path d="M272 378 L272 440" />
              </svg>

              <div
                v-for="node in protocolNodes"
                :key="node.id"
                class="ops-dag-node"
                :class="`is-${node.status}`"
                :style="{ left: `${node.x}px`, top: `${node.y}px`, width: `${node.w}px`, height: `${node.h}px` }"
              >
                <strong>{{ node.label }}</strong>
                <span>{{ node.detail }}</span>
              </div>
            </div>
          </div>

          <div class="ops-approval-strip">
            <div>
              <span class="ops-subcard-label">审批阻塞点</span>
              <p class="ops-detail-copy">web-08 需要批准命令: `nginx -t && nginx -s reload`</p>
            </div>
            <div class="ops-actions">
              <button class="ops-button primary">同意本批次</button>
              <button class="ops-button ghost">查看详情</button>
            </div>
          </div>
        </article>

        <article class="ops-card ops-sidebar-card">
          <div class="ops-card-header">
            <div>
              <h3 class="ops-card-title">子 Agent / Hosts</h3>
              <p class="ops-card-subtitle">每个 lane 对应一个目标主机或主机组</p>
            </div>
          </div>

          <div class="ops-lane-stack">
            <div v-for="lane in protocolLanes" :key="lane.id" class="ops-lane-card">
              <div class="ops-lane-top">
                <div class="ops-host-name">
                  <span class="ops-inline-dot" :class="toneClass(lane.tone)"></span>
                  <strong>{{ lane.title }}</strong>
                </div>
                <span class="ops-pill" :class="toneClass(lane.tone)">{{ lane.status }}</span>
              </div>
              <p class="ops-detail-copy">{{ lane.summary }}</p>
              <p class="ops-detail-copy ops-faint">{{ lane.meta }}</p>
              <div class="ops-inline-actions">
                <button class="ops-button ghost small" @click="openTerminal(lane.hostId)">终端</button>
                <button class="ops-button ghost small">会话</button>
                <button class="ops-button small" :class="lane.tone === 'warning' ? 'primary' : 'ghost'">
                  {{ lane.tone === "warning" ? "审批" : "详情" }}
                </button>
              </div>
            </div>
          </div>
        </article>
      </div>
    </div>
  </section>
</template>
