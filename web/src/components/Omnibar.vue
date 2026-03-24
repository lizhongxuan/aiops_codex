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
});

const emit = defineEmits(["update:modelValue", "send"]);
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
    emit("send");
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
  <div class="omnibar-wrapper">
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
      :disabled="!store.canSend || store.sending"
    ></textarea>
    
    <div class="omnibar-tools">
      <div class="tools-left" v-if="activeMentions.length">
         <span v-for="mention in activeMentions" :key="mention" class="pill-tag"><span class="pill-icon">@</span> {{ mention }}</span>
      </div>
      <div class="tools-right">
         <span class="hint-text">⌘ ↵ 发送</span>
         <button class="send-btn" :disabled="!store.canSend || store.sending" @click="emit('send')">
           <span v-if="store.sending" class="spinner-small"></span>
           <span v-else>↑</span>
         </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.omnibar-wrapper {
  max-width: 900px;
  margin: 0 auto;
  background: var(--omnibar-bg);
  border: 1px solid var(--border-color);
  border-radius: 20px;
  padding: 12px 14px;
  box-shadow: 0 8px 30px rgba(15, 23, 42, 0.08);
  display: flex;
  flex-direction: column;
  gap: 8px;
  transition: box-shadow 0.2s, border-color 0.2s;
  position: relative;
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
  font-size: 15px;
  line-height: 1.6;
  padding: 0 4px;
  color: var(--text-main);
  font-family: inherit;
}

.omnibar-input::placeholder {
  color: #94a3b8;
}

.omnibar-tools {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.tools-left {
  display: flex;
  gap: 8px;
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

.pill-icon {
  color: var(--primary);
  font-weight: 700;
}

.tools-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.hint-text {
  font-size: 11px;
  color: #94a3b8;
  font-weight: 500;
}

.send-btn {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: var(--text-main);
  color: white;
  border: none;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  cursor: pointer;
  transition: transform 0.1s, background 0.2s;
}

.send-btn:hover:not(:disabled) {
  background: #1e293b;
  transform: translateY(-1px);
}

.send-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
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
