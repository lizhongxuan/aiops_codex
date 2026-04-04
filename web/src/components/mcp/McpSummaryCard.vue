<script setup>
import McpReadonlyCardFrame from "./McpReadonlyCardFrame.vue";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
  embedded: {
    type: Boolean,
    default: false,
  },
});

const emit = defineEmits(["detail", "refresh"]);

function forwardDetail(payload) {
  emit("detail", payload);
}

function forwardRefresh(payload) {
  emit("refresh", payload);
}
</script>

<template>
  <McpReadonlyCardFrame
    :card="card"
    :embedded="embedded"
    @detail="forwardDetail"
    @refresh="forwardRefresh"
  >
    <article class="summary-shell" data-testid="mcp-summary-card">
      <p class="summary-copy">{{ card.summary || "暂无摘要，可展开查看详情。" }}</p>
    </article>
  </McpReadonlyCardFrame>
</template>

<style scoped>
.summary-shell {
  padding: 12px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.92);
  border: 1px solid rgba(15, 23, 42, 0.06);
}

.summary-copy {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
  color: #334155;
}
</style>
