<script setup>
import { computed, ref, watch } from "vue";
import { ListIcon } from "lucide-vue-next";

const OTHER_OPTION_VALUE = "__other__";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
  sessionKind: {
    type: String,
    default: "",
  },
});

const emit = defineEmits(["choice"]);

const selectedValues = ref([]);
const otherValues = ref([]);

const choiceQuestions = computed(() => {
  if (props.card.questions?.length) {
    return props.card.questions;
  }
  if (props.card.question || props.card.options?.length) {
    return [
      {
        header: props.card.title || "",
        question: props.card.question || "",
        options: props.card.options || [],
        isOther: false,
        isSecret: false,
      },
    ];
  }
  return [];
});

const resolvedSummary = computed(() => props.card.answerSummary || []);
const contextLabel = computed(() => (props.sessionKind === "workspace" ? "工作台输入请求" : ""));

watch(
  choiceQuestions,
  (questions) => {
    selectedValues.value = questions.map((question) => {
      if (question.options?.length) {
        return getOptionValue(question.options[0], 0);
      }
      return OTHER_OPTION_VALUE;
    });
    otherValues.value = questions.map(() => "");
  },
  { immediate: true, deep: true }
);

const canSubmit = computed(() => {
  if (props.card.status !== "pending") return false;
  return choiceQuestions.value.every((question, index) => {
    const selectedValue = selectedValues.value[index];
    if (question.options?.length && selectedValue !== OTHER_OPTION_VALUE) {
      return !!selectedValue;
    }
    return !!otherValues.value[index]?.trim();
  });
});

function getOptionValue(option, index) {
  return option?.value || option?.label || `option-${index}`;
}

function getOptionLabel(option) {
  return option?.label || option?.value || "未命名选项";
}

function getQuestionHeader(question, index) {
  if (question.header) return question.header;
  if (choiceQuestions.value.length > 1) return `问题 ${index + 1}`;
  return "";
}

function showInlineInput(question, index) {
  if (!question.options?.length) return true;
  return question.isOther && selectedValues.value[index] === OTHER_OPTION_VALUE;
}

function onSubmit() {
  if (!canSubmit.value) return;
  emit("choice", {
    requestId: props.card.requestId,
    answers: choiceQuestions.value.map((question, index) => {
      const selectedValue = selectedValues.value[index];
      if (!question.options?.length || selectedValue === OTHER_OPTION_VALUE) {
        const value = otherValues.value[index].trim();
        return {
          value,
          label: value,
          isOther: true,
        };
      }

      const selectedOption = question.options.find((option, optionIndex) => {
        return getOptionValue(option, optionIndex) === selectedValue;
      });
      return {
        value: selectedOption ? getOptionValue(selectedOption, 0) : selectedValue,
        label: selectedOption ? getOptionLabel(selectedOption) : selectedValue,
        isOther: false,
      };
    }),
  });
}
</script>

<template>
  <div class="choice-card">
    <div class="choice-header">
      <ListIcon size="16" class="choice-icon" />
      <div class="choice-title-group">
        <span v-if="contextLabel" class="choice-context">{{ contextLabel }}</span>
        <span class="choice-title">{{ card.title || "请选择" }}</span>
      </div>
    </div>

    <div class="choice-body">
      <div
        v-for="(question, index) in choiceQuestions"
        :key="`${card.id}-${index}`"
        class="choice-question-block"
      >
        <p v-if="getQuestionHeader(question, index)" class="choice-block-header">
          {{ getQuestionHeader(question, index) }}
        </p>

        <p v-if="question.question" class="choice-question">
          {{ question.question }}
        </p>

        <div v-if="card.status === 'pending' && question.options?.length" class="choice-options">
          <label
            v-for="(option, optionIndex) in question.options"
            :key="`${card.id}-${index}-${optionIndex}`"
            class="choice-option"
            :class="{ selected: selectedValues[index] === getOptionValue(option, optionIndex) }"
            @click="selectedValues[index] = getOptionValue(option, optionIndex)"
          >
            <span class="option-radio">
              <span
                v-if="selectedValues[index] === getOptionValue(option, optionIndex)"
                class="radio-dot"
              ></span>
            </span>
            <span class="option-copy">
              <span class="option-label">{{ getOptionLabel(option) }}</span>
              <span v-if="option.description" class="option-description">{{ option.description }}</span>
            </span>
          </label>

          <label
            v-if="question.isOther"
            class="choice-option"
            :class="{ selected: selectedValues[index] === OTHER_OPTION_VALUE }"
            @click="selectedValues[index] = OTHER_OPTION_VALUE"
          >
            <span class="option-radio">
              <span v-if="selectedValues[index] === OTHER_OPTION_VALUE" class="radio-dot"></span>
            </span>
            <span class="option-copy">
              <span class="option-label">其他</span>
            </span>
          </label>
        </div>

        <div v-if="card.status === 'pending' && showInlineInput(question, index)" class="choice-inline-input">
          <input
            v-model="otherValues[index]"
            class="choice-input"
            :type="question.isSecret ? 'password' : 'text'"
            :placeholder="question.isSecret ? '请输入保密内容' : '请输入内容'"
          />
        </div>
      </div>

      <div class="choice-footer" v-if="card.status === 'pending'">
        <button class="submit-btn" :disabled="!canSubmit" @click="onSubmit">
          提交 ↵
        </button>
      </div>

      <div v-if="card.status !== 'pending'" class="choice-resolved">
        <div v-if="resolvedSummary.length" class="choice-resolved-list">
          <div v-for="(entry, index) in resolvedSummary" :key="`${card.id}-resolved-${index}`">
            {{ entry }}
          </div>
        </div>
        <div v-else>已完成选择</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.choice-card {
  border-radius: 12px;
  background: #ffffff;
  border: 1px solid #e5e7eb;
  overflow: hidden;
  margin-top: 2px;
  margin-left: 36px;
  max-width: 680px;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.02);
}

.choice-header {
  padding: 10px 16px;
  display: flex;
  align-items: center;
  gap: 8px;
  background: #f9fafb;
  border-bottom: 1px solid #f3f4f6;
}

.choice-icon {
  color: #6b7280;
}

.choice-title {
  font-size: 13px;
  font-weight: 600;
  color: #1f2937;
}

.choice-title-group {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.choice-context {
  font-size: 11px;
  color: #475569;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.choice-body {
  padding: 12px 16px;
}

.choice-question-block + .choice-question-block {
  margin-top: 20px;
  padding-top: 18px;
  border-top: 1px solid #f3f4f6;
}

.choice-block-header {
  margin: 0 0 8px;
  font-size: 12px;
  font-weight: 600;
  color: #94a3b8;
  letter-spacing: 0.02em;
  text-transform: uppercase;
}

.choice-question {
  margin: 0 0 10px;
  font-size: 13px;
  line-height: 1.5;
  color: #374151;
}

.choice-options {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.choice-option {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 8px 12px;
  border-radius: 8px;
  cursor: pointer;
  font-size: 13px;
  color: #374151;
  transition: background 0.15s;
  min-height: 36px;
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
  margin-top: 1px;
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

.option-copy {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.option-label {
  line-height: 1.5;
}

.option-description {
  font-size: 12px;
  color: #94a3b8;
  line-height: 1.5;
}

.choice-inline-input {
  margin-top: 12px;
}

.choice-input {
  width: 100%;
  border-radius: 10px;
  border: 1px solid #d1d5db;
  background: #ffffff;
  padding: 11px 14px;
  font-size: 14px;
  color: #1f2937;
  outline: none;
}

.choice-input:focus {
  border-color: #0f172a;
  box-shadow: 0 0 0 3px rgba(15, 23, 42, 0.08);
}

.choice-footer {
  display: flex;
  justify-content: flex-end;
  margin-top: 18px;
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
  transition: background 0.2s, transform 0.1s, opacity 0.2s;
}

.submit-btn:hover:not(:disabled) {
  background: #1e293b;
}

.submit-btn:active:not(:disabled) {
  transform: translateY(1px);
}

.submit-btn:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.choice-resolved {
  padding: 12px 14px;
  background: #f9fafb;
  border-radius: 8px;
  color: #6b7280;
  font-size: 13px;
  font-weight: 500;
}

.choice-resolved-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
</style>
