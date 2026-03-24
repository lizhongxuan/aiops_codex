<script setup>
import { ref, onMounted, onBeforeUnmount, watch, computed } from "vue";
import { useRouter } from "vue-router";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { ChevronRightIcon, ChevronDownIcon, TerminalIcon, CheckIcon, XIcon, ExternalLinkIcon } from "lucide-vue-next";
import "@xterm/xterm/css/xterm.css";
import { useAppStore } from "../store";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const router = useRouter();
const store = useAppStore();
const isExpanded = ref(true);
const terminalContainer = ref(null);
let term = null;
let fitAddon = null;

const isComplete = computed(() => {
  return props.card.status === "completed" || props.card.status === "error";
});

const isSuccess = computed(() => props.card.status === "completed");
const isFailed = computed(() => props.card.status === "error" || props.card.status === "failed");

const hasOutput = computed(() => !!props.card.output);
const canOpenTerminal = computed(() => {
  return store.selectedHost?.executable && store.selectedHost?.status === "online";
});

/* Human-readable duration */
const durationLabel = computed(() => {
  const ms = props.card.durationMs;
  if (!ms) return "";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(0)}s`;
});

function toggleExpand() {
  if (hasOutput.value) {
    isExpanded.value = !isExpanded.value;
    if (isExpanded.value) {
      setTimeout(() => fitAddon?.fit(), 10);
    }
  }
}

function openInTerminal(event) {
  event.stopPropagation();
  if (!canOpenTerminal.value) return;
  router.push(`/terminal/${store.snapshot.selectedHostId}`);
}

function initTerminal() {
  if (!terminalContainer.value || !hasOutput.value) return;

  term = new Terminal({
    theme: {
      background: "#0f172a",
      foreground: "#f8fafc",
      cursor: "transparent",
      selection: "rgba(255, 255, 255, 0.3)",
    },
    fontFamily: '"SF Mono", "Fira Code", monospace',
    fontSize: 13,
    lineHeight: 1.4,
    cursorBlink: false,
    disableStdin: true,
    padding: 12,
  });

  fitAddon = new FitAddon();
  term.loadAddon(fitAddon);
  term.open(terminalContainer.value);
  fitAddon.fit();

  const formattedOutput = props.card.output
    .replace(/\n?\[?exit code: 0\]?\n?$/i, "")
    .replace(/\n/g, "\r\n");
  term.write(formattedOutput);
}

watch(
  () => isExpanded.value,
  (expanded) => {
    if (expanded && !term) {
      setTimeout(initTerminal, 10);
    }
  }
);

watch(
  () => props.card.output,
  (newOutput) => {
    if (term) {
      term.clear();
      const cleanOutput = newOutput ? newOutput.replace(/\n?\[?exit code: 0\]?\n?$/i, "").replace(/\n/g, "\r\n") : "";
      term.write(cleanOutput);
      fitAddon?.fit();
    }
  }
);

onMounted(() => {
  if (isComplete.value && isSuccess.value) {
    isExpanded.value = false;
  } else {
    setTimeout(initTerminal, 10);
  }
});

onBeforeUnmount(() => {
  if (term) {
    term.dispose();
  }
});
</script>

<template>
  <!-- Completed success: collapsed timeline summary line -->
  <div v-if="isComplete && isSuccess && !isExpanded" class="timeline-summary" @click="toggleExpand">
    <div class="timeline-left">
      <CheckIcon size="14" class="timeline-check" />
      <span class="timeline-label">已运行 <code>{{ card.command }}</code></span>
    </div>
    <div class="timeline-divider"></div>
    <span class="timeline-duration" v-if="durationLabel">已处理 {{ durationLabel }}</span>
  </div>

  <!-- Full terminal card (running / failed / manually expanded) -->
  <div v-else class="terminal-card" :class="{'minimal': isComplete && !isExpanded}">
    <div class="term-header" @click="toggleExpand">
      <div class="term-title-group">
        <component :is="isExpanded ? ChevronDownIcon : ChevronRightIcon" size="16" class="icon-carat" />
        <TerminalIcon size="14" class="icon-term" />
        <span class="term-command mono">{{ card.command || "Executing..." }}</span>
      </div>

      <div class="term-meta">
        <button
          v-if="canOpenTerminal"
          type="button"
          class="term-open-btn"
          @click="openInTerminal"
        >
          <ExternalLinkIcon size="12" />
          <span>在终端中打开</span>
        </button>
        <span class="term-cwd" v-if="card.cwd">{{ card.cwd }}</span>
        <span class="term-status-badge success" v-if="isSuccess">
          <CheckIcon size="12" /> Success
        </span>
        <span class="term-status-badge error" v-if="isFailed">
          <XIcon size="12" /> Failed
        </span>
      </div>
    </div>

    <div class="term-body" v-if="isExpanded && hasOutput">
      <div class="xterm-wrapper" ref="terminalContainer"></div>
    </div>
  </div>
</template>

<style scoped>
/* ====== Timeline summary line (collapsed success) ====== */
.timeline-summary {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 0;
  margin-left: 48px;
  cursor: pointer;
  max-width: 800px;
}

.timeline-left {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text-meta, #9ca3af);
  font-size: var(--text-meta-size, 12px);
  white-space: nowrap;
}

.timeline-check {
  color: #22c55e;
}

.timeline-label {
  color: #6b7280;
}

.timeline-label code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  color: #374151;
  font-weight: 500;
}

.timeline-divider {
  flex: 1;
  height: 1px;
  background: #e5e7eb;
  min-width: 32px;
}

.timeline-duration {
  font-size: var(--text-meta-size, 12px);
  color: var(--text-meta, #9ca3af);
  white-space: nowrap;
}

/* ====== Full terminal card ====== */
.terminal-card {
  border-radius: var(--radius-card, 16px);
  background: #ffffff;
  border: 1px solid var(--border-card, #e5e7eb);
  overflow: hidden;
  margin-top: 4px;
  margin-left: 48px;
  max-width: 800px;
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.03);
  transition: all 0.2s;
}

.terminal-card.minimal {
  background: #f8fafc;
  box-shadow: none;
}

.terminal-card.minimal:hover {
  background: #f1f5f9;
  border-color: #cbd5e1;
}

.term-header {
  padding: 8px 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: pointer;
  user-select: none;
}

.term-title-group {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  overflow: hidden;
}

.icon-carat {
  color: #94a3b8;
}

.icon-term {
  color: #64748b;
}

.term-command {
  font-size: 13px;
  color: #0f172a;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-weight: 500;
}

.terminal-card.minimal .term-command {
  color: #64748b;
}

.term-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-left: 12px;
  flex-shrink: 0;
}

.term-open-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border: 1px solid #dbe3ee;
  background: #ffffff;
  color: #475569;
  border-radius: 999px;
  padding: 4px 10px;
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
}

.term-open-btn:hover {
  background: #f8fafc;
  border-color: #cbd5e1;
}

.term-cwd {
  font-size: 11px;
  color: #94a3b8;
  max-width: 150px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.term-status-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 12px;
}

.term-status-badge.success {
  background: #dcfce7;
  color: #166534;
}

.term-status-badge.error {
  background: #fee2e2;
  color: #991b1b;
}

.term-body {
  background: #0f172a;
  padding: 8px;
  border-top: 1px solid #1e293b;
}

.xterm-wrapper {
  width: 100%;
  border-radius: 6px;
  overflow: hidden;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
</style>
