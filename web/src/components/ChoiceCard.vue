<script setup>
import { ref } from "vue";
import { ListIcon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["choice"]);

const selectedIndex = ref(0);

function onSubmit() {
  const selected = props.card.options?.[selectedIndex.value];
  if (!selected) return;
  emit("choice", {
    requestId: props.card.requestId,
    selected: selected,
    index: selectedIndex.value,
  });
}
</script>

<template>
  <div class="choice-card">
    <div class="choice-header">
      <ListIcon size="16" class="choice-icon" />
      <span class="choice-title">{{ card.title || '请选择' }}</span>
    </div>

    <div class="choice-body">
      <p class="choice-question" v-if="card.question">{{ card.question }}</p>

      <div class="choice-options" v-if="card.status === 'pending'">
        <label
          v-for="(option, idx) in card.options"
          :key="idx"
          class="choice-option"
          :class="{ selected: selectedIndex === idx }"
          @click="selectedIndex = idx"
        >
          <span class="option-radio">
            <span class="radio-dot" v-if="selectedIndex === idx"></span>
          </span>
          <span class="option-label">{{ typeof option === 'string' ? option : option.label }}</span>
        </label>
      </div>

      <div class="choice-footer" v-if="card.status === 'pending'">
        <button class="submit-btn" @click="onSubmit">
          提交 ↵
        </button>
      </div>

      <div v-if="card.status !== 'pending'" class="choice-resolved">
        已完成选择
      </div>
    </div>
  </div>
</template>

<style scoped>
.choice-card {
  border-radius: 16px;
  background: #ffffff;
  border: 1px solid #e5e7eb;
  overflow: hidden;
  margin-top: 4px;
  margin-left: 48px;
  max-width: 680px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.03);
}

.choice-header {
  padding: 14px 20px;
  display: flex;
  align-items: center;
  gap: 10px;
  background: #f9fafb;
  border-bottom: 1px solid #f3f4f6;
}

.choice-icon {
  color: #6b7280;
}

.choice-title {
  font-size: 14px;
  font-weight: 600;
  color: #1f2937;
}

.choice-body {
  padding: 16px 20px;
}

.choice-question {
  margin: 0 0 14px;
  font-size: 14px;
  line-height: 1.6;
  color: #374151;
}

.choice-options {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 16px;
}

.choice-option {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  border-radius: 10px;
  cursor: pointer;
  font-size: 14px;
  color: #374151;
  transition: background 0.15s;
  min-height: 44px;
}

.choice-option:hover {
  background: #f9fafb;
}

.choice-option.selected {
  background: #f3f4f6;
}

.option-radio {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 2px solid #d1d5db;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: border-color 0.15s;
}

.choice-option.selected .option-radio {
  border-color: #0f172a;
}

.radio-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #0f172a;
}

.option-label {
  line-height: 1.5;
}

.choice-footer {
  display: flex;
  justify-content: flex-end;
}

.submit-btn {
  padding: 8px 20px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 600;
  border: none;
  cursor: pointer;
  background: #0f172a;
  color: white;
  transition: background 0.2s, transform 0.1s;
}

.submit-btn:hover {
  background: #1e293b;
}

.submit-btn:active {
  transform: translateY(1px);
}

.choice-resolved {
  text-align: center;
  padding: 12px;
  background: #f9fafb;
  border-radius: 8px;
  color: #6b7280;
  font-size: 13px;
  font-weight: 500;
}
</style>
