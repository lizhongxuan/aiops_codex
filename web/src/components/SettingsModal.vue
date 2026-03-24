<script setup>
import { ref, onMounted } from "vue";
import { useAppStore } from "../store";
import { XIcon } from "lucide-vue-next";

const emit = defineEmits(["close"]);
const store = useAppStore();

const localModel = ref("");
const localEffort = ref("medium");
const isSaving = ref(false);

onMounted(async () => {
  await store.fetchSettings();
  localModel.value = store.settings.model || "gpt-4-turbo";
  localEffort.value = store.settings.reasoningEffort || "medium";
});

async function save() {
  if (isSaving.value) return;
  isSaving.value = true;
  await store.updateSettings({
    model: localModel.value,
    reasoningEffort: localEffort.value,
  });
  isSaving.value = false;
  emit("close");
}
</script>

<template>
  <div class="modal-overlay" @click.self="emit('close')">
    <div class="settings-modal" @click.stop>
      <div class="modal-header">
        <h2 class="modal-title">Settings</h2>
        <button class="icon-btn close-btn" @click="emit('close')">
          <XIcon size="20" />
        </button>
      </div>

      <div class="modal-body">
        <!-- Quota Section -->
        <div class="settings-group">
          <h3 class="group-title">Account Quota</h3>
          <div class="quota-display">
            <span class="quota-amount">{{ store.settings.quota || 'Unlimited' }}</span>
            <span class="quota-label">Remaining Requests</span>
          </div>
        </div>

        <!-- Model Selection -->
        <div class="settings-group">
          <h3 class="group-title">Model Configuration</h3>
          <div class="form-group">
            <label>Provider & Model</label>
            <select v-model="localModel" class="form-select">
              <optgroup label="Available Models">
                <option v-for="m in store.settings.models" :key="m.id" :value="m.id">
                  {{ m.name || m.id }}
                </option>
                <!-- Fallbacks if API empty -->
                <option v-if="!store.settings.models.length" value="gpt-4o">GPT-4o</option>
                <option v-if="!store.settings.models.length" value="gpt-4-turbo">GPT-4 Turbo</option>
                <option v-if="!store.settings.models.length" value="claude-3-opus">Claude 3 Opus</option>
              </optgroup>
            </select>
          </div>

          <div class="form-group">
            <label>Reasoning Intensity</label>
            <div class="radio-group-row">
              <label class="radio-pill" :class="{ active: localEffort === 'low' }">
                <input type="radio" value="low" v-model="localEffort" class="sr-only" />
                Low
              </label>
              <label class="radio-pill" :class="{ active: localEffort === 'medium' }">
                <input type="radio" value="medium" v-model="localEffort" class="sr-only" />
                Medium
              </label>
              <label class="radio-pill" :class="{ active: localEffort === 'high' }">
                <input type="radio" value="high" v-model="localEffort" class="sr-only" />
                High
              </label>
            </div>
            <p class="help-text">Higher intensity provides better reasoning but may take longer.</p>
          </div>
        </div>
      </div>

      <div class="modal-actions">
        <button class="btn btn-secondary" @click="emit('close')">Cancel</button>
        <button class="btn btn-primary" @click="save" :disabled="isSaving">
          {{ isSaving ? 'Saving...' : 'Save Settings' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.modal-overlay {
  position: fixed;
  inset: 0;
  background-color: rgba(0, 0, 0, 0.4);
  backdrop-filter: blur(2px);
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
}

.settings-modal {
  background: var(--bg-surface, #ffffff);
  border-radius: var(--radius-card, 16px);
  width: 100%;
  max-width: 440px;
  box-shadow: 0 10px 25px rgba(0, 0, 0, 0.1);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border-color, #E5E7EB);
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border-color, #E5E7EB);
}

.modal-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary, #111827);
}

.close-btn {
  background: none;
  border: none;
  color: var(--text-tertiary, #9CA3AF);
  cursor: pointer;
  border-radius: 6px;
  padding: 4px;
}
.close-btn:hover {
  background-color: var(--bg-hover, #F3F4F6);
  color: var(--text-primary, #111827);
}

.modal-body {
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.group-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-secondary, #4B5563);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin: 0 0 12px 0;
}

.settings-group {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.quota-display {
  display: inline-flex;
  align-items: baseline;
  background: var(--bg-hover, #F3F4F6);
  padding: 12px 16px;
  border-radius: 8px;
  gap: 8px;
}
.quota-amount {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-primary, #111827);
}
.quota-label {
  font-size: 14px;
  color: var(--text-secondary, #6B7280);
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.form-group label {
  font-size: 14px;
  font-weight: 500;
  color: var(--text-primary, #374151);
}

.form-select {
  padding: 10px 12px;
  border-radius: 8px;
  border: 1px solid var(--border-color, #D1D5DB);
  font-size: 14px;
  background-color: var(--bg-surface, #ffffff);
  color: var(--text-primary, #111827);
  outline: none;
}
.form-select:focus {
  border-color: #3b82f6;
  box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
}

.radio-group-row {
  display: flex;
  gap: 8px;
}

.radio-pill {
  flex: 1;
  text-align: center;
  padding: 8px 12px;
  border: 1px solid var(--border-color, #E5E7EB);
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-secondary, #4B5563);
  cursor: pointer;
  transition: all 0.2s;
  background: var(--bg-surface, #ffffff);
}
.radio-pill.active {
  background: var(--text-primary, #111827);
  color: #ffffff;
  border-color: var(--text-primary, #111827);
}
.radio-pill:hover:not(.active) {
  background: var(--bg-hover, #F3F4F6);
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border-width: 0;
}

.help-text {
  margin: 0;
  font-size: 12px;
  color: var(--text-tertiary, #9CA3AF);
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding: 16px 20px;
  border-top: 1px solid var(--border-color, #E5E7EB);
  background: var(--bg-hover, #F9FAFB);
}

.btn {
  padding: 8px 16px;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  border: none;
  transition: background-color 0.2s;
}
.btn-secondary {
  background: transparent;
  color: var(--text-secondary, #4B5563);
}
.btn-secondary:hover {
  background: #E5E7EB;
}
.btn-primary {
  background: var(--text-primary, #111827);
  color: #ffffff;
}
.btn-primary:hover {
  background: #374151;
}
.btn-primary:disabled {
  opacity: 0.7;
  cursor: not-allowed;
}
</style>
