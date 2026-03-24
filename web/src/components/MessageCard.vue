<script setup>
import { computed } from "vue";
import { UserIcon, BotIcon } from "lucide-vue-next";

const props = defineProps({
  card: {
    type: Object,
    required: true,
  },
});

const isUser = computed(() => props.card.role === "user");

const avatarIcon = computed(() => {
  return isUser.value ? UserIcon : BotIcon;
});
</script>

<template>
  <div class="message-wrapper" :class="{ 'is-user': isUser }">
    <div class="avatar" v-if="!isUser">
      <BotIcon size="20" />
    </div>
    
    <div class="message-content">
      <div class="message-text">
        {{ card.text || card.title }}
      </div>
      <div class="ghost-loader" v-if="card.status === 'inProgress'">
        <span class="spinner-small"></span> 
        <span class="ghost-text">Thinking...</span>
      </div>
    </div>
    
    <div class="avatar user-avatar" v-if="isUser">
      <UserIcon size="20" />
    </div>
  </div>
</template>

<style scoped>
.message-wrapper {
  display: flex;
  gap: 16px;
  max-width: 100%;
  width: 100%;
}

.message-wrapper.is-user {
  justify-content: flex-end;
}

.avatar {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: white;
  border: 1px solid #e2e8f0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  flex-shrink: 0;
}

.user-avatar {
  background: #f8fafc;
}

.message-content {
  flex: 1;
  max-width: calc(100% - 48px);
}

.is-user .message-content {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
}

.message-text {
  font-size: var(--text-body, 15px);
  line-height: var(--line-height-body, 1.7);
  color: #0f172a;
  white-space: pre-wrap;
}

.is-user .message-text {
  background: #f3f4f6;
  padding: 14px 20px;
  border-radius: var(--radius-card, 16px);
  color: #0f172a;
  display: inline-block;
}

.ghost-loader {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  color: #94a3b8;
}

.ghost-text {
  font-size: 13px;
  font-style: italic;
}


.spinner-small {
  display: inline-block;
  width: 12px;
  height: 12px;
  border: 2px solid rgba(0,0,0,0.1);
  border-radius: 50%;
  border-top-color: currentColor;
  animation: spin 1s linear infinite;
}

@keyframes spin { 
  to { transform: rotate(360deg); }
}
</style>
