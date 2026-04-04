<script setup>
import MessageCard from "../MessageCard.vue";
import ProtocolProcessFold from "./ProtocolProcessFold.vue";
import McpBundleHost from "../mcp/McpBundleHost.vue";
import McpUiCardHost from "../mcp/McpUiCardHost.vue";

const props = defineProps({
  turn: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["select-message", "select-process-item", "action", "detail", "pin", "refresh"]);

function selectMessage(message, event) {
  if (!message || event?.target?.closest("button")) return;
  emit("select-message", message);
}

function selectProcessItem(item) {
  emit("select-process-item", {
    item,
    turn: props.turn,
  });
}

function emitAction(payload) {
  emit("action", payload);
}

function handleBundleAction(payload) {
  const isTopLevelBundleRefresh =
    payload &&
    typeof payload === "object" &&
    (
      payload.bundleId ||
      payload.bundleKind ||
      payload.sections ||
      payload.target?.dataset?.testid === "mcp-bundle-action" ||
      payload.currentTarget?.dataset?.testid === "mcp-bundle-action"
    );

  if (isTopLevelBundleRefresh) {
    emit("refresh", payload);
    return;
  }
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
  <article class="protocol-turn-group" :data-testid="`protocol-turn-${turn.id}`">
    <div
      v-if="turn.userMessage"
      class="stream-row protocol-stream-row row-user protocol-turn-user"
      @click="selectMessage(turn.userMessage, $event)"
    >
      <MessageCard :card="turn.userMessage.card" />
    </div>

    <ProtocolProcessFold :turn="turn" @item-select="selectProcessItem" />

    <div v-if="turn.finalMessage" class="protocol-turn-final">
      <div v-if="turn.finalLabel" class="protocol-final-divider">
        <span class="protocol-final-divider-line" />
        <span class="protocol-final-divider-label">{{ turn.finalLabel }}</span>
        <span class="protocol-final-divider-line" />
      </div>

      <div class="stream-row protocol-stream-row row-assistant" @click="selectMessage(turn.finalMessage, $event)">
        <MessageCard :card="turn.finalMessage.card" />
      </div>
    </div>

    <div v-if="turn.resultBundles?.length" class="protocol-turn-bundles">
      <McpBundleHost
        v-for="bundle in turn.resultBundles"
        :key="bundle.id"
        :bundle="bundle.model"
        @action="handleBundleAction"
        @open-detail="emitDetail"
        @pin="emitPin"
      />
    </div>

    <div v-if="turn.actionSurfaces?.length" class="protocol-turn-actions">
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
.protocol-turn-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.protocol-turn-final {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 2px;
}

.protocol-turn-bundles,
.protocol-turn-actions {
  display: grid;
  gap: 12px;
}

.protocol-final-divider {
  display: flex;
  align-items: center;
  gap: 12px;
}

.protocol-final-divider-line {
  flex: 1;
  height: 1px;
  background: rgba(226, 232, 240, 0.82);
}

.protocol-final-divider-label {
  color: #64748b;
  font-size: 12px;
  font-weight: 600;
  line-height: 1.4;
}
</style>
