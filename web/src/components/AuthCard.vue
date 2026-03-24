<script setup>
import { ref } from "vue";
import { ShieldAlertIcon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const emit = defineEmits(["approval"]);

const decision = ref("accept");

function onSubmit() {
  if (!props.card.approval?.requestId) return;
  emit("approval", {
    approvalId: props.card.approval.requestId,
    decision: decision.value,
  });
}
</script>

<template>
  <div class="auth-card">
    <div class="auth-header">
      <ShieldAlertIcon size="24" class="auth-icon" />
      <h3 class="auth-title">Authorization Required</h3>
    </div>
    
    <div class="auth-body">
      <p class="auth-text" v-if="card.text">{{ card.text }}</p>
      
      <!-- Command or Code Preview -->
      <div class="auth-preview" v-if="card.command || card.changes?.length">
        <div v-if="card.cwd" class="cwd-badge">DIR: {{ card.cwd }}</div>
        <pre v-if="card.command" class="mono">{{ card.command }}</pre>
        
        <div v-if="card.changes?.length" class="changes-list">
          <div v-for="change in card.changes" :key="change.path" class="change-item">
            <span class="change-kind badge">{{ change.kind }}</span>
            <span class="change-path mono">{{ change.path }}</span>
            <pre v-if="change.diff" class="diff-block mono">{{ change.diff }}</pre>
          </div>
        </div>
      </div>
      
      <!-- Actions: Only show if pending -->
      <div v-if="card.status === 'pending' && card.approval" class="auth-form">
        <p class="auth-question">Do you want to proceed with this action?</p>
        
        <div class="radio-group">
          <label class="radio-label">
            <input type="radio" v-model="decision" value="accept" name="decision" />
            <span class="radio-text">Yes, run this once</span>
          </label>
          <label class="radio-label">
            <input type="radio" v-model="decision" value="accept_session" name="decision" />
            <span class="radio-text">Always allow in this session</span>
          </label>
          <label class="radio-label">
            <input type="radio" v-model="decision" value="decline" name="decision" />
            <span class="radio-text">No, decline</span>
          </label>
        </div>
        
        <button class="submit-btn" :class="{ 'btn-danger': decision === 'decline' }" @click="onSubmit">
          {{ decision === 'decline' ? 'Decline Execution' : 'Approve Execution' }}
        </button>
      </div>
      
      <div v-else class="auth-resolved">
        This request has been resolved ({{ card.status }}).
      </div>
    </div>
  </div>
</template>

<style scoped>
.auth-card {
  border-radius: 16px;
  background: #fffaf5;
  border: 1px solid #fed7aa;
  overflow: hidden;
  margin-top: 12px;
  margin-left: 48px;
  max-width: 680px;
  box-shadow: 0 4px 16px rgba(234, 88, 12, 0.08); /* Orange subtle shadow */
}

.auth-header {
  padding: 16px 20px;
  background: #fff7ed;
  border-bottom: 1px solid #ffedd5;
  display: flex;
  align-items: center;
  gap: 12px;
}

.auth-icon {
  color: #ea580c;
}

.auth-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: #9a3412;
}

.auth-body {
  padding: 20px;
}

.auth-text {
  margin: 0 0 16px;
  font-size: 14px;
  line-height: 1.6;
  color: #431407;
}

.auth-preview {
  background: #1e293b;
  border-radius: 8px;
  padding: 16px;
  margin-bottom: 20px;
  color: #f8fafc;
  font-size: 13px;
  overflow-x: auto;
}

.cwd-badge {
  display: inline-block;
  font-size: 10px;
  color: #94a3b8;
  margin-bottom: 8px;
  text-transform: uppercase;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

pre.mono {
  margin: 0;
  white-space: pre-wrap;
}

.change-item {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid #334155;
}

.badge {
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 4px;
  background: #334155;
  color: #cbd5e1;
  text-transform: uppercase;
  margin-right: 8px;
}

.change-path {
  color: #cbd5e1;
}

.diff-block {
  margin-top: 8px;
  color: #e2e8f0;
}

.auth-form {
  background: white;
  padding: 16px;
  border-radius: 12px;
  border: 1px solid #e2e8f0;
}

.auth-question {
  margin: 0 0 12px;
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
}

.radio-group {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 16px;
}

.radio-label {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  font-size: 14px;
  color: #334155;
}

.radio-label input[type="radio"] {
  width: 16px;
  height: 16px;
  accent-color: #ea580c;
}

.submit-btn {
  width: 100%;
  padding: 12px;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 600;
  border: none;
  cursor: pointer;
  transition: background 0.2s, transform 0.1s;
  background: #0f172a;
  color: white;
}

.submit-btn:hover {
  background: #1e293b;
}

.submit-btn.btn-danger {
  background: #ef4444;
}

.submit-btn.btn-danger:hover {
  background: #dc2626;
}

.submit-btn:active {
  transform: translateY(1px);
}

.auth-resolved {
  background: #f1f5f9;
  padding: 12px;
  border-radius: 8px;
  text-align: center;
  color: #64748b;
  font-size: 13px;
  font-weight: 500;
}
</style>
