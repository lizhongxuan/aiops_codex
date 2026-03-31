<script setup>
import { computed } from "vue";
import { useRouter } from "vue-router";
import { useAppStore } from "../store";
import { resolveHostDisplay } from "../lib/hostDisplay";
import {
  ArrowRightIcon,
  HistoryIcon,
  ServerIcon,
  SettingsIcon,
  UserCircleIcon,
} from "lucide-vue-next";

const router = useRouter();
const store = useAppStore();

const currentSession = computed(() => store.activeSessionSummary || null);
const workspaceSession = computed(() => {
  if (currentSession.value?.kind === "workspace") return currentSession.value;
  return store.sessionList.find((session) => session.kind === "workspace") || null;
});

const currentHostLabel = computed(() => resolveHostDisplay(store.selectedHost) || "server-local");
const missionLabel = computed(() => workspaceSession.value?.title || "协作工作台");
const missionStatus = computed(() => {
  if (store.runtime.turn.active) return phaseLabel(store.runtime.turn.phase);
  if (workspaceSession.value?.status === "completed") return "已完成";
  if (workspaceSession.value?.status === "failed") return "失败";
  return "可用";
});

const entryCards = [
  {
    key: "hosts",
    title: "Hosts",
    subtitle: "主机清单、标签、会话与接入状态",
    description: "查看主机生命周期，维护执行范围，并从这里进入主机管理。",
    icon: ServerIcon,
    href: "/settings/hosts",
  },
  {
    key: "packs",
    title: "Experience Packs",
    subtitle: "经验包、playbook、版本演进",
    description: "把成功经验沉淀为可复用的运维包，并按场景加载到主 Agent。",
    icon: HistoryIcon,
    href: "/settings/experience-packs",
  },
  {
    key: "agent",
    title: "Agent Profile",
    subtitle: "System prompt、权限、skills、MCP",
    description: "维护 Agent 的执行边界与工具策略，控制它如何规划和行动。",
    icon: UserCircleIcon,
    href: "/settings/agent",
  },
];

function phaseLabel(phase) {
  switch (phase) {
    case "thinking":
      return "主 Agent 思考中";
    case "planning":
      return "PlannerSession 生成计划";
    case "waiting_approval":
      return "等待审批";
    case "waiting_input":
      return "等待补充输入";
    case "executing":
      return "执行中";
    case "finalizing":
      return "结果汇总中";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    case "aborted":
      return "已停止";
    default:
      return "待命";
  }
}

function openRoute(href) {
  router.push(href);
}
</script>

<template>
  <section class="settings-page">
    <header class="settings-hero">
      <div class="settings-hero-copy">
        <div class="settings-kicker">
          <SettingsIcon size="14" />
          <span>Settings Center</span>
        </div>
        <h2>设置</h2>
        <p>把主机、经验包和 Agent Profile 收敛到一个入口，避免再在主侧边栏里铺开一层运维目录。</p>
      </div>

      <div class="settings-hero-stats">
        <div class="settings-stat">
          <span>当前会话</span>
          <strong>{{ currentSession?.title || "新建会话" }}</strong>
        </div>
        <div class="settings-stat">
          <span>工作台</span>
          <strong>{{ missionLabel }}</strong>
        </div>
        <div class="settings-stat">
          <span>状态</span>
          <strong>{{ missionStatus }}</strong>
        </div>
        <div class="settings-stat">
          <span>当前主机</span>
          <strong>{{ currentHostLabel }}</strong>
        </div>
      </div>
    </header>

    <section class="settings-context-row">
      <article class="settings-context-card">
        <span class="settings-context-label">会话</span>
        <h3>{{ currentSession?.title || "暂无活动会话" }}</h3>
        <p>{{ currentSession?.preview || "切换到对话或工作台后，这里会显示当前会话概览。" }}</p>
      </article>

      <article class="settings-context-card">
        <span class="settings-context-label">工作台</span>
        <h3>{{ missionLabel }}</h3>
        <p>{{ workspaceSession?.preview || "工作台的 mission 摘要会出现在这里。" }}</p>
      </article>

      <article class="settings-context-card">
        <span class="settings-context-label">终端</span>
        <h3>{{ currentHostLabel }}</h3>
        <p>继续沿用当前选中的主机上下文，便于从设置页直接回到执行面。</p>
      </article>
    </section>

    <section class="settings-grid">
      <button
        v-for="card in entryCards"
        :key="card.key"
        type="button"
        class="settings-entry"
        @click="openRoute(card.href)"
      >
        <div class="settings-entry-head">
          <component :is="card.icon" size="18" class="settings-entry-icon" />
          <span class="settings-entry-arrow">
            <ArrowRightIcon size="16" />
          </span>
        </div>
        <div class="settings-entry-copy">
          <strong>{{ card.title }}</strong>
          <span>{{ card.subtitle }}</span>
          <p>{{ card.description }}</p>
        </div>
      </button>
    </section>

    <footer class="settings-footer">
      <button class="settings-footer-link" @click="openRoute('/')">回到对话</button>
      <button class="settings-footer-link" @click="openRoute('/protocol')">打开工作台</button>
      <button class="settings-footer-link" @click="openRoute('/settings/agent')">直接进入 Agent Profile</button>
    </footer>
  </section>
</template>

<style scoped>
.settings-page {
  min-height: 100%;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 18px;
  background:
    radial-gradient(circle at top right, rgba(37, 99, 235, 0.08), transparent 28%),
    linear-gradient(180deg, #f8fbff 0%, #f8fafc 100%);
}

.settings-hero {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  padding: 22px;
  border-radius: 24px;
  background: rgba(255, 255, 255, 0.88);
  border: 1px solid rgba(226, 232, 240, 0.9);
  box-shadow: 0 18px 40px rgba(15, 23, 42, 0.05);
}

.settings-hero-copy {
  min-width: 0;
}

.settings-kicker {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-radius: 999px;
  background: #eff6ff;
  color: #1d4ed8;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.settings-hero h2 {
  margin: 12px 0 8px;
  font-size: 30px;
}

.settings-hero p {
  margin: 0;
  color: #475569;
  line-height: 1.7;
  max-width: 760px;
}

.settings-hero-stats {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  min-width: 320px;
}

.settings-stat,
.settings-context-card,
.settings-entry {
  border-radius: 20px;
  background: white;
  border: 1px solid rgba(226, 232, 240, 0.9);
}

.settings-stat {
  padding: 14px 16px;
}

.settings-stat span,
.settings-context-label,
.settings-entry span {
  display: block;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #64748b;
}

.settings-stat strong,
.settings-context-card h3,
.settings-entry strong {
  display: block;
  margin-top: 6px;
  color: #0f172a;
}

.settings-context-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.settings-context-card {
  padding: 16px;
  box-shadow: 0 12px 28px rgba(15, 23, 42, 0.04);
}

.settings-context-card h3 {
  margin: 8px 0 6px;
  font-size: 18px;
}

.settings-context-card p {
  margin: 0;
  color: #64748b;
  line-height: 1.7;
}

.settings-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}

.settings-entry {
  padding: 16px;
  text-align: left;
  cursor: pointer;
  box-shadow: 0 12px 28px rgba(15, 23, 42, 0.05);
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
}

.settings-entry:hover {
  transform: translateY(-2px);
  border-color: rgba(37, 99, 235, 0.35);
  box-shadow: 0 18px 34px rgba(15, 23, 42, 0.08);
}

.settings-entry-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.settings-entry-icon {
  color: #2563eb;
}

.settings-entry-arrow {
  color: #94a3b8;
}

.settings-entry-copy strong {
  font-size: 18px;
}

.settings-entry-copy p {
  margin: 12px 0 0;
  color: #64748b;
  line-height: 1.7;
}

.settings-footer {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.settings-footer-link {
  border: 1px solid rgba(203, 213, 225, 0.9);
  background: white;
  color: #0f172a;
  border-radius: 999px;
  padding: 10px 14px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
}

@media (max-width: 960px) {
  .settings-hero,
  .settings-context-row,
  .settings-grid {
    grid-template-columns: 1fr;
  }

  .settings-hero {
    flex-direction: column;
  }

  .settings-hero-stats {
    min-width: 0;
  }
}

@media (max-width: 640px) {
  .settings-page {
    padding: 16px;
  }

  .settings-hero-stats {
    grid-template-columns: 1fr;
  }
}
</style>
