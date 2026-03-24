<script setup>
import { ClipboardCheckIcon } from "lucide-vue-next";

defineProps({
  card: {
    type: Object,
    required: true,
  },
});
</script>

<template>
  <div class="result-card">
    <div class="result-header">
      <ClipboardCheckIcon size="16" class="result-icon" />
      <span class="result-title">{{ card.title || '执行结果' }}</span>
    </div>

    <div class="result-body">
      <p class="result-summary" v-if="card.summary || card.text">{{ card.summary || card.text }}</p>

      <div class="kv-table" v-if="card.kvRows?.length">
        <div class="kv-row" v-for="(row, idx) in card.kvRows" :key="idx">
          <span class="kv-key">{{ row.key }}</span>
          <span class="kv-val">{{ row.value }}</span>
        </div>
      </div>

      <div class="result-highlights" v-if="card.highlights?.length">
        <span class="highlight-pill" v-for="(h, idx) in card.highlights" :key="idx">{{ h }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.result-card {
  border-radius: 16px;
  background: #ffffff;
  border: 1px solid #e5e7eb;
  overflow: hidden;
  margin-top: 4px;
  margin-left: 48px;
  max-width: 680px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.03);
}

.result-header {
  padding: 14px 20px;
  display: flex;
  align-items: center;
  gap: 10px;
  background: #f0fdf4;
  border-bottom: 1px solid #dcfce7;
}

.result-icon {
  color: #16a34a;
}

.result-title {
  font-size: 14px;
  font-weight: 600;
  color: #166534;
}

.result-body {
  padding: 16px 20px;
}

.result-summary {
  margin: 0 0 14px;
  font-size: 15px;
  line-height: 1.7;
  color: #1f2937;
}

.kv-table {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-bottom: 14px;
}

.kv-row {
  display: flex;
  justify-content: space-between;
  padding: 8px 12px;
  border-radius: 8px;
  font-size: 13px;
}

.kv-row:nth-child(odd) {
  background: #f9fafb;
}

.kv-key {
  color: #6b7280;
  font-weight: 500;
}

.kv-val {
  color: #111827;
  font-weight: 600;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.result-highlights {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.highlight-pill {
  display: inline-block;
  padding: 4px 12px;
  border-radius: 9999px;
  background: #eff6ff;
  color: #1e40af;
  font-size: 12px;
  font-weight: 600;
}
</style>
