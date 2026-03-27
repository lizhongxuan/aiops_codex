<script setup>
import { ref, nextTick, computed } from "vue";
import { useAppStore } from "../store";

const props = defineProps({
  modelValue: {
    type: String,
    default: "",
  },
  placeholder: {
    type: String,
    default: "",
  },
  disabled: {
    type: Boolean,
    default: false,
  },
  isDockedBottom: {
    type: Boolean,
    default: false,
  },
  allowFollowUp: {
    type: Boolean,
    default: false,
  },
});

const emit = defineEmits(["update:modelValue", "send", "stop"]);
const store = useAppStore();

const textareaRef = ref(null);
const mentionPopover = ref({
  visible: false,
  query: "",
  x: 0,
  y: 0,
  selectedIndex: 0,
});

const mentionOptions = computed(() => {
  const query = mentionPopover.value.query.toLowerCase();

  return store.snapshot.hosts
    .map((host) => ({ type: "host", id: host.id, label: host.name }))
    .filter((option) => option.label.toLowerCase().includes(query))
    .slice(0, 5);
});

const activeMentions = computed(() => {
  const mentions = [];
  const seen = new Set();
  for (const match of props.modelValue.matchAll(/@([A-Za-z0-9._-]+)/g)) {
    const label = match[1];
    if (seen.has(label)) continue;
    seen.add(label);
    mentions.push(label);
  }
  return mentions;
});

const canStop = computed(() => store.runtime.turn.active);
const followUpMode = computed(() => props.allowFollowUp && canStop.value);
const primaryAction = computed(() => (canStop.value && !followUpMode.value ? "stop" : "send"));
const inputDisabled = computed(() => props.disabled || !store.canSend || store.sending || (canStop.value && !followUpMode.value ? true : false));
const sendDisabled = computed(() => props.disabled || !store.canSend || store.sending || !props.modelValue.trim());
const showSecondaryStop = computed(() => followUpMode.value);
const hintText = computed(() => {
  if (primaryAction.value === "stop") return "停止当前任务";
  if (followUpMode.value) return "⌘ ↵ 发送 follow-up";
  return "⌘ ↵ 发送";
});

function onInput(e) {
  const text = e.target.value;
  emit("update:modelValue", text);

  const cursor = e.target.selectionStart;
  const textBeforeCursor = text.slice(0, cursor);
  const match = textBeforeCursor.match(/@([A-Za-z0-9._-]*)$/);

  if (match) {
    const coords = getCaretCoordinates(e.target, cursor);
    mentionPopover.value = {
      visible: true,
      query: match[1],
      x: coords.left,
      y: coords.top - 160, // approximate popover height above
      selectedIndex: 0,
    };
    if (!mentionOptions.value.length) {
      mentionPopover.value.visible = false;
    }
  } else {
    mentionPopover.value.visible = false;
  }
}

function onKeydown(e) {
  if (mentionPopover.value.visible && !mentionOptions.value.length) {
    mentionPopover.value.visible = false;
  }

  if (mentionPopover.value.visible) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      mentionPopover.value.selectedIndex = (mentionPopover.value.selectedIndex + 1) % mentionOptions.value.length;
      return;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      mentionPopover.value.selectedIndex = (mentionPopover.value.selectedIndex - 1 + mentionOptions.value.length) % mentionOptions.value.length;
      return;
    }
    if (e.key === "Enter" || e.key === "Tab") {
      e.preventDefault();
      selectMention(mentionOptions.value[mentionPopover.value.selectedIndex]);
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      mentionPopover.value.visible = false;
      return;
    }
  }
  
  if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
    e.preventDefault();
    if (primaryAction.value === "stop") {
      emit("stop");
    } else if (!sendDisabled.value) {
      emit("send");
    }
  }
}

function selectMention(option) {
  if (!option) return;

  const text = props.modelValue;
  const cursor = textareaRef.value.selectionStart;
  const textBeforeCursor = text.slice(0, cursor);
  const match = textBeforeCursor.match(/@([A-Za-z0-9._-]*)$/);

  if (match) {
    const newText = text.slice(0, match.index) + `@${option.label} ` + text.slice(cursor);
    emit("update:modelValue", newText);
    mentionPopover.value.visible = false;

    nextTick(() => {
      textareaRef.value.focus();
      const newCursorPos = match.index + option.label.length + 2;
      textareaRef.value.setSelectionRange(newCursorPos, newCursorPos);
    });
  }
}

// Helper to get caret coords in textarea (naive approach for MVP)
function getCaretCoordinates(element, position) {
  // Simple approximation without creating mirror div
  return { left: 24, top: 0 }; 
}
</script>

<template>
  <div class="omnibar-wrapper" :class="{ 'is-docked-bottom': isDockedBottom }">
    <!-- Popover -->
    <div class="mention-popover" v-if="mentionPopover.visible && mentionOptions.length" :style="{ left: mentionPopover.x + 'px', bottom: '100%' }">
      <div class="popover-header">Hosts</div>
      <div class="popover-list">
        <div 
          v-for="(opt, idx) in mentionOptions" 
          :key="opt.id"
          class="popover-item"
          :class="{ active: idx === mentionPopover.selectedIndex }"
          @click="selectMention(opt)"
        >
          <span class="type-badge">{{ opt.type }}</span>
          {{ opt.label }}
        </div>
      </div>
    </div>

    <textarea
      ref="textareaRef"
      :value="modelValue"
      @input="onInput"
      @keydown="onKeydown"
      rows="3"
      class="omnibar-input"
      :placeholder="placeholder"
      :disabled="inputDisabled"
    ></textarea>
    
    <div class="omnibar-tools">
      <div class="tools-left" v-if="activeMentions.length">
         <span v-for="mention in activeMentions" :key="mention" class="pill-tag"><span class="pill-icon">@</span> {{ mention }}</span>
      </div>
      <div class="tools-right">
         <span class="hint-text">{{ hintText }}</span>
         <div class="action-group">
           <button
             class="send-btn"
             :class="{ 'stop-btn': primaryAction === 'stop' }"
             :disabled="primaryAction === 'stop' ? false : sendDisabled"
             @click="primaryAction === 'stop' ? emit('stop') : emit('send')"
           >
             <span v-if="primaryAction === 'stop'">■</span>
             <span v-else-if="store.sending" class="spinner-small"></span>
             <span v-else>↑</span>
           </button>
           <button v-if="showSecondaryStop" type="button" class="stop-link-btn" @click="emit('stop')">停止</button>
         </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.omnibar-wrapper {
  width: 100%;
  max-width: 860px;
  margin: 0 auto;
  background: var(--omnibar-bg);
  border: 1px solid var(--border-color);
  border-radius: 18px;
  padding: 12px 14px 11px;
  box-shadow: 0 6px 24px rgba(15, 23, 42, 0.07);
  display: flex;
  flex-direction: column;
  gap: 8px;
  transition: box-shadow 0.2s, border-color 0.2s;
  position: relative;
}

.omnibar-wrapper.is-docked-bottom {
  border-top-left-radius: 0;
  border-top-right-radius: 0;
  border-top-color: transparent;
}

.omnibar-wrapper:focus-within {
  border-color: #cbd5e1;
  box-shadow: 0 12px 40px rgba(15, 23, 42, 0.12);
  background: #ffffff;
}

.omnibar-input {
  width: 100%;
  border: none;
  background: transparent;
  resize: none;
  outline: none;
  min-height: 78px;
  font-size: 15px;
  line-height: 1.55;
  padding: 2px 4px 0;
  color: var(--text-main);
  font-family: inherit;
}

.omnibar-input::placeholder {
  color: #94a3b8;
}

.omnibar-input:disabled {
  color: #64748b;
}

.omnibar-tools {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.tools-left {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.pill-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  background: rgba(15, 23, 42, 0.06);
  color: var(--text-main);
  padding: 4px 10px;
  border-radius: 9999px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.tools-right {
  display: inline-flex;
  align-items: center;
  gap: 12px;
}

.action-group {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.hint-text {
  font-size: 11px;
  color: #94a3b8;
}

.send-btn {
  width: 36px;
  height: 36px;
  border-radius: 999px;
  border: none;
  background: #0f172a;
  color: white;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: transform 0.15s ease, opacity 0.15s ease, background 0.15s ease;
}

.send-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.send-btn:not(:disabled):hover {
  transform: translateY(-1px);
}

.send-btn.stop-btn {
  background: #dc2626;
}

.stop-link-btn {
  border: 1px solid #fecaca;
  background: #fff;
  color: #b91c1c;
  border-radius: 999px;
  padding: 7px 10px;
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
}

.stop-link-btn:hover {
  background: #fef2f2;
}

.pill-icon {
  color: var(--primary);
  font-weight: 700;
}

.mention-popover {
  position: absolute;
  margin-bottom: 12px;
  width: 260px;
  background: white;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  box-shadow: 0 12px 32px rgba(15, 23, 42, 0.1);
  overflow: hidden;
  z-index: 100;
}

.popover-header {
  padding: 8px 12px;
  font-size: 11px;
  font-weight: 600;
  color: #64748b;
  background: #f8fafc;
  border-bottom: 1px solid #f1f5f9;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.popover-list {
  display: flex;
  flex-direction: column;
  padding: 4px;
}

.popover-item {
  padding: 8px 12px;
  font-size: 13px;
  color: #0f172a;
  border-radius: 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 8px;
}

.popover-item:hover, .popover-item.active {
  background: #f1f5f9;
}

.type-badge {
  font-size: 9px;
  padding: 2px 4px;
  border-radius: 4px;
  background: #e2e8f0;
  color: #475569;
  text-transform: uppercase;
}

.spinner-small {
  display: inline-block;
  width: 14px; height: 14px;
  border: 2px solid rgba(255,255,255,0.3);
  border-radius: 50%;
  border-top-color: white;
  animation: spin 1s linear infinite;
}

@keyframes spin { 
  to { transform: rotate(360deg); }
}
</style>
