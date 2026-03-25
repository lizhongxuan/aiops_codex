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
  return (
    props.card.status === "completed" ||
    props.card.status === "error" ||
    props.card.status === "failed" ||
    props.card.status === "permission_denied" ||
    props.card.status === "disconnected" ||
    props.card.status === "host_timeout" ||
    props.card.status === "timeout" ||
    props.card.status === "cancelled"
  );
});

const isSuccess = computed(() => props.card.status === "completed");
const isFailed = computed(() => props.card.status === "error" || props.card.status === "failed");
const isPermissionDenied = computed(() => props.card.status === "permission_denied");
const isDisconnected = computed(() => props.card.status === "disconnected");
const isHostTimeout = computed(() => props.card.status === "host_timeout");
const isTimeout = computed(() => props.card.status === "timeout");
const isCancelled = computed(() => props.card.status === "cancelled");

const hasOutput = computed(() => !!props.card.output);
const isRunning = computed(() => !isComplete.value);
const canOpenTerminal = computed(() => {
  return (
    store.selectedHost?.status === "online" &&
    (store.selectedHost?.terminalCapable || store.selectedHost?.executable)
  );
});

const dynamicHeight = computed(() => {
  if (!props.card.output) return 60;
  const lines = props.card.output.split('\n').length;
  const h = Math.max(60, Math.min(250, lines * 20)); // approx 20px per line
  return h;
});

const commandDescriptor = computed(() => describeCommand(props.card.command || ""));

function stripMatchingQuotes(value) {
  if (!value || value.length < 2) return value;
  const first = value[0];
  const last = value[value.length - 1];
  if ((first === "'" && last === "'") || (first === '"' && last === '"')) {
    return value.slice(1, -1);
  }
  return value;
}

function shellKindLabel(shellName) {
  switch ((shellName || "").toLowerCase()) {
    case "zsh":
      return "Zsh";
    case "bash":
      return "Bash";
    case "sh":
      return "Sh";
    default:
      return "Shell";
  }
}

function normalizeDisplayCommand(value) {
  return value.replace(/\s+/g, " ").trim();
}

function truncateDisplayCommand(value, max = 108) {
  if (!value || value.length <= max) return value;
  return `${value.slice(0, max - 3)}...`;
}

function describeCommand(command) {
  const raw = (command || "").trim();
  if (!raw) {
    return { kind: "", display: "Executing..." };
  }

  let kind = "";
  let display = raw;

  const shellWrapper = raw.match(/^(?:\/bin\/)?(zsh|bash|sh)\s+-lc\s+([\s\S]+)$/i);
  if (shellWrapper) {
    kind = shellKindLabel(shellWrapper[1]);
    display = normalizeDisplayCommand(stripMatchingQuotes(shellWrapper[2].trim()));
  }

  const lowerDisplay = display.toLowerCase();
  if (/^(python|python3)\b/.test(lowerDisplay)) {
    kind = "Python";
    if (display.includes("<<")) {
      const binary = lowerDisplay.startsWith("python3") ? "python3" : "python";
      return { kind, display: `${binary} heredoc` };
    }
  } else if (/^(node|nodejs)\b/.test(lowerDisplay)) {
    kind = "Node";
  } else if (!kind) {
    kind = "Shell";
  }

  return {
    kind,
    display: truncateDisplayCommand(display),
  };
}

/* Human-readable duration */
const durationLabel = computed(() => {
  const ms = props.card.durationMs;
  if (!ms) return "";
  const totalSeconds = Math.max(1, Math.round(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const parts = [];
  if (hours > 0) parts.push(`${hours}h`);
  if (minutes > 0) parts.push(`${minutes}m`);
  if (seconds > 0 || parts.length === 0) parts.push(`${seconds}s`);
  return parts.join(" ");
});

function toggleExpand() {
  if (!hasOutput.value && isSuccess.value) {
    return;
  }
  isExpanded.value = !isExpanded.value;
  if (isExpanded.value) {
    setTimeout(() => {
      if (!term && hasOutput.value) {
        initTerminal();
        return;
      }
      fitAddon?.fit();
    }, 10);
  }
}

function openInTerminal(event) {
  event.stopPropagation();
  if (!canOpenTerminal.value) return;
  router.push(`/terminal/${store.snapshot.selectedHostId}`);
}

function initTerminal() {
  if (!terminalContainer.value || !hasOutput.value) return;

  disposeTerminal();

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

function disposeTerminal() {
  if (term) {
    term.dispose();
    term = null;
    fitAddon = null;
  }
}

watch(
  () => props.card.status,
  (status) => {
    if (status === "completed") {
      isExpanded.value = false;
      return;
    }
    if (
      status === "failed" ||
      status === "error" ||
      status === "permission_denied" ||
      status === "disconnected" ||
      status === "host_timeout" ||
      status === "timeout" ||
      status === "cancelled"
    ) {
      isExpanded.value = true;
    }
  }
);

watch(
  () => isExpanded.value,
  (expanded) => {
    if (!expanded) {
      disposeTerminal();
      return;
    }
    if (!term) {
      setTimeout(initTerminal, 10);
    }
  }
);

watch(
  () => props.card.output,
  (newOutput) => {
    if (newOutput && isExpanded.value && !term) {
      setTimeout(initTerminal, 10);
      return;
    }
    if (term) {
      term.clear();
      const cleanOutput = newOutput ? newOutput.replace(/\n?\[?exit code: 0\]?\n?$/i, "").replace(/\n/g, "\r\n") : "";
      term.write(cleanOutput);
      fitAddon?.fit();
    }
  }
);

onMounted(() => {
  if (isSuccess.value) {
    isExpanded.value = false;
  } else if (
    isFailed.value ||
    isPermissionDenied.value ||
    isDisconnected.value ||
    isHostTimeout.value ||
    isTimeout.value ||
    isCancelled.value
  ) {
    isExpanded.value = true;
    setTimeout(initTerminal, 10);
  } else {
    setTimeout(initTerminal, 10);
  }
});

onBeforeUnmount(() => {
  disposeTerminal();
});
</script>

<template>
  <div
    v-if="isSuccess && !isExpanded"
    class="timeline-summary"
    :class="{ clickable: hasOutput }"
    :title="hasOutput ? '点击查看输出' : ''"
    @click="toggleExpand"
    @keydown.enter.prevent="toggleExpand"
    @keydown.space.prevent="toggleExpand"
    :tabindex="hasOutput ? 0 : -1"
    :role="hasOutput ? 'button' : undefined"
  >
    <div class="timeline-left">
      <CheckIcon size="14" class="timeline-check" />
      <span v-if="commandDescriptor.kind" class="command-kind-badge">{{ commandDescriptor.kind }}</span>
      <span class="timeline-label">
        已运行 <code :title="card.command">{{ commandDescriptor.display }}</code>
      </span>
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
        <span v-if="commandDescriptor.kind" class="command-kind-badge subtle">{{ commandDescriptor.kind }}</span>
        <span class="term-command mono" :title="card.command">{{ commandDescriptor.display }}</span>
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
        <span class="term-status-badge permission" v-if="isPermissionDenied">
          <XIcon size="12" /> Permission denied
        </span>
        <span class="term-status-badge warning" v-if="isDisconnected">
          <XIcon size="12" /> Host disconnected
        </span>
        <span class="term-status-badge warning" v-if="isHostTimeout">
          <XIcon size="12" /> Heartbeat timed out
        </span>
        <span class="term-status-badge timeout" v-if="isTimeout">
          <XIcon size="12" /> Timed out
        </span>
        <span class="term-status-badge cancelled" v-if="isCancelled">
          <XIcon size="12" /> Cancelled
        </span>
      </div>
    </div>

    <div class="term-body" v-if="isExpanded">
      <div v-if="hasOutput" class="xterm-wrapper" ref="terminalContainer" :style="{ height: dynamicHeight + 'px' }"></div>
      <div v-else class="terminal-placeholder">
        <div class="terminal-placeholder-label">{{ isRunning ? "命令执行中，等待输出..." : "该命令没有输出" }}</div>
        <div class="terminal-placeholder-command mono">{{ card.command || "Executing..." }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ====== Timeline summary line (collapsed success) ====== */
.timeline-summary {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: 12px;
  padding: 8px 0;
  margin-left: 48px;
  max-width: 800px;
}

.timeline-summary.clickable {
  cursor: pointer;
}

.timeline-summary.clickable:hover .timeline-label,
.timeline-summary.clickable:focus-visible .timeline-label {
  color: #334155;
}

.timeline-summary.clickable:focus-visible {
  outline: none;
}

.timeline-left {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text-meta, #9ca3af);
  font-size: var(--text-meta-size, 12px);
  min-width: 0;
  white-space: normal;
  flex-wrap: wrap;
}

.timeline-check {
  color: #22c55e;
}

.timeline-failed {
  color: #ef4444;
}

.timeline-label {
  color: #6b7280;
  min-width: 0;
}

.timeline-label code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  color: #374151;
  font-weight: 500;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
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
  min-width: 0;
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
  white-space: normal;
  overflow: hidden;
  font-weight: 500;
  overflow-wrap: anywhere;
  line-height: 1.45;
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

.command-kind-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  border-radius: 999px;
  background: #eff6ff;
  color: #1d4ed8;
  border: 1px solid #bfdbfe;
  font-size: 11px;
  font-weight: 700;
  line-height: 1.2;
  flex-shrink: 0;
}

.command-kind-badge.subtle {
  background: #f8fafc;
  color: #475569;
  border-color: #dbe3ee;
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

.term-status-badge.permission {
  background: #fff7ed;
  color: #c2410c;
}

.term-status-badge.warning {
  background: #fff7ed;
  color: #c2410c;
}

.term-status-badge.timeout {
  background: #fef3c7;
  color: #92400e;
}

.term-status-badge.cancelled {
  background: #e2e8f0;
  color: #475569;
}

.term-body {
  background: #0f172a;
  padding: 6px;
  border-top: 1px solid #1e293b;
}

.xterm-wrapper {
  width: 100%;
  border-radius: 6px;
  overflow: hidden;
  max-height: 250px;
}

.terminal-placeholder {
  min-height: 72px;
  border-radius: 8px;
  background: #111827;
  color: #cbd5e1;
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.terminal-placeholder-label {
  font-size: 12px;
  color: #94a3b8;
}

.terminal-placeholder-command {
  font-size: 13px;
  color: #f8fafc;
  line-height: 1.5;
  word-break: break-all;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

:deep(.xterm-viewport) {
  overflow-y: auto !important;
}

@media (max-width: 900px) {
  .timeline-summary {
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
  }

  .timeline-divider {
    display: none;
  }

  .term-header {
    align-items: flex-start;
    gap: 10px;
  }

  .term-meta {
    margin-left: 0;
    flex-wrap: wrap;
    justify-content: flex-end;
  }
}
</style>
