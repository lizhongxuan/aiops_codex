<script setup>
import { computed, ref } from "vue";
import Modal from "./Modal.vue";

const props = defineProps({
  host: {
    type: Object,
    default: null,
  },
});

const emit = defineEmits(["close", "save"]);

const isEditing = computed(() => !!props.host?.id);

function labelsToText(labels) {
  if (!labels) return "";
  return Object.entries(labels)
    .map(([key, value]) => (value ? `${key}=${value}` : key))
    .join("\n");
}

const form = ref({
  id: props.host?.id || "",
  name: props.host?.name || "",
  address: props.host?.address || "",
  sshUser: props.host?.sshUser || "",
  sshPort: props.host?.sshPort || 22,
  labelsText: labelsToText(props.host?.labels),
  installViaSsh: false,
});

function parseLabels(raw) {
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

function submit() {
  emit("save", {
    id: form.value.id.trim(),
    name: form.value.name.trim(),
    address: form.value.address.trim(),
    sshUser: form.value.sshUser.trim(),
    sshPort: Number(form.value.sshPort) || 22,
    labels: parseLabels(form.value.labelsText),
    installViaSsh: !!form.value.installViaSsh,
  });
}
</script>

<template>
  <Modal :title="isEditing ? '编辑主机' : '新增主机'" @close="emit('close')">
    <form class="host-form" @submit.prevent="submit">
      <label class="host-form-field">
        <span>Host ID</span>
        <input v-model="form.id" :disabled="isEditing" placeholder="web-01" required />
      </label>

      <label class="host-form-field">
        <span>显示名称</span>
        <input v-model="form.name" placeholder="web-01 / 支付-应用节点" />
      </label>

      <label class="host-form-field">
        <span>目标机器</span>
        <input v-model="form.address" placeholder="10.0.0.21 或 web-01.internal" required />
      </label>

      <div class="host-form-grid">
        <label class="host-form-field">
          <span>SSH 用户</span>
          <input v-model="form.sshUser" placeholder="ubuntu / root" />
        </label>

        <label class="host-form-field">
          <span>SSH 端口</span>
          <input v-model="form.sshPort" type="number" min="1" max="65535" />
        </label>
      </div>

      <label class="host-form-field">
        <span>标签</span>
        <textarea
          v-model="form.labelsText"
          rows="4"
          placeholder="env=prod&#10;role=web&#10;app=nginx"
        />
      </label>

      <label class="host-form-checkbox" v-if="!isEditing">
        <input v-model="form.installViaSsh" type="checkbox" />
        <span>保存后通过 SSH 安装 host-agent</span>
      </label>

      <p class="host-form-note">
        SSH 安装依赖当前机器已经可以访问目标主机，例如本机已有 SSH key、跳板机或 agent 转发。
      </p>

      <div class="host-form-actions">
        <button type="button" class="ops-button ghost" @click="emit('close')">取消</button>
        <button type="submit" class="ops-button primary">
          {{ isEditing ? "保存主机" : "创建主机" }}
        </button>
      </div>
    </form>
  </Modal>
</template>

<style scoped>
.host-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.host-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.host-form-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.host-form-field span {
  font-size: 13px;
  font-weight: 600;
  color: #334155;
}

.host-form-field input,
.host-form-field textarea {
  width: 100%;
  border: 1px solid #dbe3ee;
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 14px;
  color: #0f172a;
  background: #fff;
}

.host-form-field textarea {
  resize: vertical;
  min-height: 96px;
}

.host-form-field input:focus,
.host-form-field textarea:focus {
  outline: none;
  border-color: #93c5fd;
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.12);
}

.host-form-checkbox {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  color: #1e293b;
}

.host-form-note {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  color: #64748b;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 10px 12px;
}

.host-form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

@media (max-width: 720px) {
  .host-form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
