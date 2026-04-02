<script setup>
import { ref } from "vue";
import Modal from "./Modal.vue";

const props = defineProps({
  count: {
    type: Number,
    default: 0,
  },
});

const emit = defineEmits(["close", "save"]);

const addText = ref("");
const removeText = ref("");

function parseAdd(raw) {
  return String(raw || "")
    .split(/\n|,|;/)
    .map((item) => item.trim())
    .filter(Boolean)
    .reduce((acc, item) => {
      const [key, ...rest] = item.split("=");
      const normalizedKey = key?.trim();
      if (!normalizedKey) return acc;
      acc[normalizedKey] = rest.join("=").trim();
      return acc;
    }, {});
}

function parseRemove(raw) {
  return String(raw || "")
    .split(/\n|,|;/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function submit() {
  emit("save", {
    add: parseAdd(addText.value),
    remove: parseRemove(removeText.value),
  });
}
</script>

<template>
  <Modal title="批量标签" @close="emit('close')">
    <form class="tag-form" @submit.prevent="submit">
      <p class="tag-note">已选 {{ count }} 台主机。支持批量添加标签，也可以按 key 批量删除。</p>

      <label class="tag-field">
        <span>新增标签</span>
        <textarea v-model="addText" rows="4" placeholder="env=prod&#10;role=web&#10;batch=blue" />
      </label>

      <label class="tag-field">
        <span>删除标签</span>
        <textarea v-model="removeText" rows="3" placeholder="batch&#10;owner" />
      </label>

      <div class="tag-actions">
        <button type="button" class="ops-button ghost" @click="emit('close')">取消</button>
        <button type="submit" class="ops-button primary">应用到已选主机</button>
      </div>
    </form>
  </Modal>
</template>

<style scoped>
.tag-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.tag-note {
  margin: 0;
  font-size: 13px;
  color: #475569;
  line-height: 1.6;
}

.tag-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.tag-field span {
  font-size: 13px;
  font-weight: 600;
  color: #334155;
}

.tag-field textarea {
  width: 100%;
  border: 1px solid #dbe3ee;
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 14px;
  color: #0f172a;
  resize: vertical;
}

.tag-field textarea:focus {
  outline: none;
  border-color: #93c5fd;
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.12);
}

.tag-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
