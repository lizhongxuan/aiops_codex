<script setup>
import { computed } from "vue";
import MessageCard from "./MessageCard.vue";
import PlanCard from "./PlanCard.vue";
import TerminalCard from "./TerminalCard.vue";
import CodeCard from "./CodeCard.vue";
import AuthCard from "./AuthCard.vue";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["approval"]);

function handleApproval(payload) {
  emit("approval", payload);
}

const isTerminal = computed(() => {
  return props.card.type === "StepCard" && !!props.card.command;
});

const isCode = computed(() => {
  return props.card.type === "StepCard" && !props.card.command && props.card.changes?.length > 0;
});
</script>

<template>
  <div class="card-root">
    <template v-if="card.type === 'MessageCard' || (card.type === 'StepCard' && !isTerminal && !isCode)">
      <MessageCard :card="card" />
    </template>
    
    <template v-else-if="card.type === 'PlanCard'">
      <PlanCard :card="card" />
    </template>
    
    <template v-else-if="card.type === 'CommandApprovalCard' || card.type === 'FileChangeApprovalCard'">
      <AuthCard :card="card" @approval="handleApproval" />
    </template>
    
    <template v-else-if="isTerminal">
      <TerminalCard :card="card" />
    </template>
    
    <template v-else-if="isCode">
      <CodeCard :card="card" />
    </template>
    
    <template v-else>
      <!-- Fallback generic renderer -->
      <div class="generic-card">
        <p v-if="card.text">{{ card.text }}</p>
        <pre v-if="card.output" class="mono">{{ card.output }}</pre>
      </div>
    </template>
  </div>
</template>

<style scoped>
.card-root {
  width: 100%;
}

.generic-card {
  margin-top: 8px;
  margin-left: 48px;
  padding: 16px;
  background: white;
  border-radius: 12px;
  border: 1px dashed #cbd5e1;
  font-size: 14px;
}
.mono {
  font-family: monospace;
}
</style>
