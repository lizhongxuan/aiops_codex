<script setup>
import { computed, ref } from "vue";
import { BotIcon, ChevronDownIcon, ChevronUpIcon } from "lucide-vue-next";

defineOptions({
  inheritAttrs: false,
});

const props = defineProps({
  title: {
    type: String,
    default: "Background workers",
  },
  subtitle: {
    type: String,
    default: "点击查看正在运行的 host-agent",
  },
  agents: {
    type: Array,
    default: () => [],
  },
  emptyLabel: {
    type: String,
    default: "暂无后台 worker。",
  },
  docked: {
    type: Boolean,
    default: false,
  },
});

const emit = defineEmits(["select"]);
const expanded = ref(true);

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
        title: String(agent?.title || agent?.label || agent?.name || agent?.hostName || agent?.hostId || agent?.id || "agent"),
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
  return "neutral";
}

function selectAgent(agent) {
  emit("select", agent);
}

function toggleExpanded() {
  expanded.value = !expanded.value;
}
</script>

<template>
  <section class="protocol-background-agents-card" :class="{ docked }">
    <button class="background-summary" type="button" @click="toggleExpanded">
      <div class="background-summary-copy">
        <div class="background-summary-title">
          <BotIcon size="13" />
          <span>{{ normalizedAgents.length }} background agents</span>
        </div>
      </div>
      <component :is="expanded ? ChevronUpIcon : ChevronDownIcon" size="14" />
    </button>

    <div v-if="expanded && normalizedAgents.length" class="background-list">
      <button
        v-for="agent in normalizedAgents"
        :key="agent.id"
        class="background-agent"
        type="button"
        @click="selectAgent(agent)"
      >
        <div class="background-agent-inline">
          <strong :class="`tone-${toneClass(agent.status)}`">{{ agent.title }}</strong>
          <span class="agent-separator">-</span>
          <span>{{ agent.detail || agent.meta || agent.statusLabel }}</span>
        </div>
        <span class="background-open">Open</span>
      </button>
    </div>

    <div v-else-if="expanded" class="background-empty">
      <p>{{ emptyLabel }}</p>
    </div>
  </section>
</template>

<style scoped>
.protocol-background-agents-card {
  margin-left: 36px;
  max-width: 820px;
  border-radius: 10px;
  border: 1px solid rgba(226, 232, 240, 0.96);
  background: #ffffff;
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.02);
  overflow: hidden;
}

.protocol-background-agents-card.docked {
  margin-left: 0;
  max-width: 100%;
  border-radius: 10px;
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.02);
}

.background-summary {
  width: 100%;
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
  padding: 6px 12px;
  border: 0;
  background: transparent;
  cursor: pointer;
  font: inherit;
  color: inherit;
}

.background-summary-copy {
  min-width: 0;
  text-align: left;
}

.background-summary-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #6b7280;
  font-size: 11px;
  font-weight: 600;
}

.background-list {
  display: flex;
  flex-direction: column;
  border-top: 1px solid rgba(241, 245, 249, 0.95);
}

.background-agent {
  width: 100%;
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
  padding: 6px 12px;
  border: 0;
  background: transparent;
  cursor: pointer;
  font: inherit;
}

.background-agent + .background-agent {
  border-top: 1px solid rgba(243, 244, 246, 0.95);
}

.background-agent-inline {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  text-align: left;
  overflow: hidden;
}

.background-agent-inline strong {
  font-size: 12px;
  line-height: 1.4;
  font-weight: 600;
  white-space: nowrap;
}

.background-agent-inline span {
  color: #6b7280;
  font-size: 11px;
  line-height: 1.4;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.agent-separator {
  color: #cbd5e1;
  flex-shrink: 0;
}

.background-open {
  flex: none;
  color: #9ca3af;
  font-size: 11px;
  font-weight: 600;
}

.background-empty {
  border-top: 1px solid rgba(241, 245, 249, 0.95);
  padding: 6px 12px;
  color: #6b7280;
  font-size: 11px;
}

.tone-success {
  color: #059669;
}

.tone-warning {
  color: #d97706;
}

.tone-danger {
  color: #dc2626;
}

.tone-neutral {
  color: #0f172a;
}

@media (max-width: 900px) {
  .protocol-background-agents-card {
    margin-left: 0;
    max-width: 100%;
  }
}
</style>
