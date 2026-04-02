<script setup>
import { computed, ref, watch } from "vue";
import { CheckCircle2Icon, ChevronDownIcon, ChevronUpIcon, CircleDotIcon, Loader2Icon } from "lucide-vue-next";

defineOptions({
  inheritAttrs: false,
});

const props = defineProps({
  title: {
    type: String,
    default: "工作台计划投影",
  },
  summaryLabel: {
    type: String,
    default: "",
  },
  steps: {
    type: Array,
    default: () => [],
  },
  initiallyExpanded: {
    type: Boolean,
    default: true,
  },
  docked: {
    type: Boolean,
    default: false,
  },
});

const emit = defineEmits(["step-action", "host-select"]);

const expanded = ref(props.initiallyExpanded);

watch(
  () => props.steps.length,
  (next, previous) => {
    if (!previous && next) {
      expanded.value = true;
    }
  },
);

const normalizedSteps = computed(() =>
  (Array.isArray(props.steps) ? props.steps : []).map((step, index) => ({
    id: String(step?.id || `step-${index + 1}`),
    index: step?.index || index + 1,
    title: String(step?.step?.title || step?.title || step?.step || "步骤"),
    detail: String(step?.detail || step?.summary || step?.step?.description || ""),
    status: String(step?.statusLabel || step?.statusText || step?.status || "待执行"),
    hosts: Array.isArray(step?.hostAgent || step?.hostAgents || step?.hosts)
      ? (step.hostAgent || step.hostAgents || step.hosts)
      : [],
    actions: Array.isArray(step?.actions || step?.buttons) ? (step.actions || step.buttons) : [],
    raw: step,
  })),
);

const completedCount = computed(() =>
  normalizedSteps.value.filter((step) => /完成|success|done/i.test(step.status)).length,
);

const widgetSummary = computed(() => {
  if (props.summaryLabel) return props.summaryLabel;
  const total = normalizedSteps.value.length;
  return `共 ${total} 个任务，已完成 ${completedCount.value} 个`;
});

function toggleExpanded() {
  expanded.value = !expanded.value;
}

function emitStepAction(action, step) {
  emit("step-action", { action, plan: step.raw });
}

function emitHostSelect(host, step) {
  emit("host-select", { host, plan: step.raw });
}

function hostLabel(host) {
  if (!host) return "";
  if (typeof host === "string") return host;
  return String(host.label || host.name || host.hostId || host.id || "");
}

function toneClass(status) {
  const value = String(status || "").toLowerCase();
  if (value.includes("完成") || value.includes("success") || value.includes("done")) return "success";
  if (value.includes("审批") || value.includes("wait")) return "warning";
  if (value.includes("失败") || value.includes("fail") || value.includes("error")) return "danger";
  if (value.includes("执行") || value.includes("run") || value.includes("active")) return "active";
  return "neutral";
}
</script>

<template>
  <section class="protocol-inline-plan-widget" :class="{ docked }">
    <button class="plan-widget-summary" type="button" @click="toggleExpanded">
      <div class="plan-widget-summary-main">
        <div class="plan-widget-title-row">
          <CircleDotIcon size="14" />
          <span>{{ title }}</span>
        </div>
        <strong>{{ widgetSummary }}</strong>
      </div>
      <component :is="expanded ? ChevronUpIcon : ChevronDownIcon" size="18" />
    </button>

    <div v-if="expanded" class="plan-widget-body">
      <article
        v-for="step in normalizedSteps"
        :key="step.id"
        class="plan-widget-step"
      >
        <div class="plan-widget-step-main">
          <span class="plan-widget-index">{{ step.index }}.</span>
          <div class="plan-widget-copy">
            <div class="plan-widget-line">
              <span class="plan-widget-title">{{ step.title }}</span>
              <span class="plan-widget-status" :class="toneClass(step.status)">
                <Loader2Icon v-if="/执行|run|active/i.test(step.status)" size="12" class="spin" />
                <CheckCircle2Icon v-else-if="/完成|success|done/i.test(step.status)" size="12" />
                <CircleDotIcon v-else size="12" />
                {{ step.status }}
              </span>
            </div>
            <p v-if="step.detail" class="plan-widget-detail">{{ step.detail }}</p>
            <div v-if="step.hosts.length" class="plan-widget-hosts">
              <button
                v-for="host in step.hosts"
                :key="host.id || host.hostId || hostLabel(host)"
                type="button"
                class="plan-widget-host plan-host-pill"
                @click="emitHostSelect(host, step)"
              >
                {{ hostLabel(host) }}
              </button>
            </div>
          </div>
        </div>

        <div v-if="step.actions.length" class="plan-widget-actions">
          <button
            v-for="action in step.actions"
            :key="action.id || action.key || action.label"
            type="button"
            class="plan-widget-action plan-action"
            @click="emitStepAction(action, step)"
          >
            {{ action.label || action.text || "查看" }}
          </button>
        </div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.protocol-inline-plan-widget {
  margin-left: 48px;
  max-width: 900px;
  border-radius: 26px;
  border: 1px solid rgba(226, 232, 240, 0.96);
  background: #ffffff;
  box-shadow: 0 10px 28px rgba(15, 23, 42, 0.04);
  overflow: hidden;
}

.protocol-inline-plan-widget.docked {
  margin-left: 0;
  max-width: 100%;
  border-radius: 24px;
  box-shadow: 0 6px 20px rgba(15, 23, 42, 0.03);
}

.plan-widget-summary {
  width: 100%;
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  padding: 16px 20px 15px;
  border: 0;
  background: transparent;
  cursor: pointer;
  font: inherit;
  color: inherit;
}

.plan-widget-summary-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  text-align: left;
}

.plan-widget-title-row {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: #6b7280;
  font-size: 12.5px;
  font-weight: 600;
}

.plan-widget-summary strong {
  color: #111827;
  font-size: 16px;
  line-height: 1.55;
  font-weight: 600;
}

.plan-widget-body {
  border-top: 1px solid rgba(241, 245, 249, 0.95);
  padding: 4px 0;
}

.plan-widget-step {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 20px;
}

.plan-widget-step + .plan-widget-step {
  border-top: 1px solid rgba(243, 244, 246, 0.95);
}

.plan-widget-step-main {
  display: flex;
  gap: 14px;
  min-width: 0;
}

.plan-widget-index {
  width: 22px;
  height: 22px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  border: 1px solid rgba(209, 213, 219, 0.95);
  color: #6b7280;
  font-weight: 600;
  font-size: 11px;
  line-height: 1;
  flex: none;
  margin-top: 1px;
}

.plan-widget-copy {
  display: flex;
  flex-direction: column;
  gap: 7px;
  min-width: 0;
}

.plan-widget-line {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}

.plan-widget-title {
  color: #111827;
  font-size: 16px;
  font-weight: 600;
  line-height: 1.6;
}

.plan-widget-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 9px;
  border-radius: 999px;
  background: #f8fafc;
  border: 1px solid rgba(226, 232, 240, 0.95);
  color: #475569;
  font-size: 12px;
  font-weight: 600;
}

.plan-widget-status.active {
  color: #1d4ed8;
  border-color: rgba(191, 219, 254, 0.95);
  background: #eff6ff;
}

.plan-widget-status.warning {
  color: #b45309;
  border-color: rgba(253, 230, 138, 0.95);
  background: #fffbeb;
}

.plan-widget-status.success {
  color: #047857;
  border-color: rgba(167, 243, 208, 0.95);
  background: #ecfdf5;
}

.plan-widget-status.danger {
  color: #b91c1c;
  border-color: rgba(254, 202, 202, 0.95);
  background: #fef2f2;
}

.plan-widget-detail {
  margin: 0;
  color: #6b7280;
  font-size: 14.5px;
  line-height: 1.72;
}

.plan-widget-hosts {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.plan-widget-host {
  border: 1px solid rgba(226, 232, 240, 0.95);
  background: #f8fafc;
  color: #334155;
  border-radius: 999px;
  padding: 6px 12px;
  font: inherit;
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
}

.plan-widget-actions {
  flex: none;
  display: flex;
  align-items: flex-start;
}

.plan-widget-action {
  border: 1px solid rgba(229, 231, 235, 0.95);
  background: #ffffff;
  color: #475569;
  border-radius: 999px;
  padding: 8px 14px;
  font: inherit;
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 900px) {
  .protocol-inline-plan-widget {
    margin-left: 0;
    max-width: 100%;
  }

  .plan-widget-step {
    flex-direction: column;
  }
}
</style>
