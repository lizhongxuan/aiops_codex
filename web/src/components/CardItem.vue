<script setup>
const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["approval"]);

function onDecision(decision) {
  if (!props.card.approval?.requestId) {
    return;
  }
  emit("approval", {
    approvalId: props.card.approval.requestId,
    decision,
  });
}

function lineClass(status) {
  if (status === "completed") {
    return "plan-item completed";
  }
  if (status === "inProgress") {
    return "plan-item active";
  }
  return "plan-item";
}

function cardClass(card) {
  if (card.type === "MessageCard" && card.role === "user") {
    return "message-card user-bubble";
  }
  if (card.type === "MessageCard") {
    return "message-card assistant-block";
  }
  if (card.type === "PlanCard") {
    return "plan-card";
  }
  if (card.type === "StepCard") {
    return "step-card";
  }
  if (card.type === "ResultCard") {
    return "result-card";
  }
  if (card.type === "CommandApprovalCard" || card.type === "FileChangeApprovalCard") {
    return "approval-card";
  }
  return "generic-card";
}

function roleLabel(card) {
  if (card.role === "user") {
    return "你";
  }
  if (card.type === "MessageCard") {
    return "Codex";
  }
  return card.type;
}
</script>

<template>
  <article class="card" :class="cardClass(card)" :data-card-type="card.type">
    <header class="card-header">
      <span class="card-type">{{ roleLabel(card) }}</span>
      <span v-if="card.status" class="card-status">{{ card.status }}</span>
    </header>

    <h3 v-if="card.title" class="card-title">{{ card.title }}</h3>

    <template v-if="card.type === 'PlanCard'">
      <ul class="plan-list">
        <li
          v-for="item in card.items"
          :key="`${card.id}-${item.step}`"
          :class="lineClass(item.status)"
        >
          {{ item.step }}
        </li>
      </ul>
    </template>

    <template v-else-if="card.type === 'StepCard'">
      <p v-if="card.command" class="mono">{{ card.command }}</p>
      <p v-if="card.cwd" class="subtle">{{ card.cwd }}</p>
      <pre v-if="card.output" class="output">{{ card.output }}</pre>
      <ul v-if="card.changes?.length" class="changes">
        <li v-for="change in card.changes" :key="`${card.id}-${change.path}`">
          <span class="mono">{{ change.path }}</span>
          <span class="change-kind">{{ change.kind }}</span>
        </li>
      </ul>
    </template>

    <template
      v-else-if="
        card.type === 'CommandApprovalCard' || card.type === 'FileChangeApprovalCard'
      "
    >
      <p v-if="card.command" class="mono">{{ card.command }}</p>
      <p v-if="card.cwd" class="subtle">{{ card.cwd }}</p>
      <p v-if="card.text" class="card-text">{{ card.text }}</p>
      <ul v-if="card.changes?.length" class="changes">
        <li v-for="change in card.changes" :key="`${card.id}-${change.path}`">
          <div class="change-path mono">{{ change.path }}</div>
          <div class="change-kind">{{ change.kind }}</div>
          <pre v-if="change.diff" class="output">{{ change.diff }}</pre>
        </li>
      </ul>
      <div v-if="card.approval && card.status === 'pending'" class="approval-actions">
        <button class="primary" @click="onDecision('accept')">Approve once</button>
        <button @click="onDecision('decline')">Decline</button>
      </div>
    </template>

    <template v-else>
      <p v-if="card.text" class="card-text">{{ card.text }}</p>
      <pre v-if="card.output" class="output">{{ card.output }}</pre>
    </template>
  </article>
</template>
