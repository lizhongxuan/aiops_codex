<script setup>
import { computed } from "vue";
import { CircleDotIcon, CpuIcon, SparklesIcon } from "lucide-vue-next";

defineOptions({
  inheritAttrs: false,
});

const props = defineProps({
  title: {
    type: String,
    default: "Background agents",
  },
  subtitle: {
    type: String,
    default: "后台 agent 状态列表",
  },
  agents: {
    type: Array,
    default: () => [],
  },
  emptyLabel: {
    type: String,
    default: "暂无后台 agent。",
  },
});

const emit = defineEmits(["select"]);

const normalizedAgents = computed(() =>
  (Array.isArray(props.agents) ? props.agents : [])
    .map((agent, index) => {
      if (typeof agent === "string") {
        return {
          id: `agent-${index}`,
          title: agent,
          status: "idle",
          statusLabel: "idle",
          detail: "",
          meta: "",
        };
      }
      return {
        id: String(agent?.id || agent?.agentId || agent?.hostId || agent?.name || index),
        title: String(agent?.title || agent?.label || agent?.name || agent?.hostName || agent?.hostId || "agent"),
        status: String(agent?.status || agent?.tone || "idle").toLowerCase(),
        statusLabel: String(agent?.statusLabel || agent?.status || "idle"),
        detail: String(agent?.detail || agent?.summary || agent?.text || ""),
        meta: String(agent?.meta || agent?.subtitle || agent?.route || ""),
      };
    })
    .filter((agent) => agent.title),
);

function toneClass(status) {
  const value = String(status || "").toLowerCase();
  if (!value) return "neutral";
  if (value.includes("run") || value.includes("active") || value.includes("success")) return "success";
  if (value.includes("wait") || value.includes("block") || value.includes("warn")) return "warning";
  if (value.includes("fail") || value.includes("error") || value.includes("off")) return "danger";
  if (value.includes("idle") || value.includes("queue") || value.includes("pending")) return "neutral";
  return "neutral";
}

function selectAgent(agent) {
  emit("select", agent);
}
</script>

<template>
  <section class="protocol-background-agents-card">
    <div class="background-head">
      <div>
        <div class="background-kicker">
          <SparklesIcon size="14" />
          <span>{{ title }}</span>
        </div>
        <p>{{ subtitle }}</p>
      </div>

      <div class="background-count">
        <CpuIcon size="14" />
        <span>{{ normalizedAgents.length }} 个</span>
      </div>
    </div>

    <div v-if="normalizedAgents.length" class="background-list">
      <button
        v-for="agent in normalizedAgents"
        :key="agent.id"
        class="background-agent"
        :class="toneClass(agent.status)"
        type="button"
        @click="selectAgent(agent)"
      >
        <div class="background-agent-head">
          <div class="background-agent-title">
            <CircleDotIcon size="14" />
            <strong>{{ agent.title }}</strong>
          </div>
          <span class="background-status">{{ agent.statusLabel }}</span>
        </div>
        <p v-if="agent.detail" class="background-detail">{{ agent.detail }}</p>
        <div v-if="agent.meta" class="background-meta">{{ agent.meta }}</div>
      </button>
    </div>

    <div v-else class="background-empty">
      <CircleDotIcon size="18" />
      <p>{{ emptyLabel }}</p>
    </div>
  </section>
</template>

<style scoped>
.protocol-background-agents-card {
  border-radius: 22px;
  border: 1px solid rgba(226, 232, 240, 0.95);
  background:
    radial-gradient(circle at top left, rgba(14, 165, 233, 0.04), transparent 36%),
    linear-gradient(180deg, rgba(255, 255, 255, 0.98), rgba(248, 250, 252, 0.95));
  box-shadow: 0 12px 28px rgba(15, 23, 42, 0.05);
  padding: 16px;
}

.background-head {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  align-items: flex-start;
}

.background-kicker {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #2563eb;
}

.background-head p {
  margin: 8px 0 0;
  color: #475569;
  line-height: 1.6;
}

.background-count {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border-radius: 999px;
  background: #f8fafc;
  border: 1px solid rgba(226, 232, 240, 0.92);
  color: #334155;
  font-size: 12px;
  font-weight: 800;
  white-space: nowrap;
}

.background-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 14px;
}

.background-agent {
  width: 100%;
  font: inherit;
  text-align: left;
  border-radius: 16px;
  border: 1px solid rgba(226, 232, 240, 0.92);
  background: rgba(255, 255, 255, 0.96);
  padding: 12px 13px;
  cursor: pointer;
  transition:
    transform 120ms ease,
    border-color 120ms ease,
    box-shadow 120ms ease;
}

.background-agent:hover {
  transform: translateY(-1px);
  border-color: rgba(96, 165, 250, 0.9);
  box-shadow: 0 14px 24px rgba(37, 99, 235, 0.1);
}

.background-agent.success {
  border-color: rgba(167, 243, 208, 0.95);
}

.background-agent.warning {
  border-color: rgba(253, 230, 138, 0.95);
}

.background-agent.danger {
  border-color: rgba(254, 202, 202, 0.95);
}

.background-agent-head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: flex-start;
}

.background-agent-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: #0f172a;
}

.background-agent-title strong {
  font-size: 13px;
  line-height: 1.4;
}

.background-status {
  flex: none;
  padding: 5px 9px;
  border-radius: 999px;
  background: #f8fafc;
  border: 1px solid rgba(226, 232, 240, 0.92);
  font-size: 11px;
  font-weight: 800;
  color: #475569;
}

.background-detail,
.background-meta {
  margin: 8px 0 0;
  color: #64748b;
  line-height: 1.6;
  font-size: 13px;
}

.background-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  min-height: 140px;
  margin-top: 14px;
  border-radius: 16px;
  border: 1px dashed rgba(203, 213, 225, 0.95);
  background: rgba(248, 250, 252, 0.9);
  color: #64748b;
}

.background-empty p {
  margin: 0;
}

@media (max-width: 760px) {
  .background-head {
    flex-direction: column;
  }
}
</style>
