<script setup>
import { ref, onMounted, onBeforeUnmount, watch, computed } from "vue";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { ChevronRightIcon, ChevronDownIcon, TerminalIcon, CheckIcon } from "lucide-vue-next";
import "@xterm/xterm/css/xterm.css";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const isExpanded = ref(true);
const terminalContainer = ref(null);
let term = null;
let fitAddon = null;

const isComplete = computed(() => {
  return props.card.status === "completed" || props.card.status === "error";
});

const hasOutput = computed(() => !!props.card.output);

function toggleExpand() {
  if (hasOutput.value) {
    isExpanded.value = !isExpanded.value;
    // Need to let DOM render before fitting
    if (isExpanded.value) {
      setTimeout(() => fitAddon?.fit(), 10);
    }
  }
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
  
  // Normalize output with xterm formatting
  const formattedOutput = props.card.output.replace(/\n/g, "\r\n");
  term.write(formattedOutput);
}

// Watch for DOM creation or Output changes
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
      term.write(newOutput ? newOutput.replace(/\n/g, "\r\n") : "");
      fitAddon?.fit();
    }
  }
);

onMounted(() => {
  // If completed, we auto-collapse it on mount according to user request (minimal state)
  if (isComplete.value) {
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
  <div class="terminal-card" :class="{'minimal': !isExpanded}">
    <div class="term-header" @click="toggleExpand">
      <div class="term-title-group">
        <component :is="isExpanded ? ChevronDownIcon : ChevronRightIcon" size="16" class="icon-carat" />
        <TerminalIcon size="14" class="icon-term" />
        <span class="term-command mono">{{ card.command || "Executing..." }}</span>
      </div>
      
      <div class="term-meta">
        <span class="term-cwd" v-if="card.cwd">{{ card.cwd }}</span>
        <span class="term-status-badge success" v-if="card.status === 'completed'">
          <CheckIcon size="12" /> Success
        </span>
        <span class="term-status-badge error" v-else-if="card.status === 'error'">Failed</span>
      </div>
    </div>
    
    <div class="term-body" v-if="isExpanded && hasOutput">
      <div class="xterm-wrapper" ref="terminalContainer"></div>
    </div>
  </div>
</template>

<style scoped>
.terminal-card {
  border-radius: 12px;
  background: #ffffff;
  border: 1px solid #e2e8f0;
  overflow: hidden;
  margin-top: 8px;
  margin-left: 48px; /* align with message bubble */
  max-width: 800px;
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.03);
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
  padding: 10px 14px;
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
  padding: 12px;
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
