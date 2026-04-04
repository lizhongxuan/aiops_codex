<script setup>
import MessageCard from "../MessageCard.vue";
import ChatProcessFold from "./ChatProcessFold.vue";
import McpBundleHost from "../mcp/McpBundleHost.vue";
import McpUiCardHost from "../mcp/McpUiCardHost.vue";

defineProps({
  turn: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["action", "detail", "pin", "refresh"]);

function emitAction(payload) {
  emit("action", payload);
}

function emitDetail(payload) {
  emit("detail", payload);
}

function emitPin(payload) {
  emit("pin", payload);
}

function emitRefresh(payload) {
  emit("refresh", payload);
}
</script>

<template>
  <article class="chat-turn-group" :data-testid="`chat-turn-${turn.id}`">
    <div v-if="turn.userMessage" class="stream-row row-user">
      <MessageCard :card="turn.userMessage.card" />
    </div>

    <ChatProcessFold :turn="turn" />

    <div v-if="turn.finalMessage" class="chat-turn-final">
      <div v-if="turn.finalLabel" class="chat-turn-final-divider">
        <span class="chat-turn-final-divider-line" />
        <span class="chat-turn-final-divider-label">{{ turn.finalLabel }}</span>
        <span class="chat-turn-final-divider-line" />
      </div>

      <div class="stream-row row-assistant">
        <MessageCard :card="turn.finalMessage.card" />
      </div>
    </div>

    <div v-if="turn.resultBundles?.length" class="chat-turn-bundles">
      <McpBundleHost
        v-for="bundle in turn.resultBundles"
        :key="bundle.id"
        :bundle="bundle.model"
        @action="emitAction"
        @open-detail="emitDetail"
        @pin="emitPin"
      />
    </div>

    <div v-if="turn.actionSurfaces?.length" class="chat-turn-actions">
      <McpUiCardHost
        v-for="surface in turn.actionSurfaces"
        :key="surface.id"
        :card="surface.model"
        @action="emitAction"
        @detail="emitDetail"
        @refresh="emitRefresh"
      />
    </div>
  </article>
</template>

<style scoped>
.chat-turn-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.chat-turn-final {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 1px;
}

.chat-turn-bundles,
.chat-turn-actions {
  display: grid;
  gap: 10px;
}

.chat-turn-final-divider {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-left: 30px;
  max-width: 720px;
}

.chat-turn-final-divider-line {
  flex: 1;
  height: 1px;
  background: rgba(226, 232, 240, 0.82);
}

.chat-turn-final-divider-label {
  color: #64748b;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.4;
}
</style>
