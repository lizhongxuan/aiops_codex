<script setup>
import { useRouter } from "vue-router";
import Modal from "./Modal.vue";
import { useAppStore } from "../store";
import { ServerIcon, CheckCircle2Icon, TerminalIcon } from "lucide-vue-next";

const emit = defineEmits(["close"]);
const store = useAppStore();
const router = useRouter();

async function selectHost(hostId) {
  const ok = await store.selectHost(hostId);
  if (!ok) return;
  emit("close");
}

async function openTerminal(hostId) {
  const ok = await store.selectHost(hostId);
  if (!ok) return;
  emit("close");
  router.push(`/terminal/${hostId}`);
}
</script>

<template>
  <Modal title="Select Host Environment" @close="emit('close')">
    <div class="host-list">
      <div
        v-for="host in store.snapshot.hosts"
        :key="host.id"
        class="host-item"
        :class="{ active: store.snapshot.selectedHostId === host.id }"
        @click="selectHost(host.id)"
      >
        <div class="host-icon">
          <ServerIcon size="20" />
        </div>
        <div class="host-info">
          <div class="host-name">{{ host.name }}</div>
          <div class="host-id">ID: {{ host.id }}</div>
          <div class="host-meta">
            <span class="badge" :class="host.status === 'online' ? 'online' : 'offline'">
              {{ host.status }}
            </span>
            <span class="host-kind">{{ host.kind }}</span>
            <span v-if="host.kind === 'agent' && host.executable" class="badge terminal">远程可管</span>
            <span v-else-if="host.terminalCapable && !host.executable" class="badge terminal">远程终端</span>
            <span v-else-if="!host.executable" class="badge readonly">只读展示</span>
          </div>
        </div>
        <button
          v-if="(host.terminalCapable || host.executable) && host.status === 'online'"
          type="button"
          class="host-action-btn"
          @click.stop="openTerminal(host.id)"
        >
          <TerminalIcon size="14" />
          <span>进入终端</span>
        </button>
        <div class="host-check" v-if="store.snapshot.selectedHostId === host.id">
          <CheckCircle2Icon size="20" class="text-blue" />
        </div>
      </div>
      
      <div v-if="!store.snapshot.hosts.length" class="empty-state">
        No hosts available.
      </div>
    </div>
  </Modal>
</template>

<style scoped>
.host-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.host-item {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px;
  border-radius: 12px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  cursor: pointer;
  transition: all 0.2s;
  text-align: left;
}

.host-item:hover {
  border-color: #cbd5e1;
  background: #f8fafc;
}

.host-item.active {
  border-color: #3b82f6;
  background: #eff6ff;
  box-shadow: 0 2px 8px rgba(59, 130, 246, 0.1);
}

.host-icon {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  background: #f1f5f9;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
}

.host-item.active .host-icon {
  background: #dbeafe;
  color: #3b82f6;
}

.host-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.host-name {
  font-weight: 600;
  font-size: 15px;
  color: #0f172a;
}

.host-id {
  font-size: 12px;
  color: #64748b;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.host-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}

.host-kind {
  color: #64748b;
  text-transform: uppercase;
  font-weight: 500;
}

.badge {
  padding: 2px 8px;
  border-radius: 12px;
  font-weight: 600;
  font-size: 11px;
}

.badge.online {
  background: #dcfce7;
  color: #166534;
}

.badge.offline {
  background: #f1f5f9;
  color: #64748b;
}

.badge.readonly {
  background: #fffedd;
  color: #854d0e;
  border: 1px solid #fef08a;
}

.badge.terminal {
  background: #eff6ff;
  color: #1d4ed8;
  border: 1px solid #bfdbfe;
}

.text-blue {
  color: #3b82f6;
}

.host-action-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border-radius: 999px;
  border: 1px solid #dbe3ee;
  background: #ffffff;
  color: #334155;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

.host-action-btn:hover {
  background: #f8fafc;
  border-color: #cbd5e1;
}

.empty-state {
  text-align: center;
  padding: 32px;
  color: #64748b;
  font-size: 14px;
  background: #f8fafc;
  border-radius: 12px;
  border: 1px dashed #cbd5e1;
}
</style>
