<script setup>
import { computed, ref } from "vue";
import { UserIcon, BotIcon, CopyIcon, CheckIcon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const isUser = computed(() => props.card.role === "user");
const messageText = computed(() => props.card.text || props.card.title || "");

const avatarIcon = computed(() => {
  return isUser.value ? UserIcon : BotIcon;
});

const renderAsCode = computed(() => {
  if (isUser.value) return false;
  return looksStructuredText(messageText.value);
});

function looksStructuredText(value) {
  const trimmed = value.trim();
  if (!trimmed.includes("\n")) return false;
  const lines = trimmed
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
  if (lines.length < 2) return false;

  let structuredCount = 0;
  for (const line of lines) {
    if (
      /^[./~\w-][./~\w\s-]*$/.test(line) ||
      /[\\/]/.test(line) ||
      /\.[A-Za-z0-9_-]+$/.test(line) ||
      /^[A-Za-z0-9_.-]+$/.test(line)
    ) {
      structuredCount += 1;
    }
  }
  return structuredCount / lines.length >= 0.6;
}

const parsedMessageChunks = computed(() => {
  const text = messageText.value;
  if (!text) return [];
  
  const regex = /\[([^\]]+)\]\(([^)]+)\)/g;
  let match;
  let lastIndex = 0;
  const chunks = [];
  
  while ((match = regex.exec(text)) !== null) {
    if (match.index > lastIndex) {
      chunks.push({ type: 'text', content: text.substring(lastIndex, match.index) });
    }
    chunks.push({ type: 'link', label: match[1], path: match[2] });
    lastIndex = regex.lastIndex;
  }
  
  if (lastIndex < text.length) {
    chunks.push({ type: 'text', content: text.substring(lastIndex) });
  }
  
  return chunks.length > 0 ? chunks : [{ type: 'text', content: text }];
});

const isCopied = ref(false);

async function handleCopy() {
  if (!messageText.value || isCopied.value) return;
  try {
    await navigator.clipboard.writeText(messageText.value);
    isCopied.value = true;
    setTimeout(() => {
      isCopied.value = false;
    }, 2000);
  } catch (err) {
    console.error("Failed to copy:", err);
  }
}
</script>

<template>
  <div class="message-wrapper" :class="{ 'is-user': isUser }">
    <div class="avatar" v-if="!isUser">
      <BotIcon size="20" />
    </div>
    
    <div class="message-content">
      <div class="content-block relative-block" v-if="!isUser">
        <pre v-if="renderAsCode" class="message-code">{{ messageText }}</pre>
        <div v-else class="message-text">
          <template v-for="(chunk, idx) in parsedMessageChunks" :key="idx">
            <span v-if="chunk.type === 'text'">{{ chunk.content }}</span>
            <span v-else-if="chunk.type === 'link'" class="file-link-capsule" :title="chunk.path">
              {{ chunk.label }}
            </span>
          </template>
        </div>
        <button class="copy-btn" @click="handleCopy" :class="{ copied: isCopied }" title="Copy">
          <CheckIcon v-if="isCopied" size="14" class="text-green-500" />
          <CopyIcon v-else size="14" />
          <span v-if="isCopied" class="copy-tooltip">✓ 复制成功</span>
        </button>
      </div>
      <template v-else>
        <pre v-if="renderAsCode" class="message-code">{{ messageText }}</pre>
        <div v-else class="message-text">
          <template v-for="(chunk, idx) in parsedMessageChunks" :key="idx">
            <span v-if="chunk.type === 'text'">{{ chunk.content }}</span>
            <span v-else-if="chunk.type === 'link'" class="file-link-capsule" :title="chunk.path">
              {{ chunk.label }}
            </span>
          </template>
        </div>
      </template>
      <div class="ghost-loader" v-if="card.status === 'inProgress'">
        <span class="spinner-small"></span> 
        <span class="ghost-text">Thinking...</span>
      </div>
    </div>
    
    <div class="avatar user-avatar" v-if="isUser">
      <UserIcon size="20" />
    </div>
  </div>
</template>

<style scoped>
.message-wrapper {
  display: flex;
  gap: 16px;
  max-width: 100%;
  width: 100%;
}

.message-wrapper.is-user {
  justify-content: flex-end;
}

.avatar {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: white;
  border: 1px solid #e2e8f0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  flex-shrink: 0;
}

.user-avatar {
  background: #f8fafc;
}

.message-content {
  flex: 1;
  max-width: calc(100% - 48px);
}

.is-user .message-content {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
}

.message-text {
  font-size: var(--text-body, 15px);
  line-height: var(--line-height-body, 1.7);
  color: #0f172a;
  white-space: pre-wrap;
}

.message-code {
  margin: 0;
  padding: 14px 16px;
  border-radius: 14px;
  border: 1px solid #dbe3ee;
  background: #f8fafc;
  color: #0f172a;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 13px;
  line-height: 1.65;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.is-user .message-text {
  background: #f3f4f6;
  padding: 14px 20px;
  border-radius: var(--radius-card, 16px);
  color: #0f172a;
  display: inline-block;
}

.is-user .message-code {
  background: #f3f4f6;
  border-color: transparent;
}

.ghost-loader {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  color: #94a3b8;
}

.ghost-text {
  font-size: 13px;
  font-style: italic;
}


.spinner-small {
  display: inline-block;
  width: 12px;
  height: 12px;
  border: 2px solid rgba(0,0,0,0.1);
  border-radius: 50%;
  border-top-color: currentColor;
  animation: spin 1s linear infinite;
}

@keyframes spin { 
  to { transform: rotate(360deg); }
}

.relative-block {
  position: relative;
  display: inline-block;
  max-width: 100%;
}

.copy-btn {
  position: absolute;
  bottom: 8px;
  right: 8px;
  background: white;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  padding: 6px;
  color: #6b7280;
  cursor: pointer;
  opacity: 0;
  transition: all 0.2s ease;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 2px 4px rgba(0,0,0,0.05);
}

.relative-block:hover .copy-btn {
  opacity: 1;
}

.copy-btn:hover {
  background: #f9fafb;
  color: #111827;
}

.copy-btn.copied {
  opacity: 1;
  border-color: #22c55e;
  background: #f0fdf4;
  color: #15803d;
}

.text-green-500 {
  color: #22c55e;
}

.copy-tooltip {
  position: absolute;
  bottom: 110%;
  right: 0;
  background: #111827;
  color: white;
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 11px;
  white-space: nowrap;
  pointer-events: none;
  animation: fadeIn 0.2s ease;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: translateY(0); }
}

.file-link-capsule {
  display: inline-flex;
  align-items: center;
  background-color: #eff6ff;
  color: #2563eb;
  padding: 2px 8px;
  border-radius: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 13px;
  margin: 0 4px;
  cursor: help;
  border: 1px solid #bfdbfe;
  transition: all 0.2s ease;
  vertical-align: middle;
}

.file-link-capsule:hover {
  background-color: #dbeafe;
  border-color: #93c5fd;
  color: #1d4ed8;
}
</style>
