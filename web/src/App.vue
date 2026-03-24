<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useAppStore } from "./store";
import LoginModal from "./components/LoginModal.vue";
import HostModal from "./components/HostModal.vue";
import { MessageSquarePlusIcon, AppWindowIcon, SettingsIcon, UserCircleIcon, ServerIcon, PanelsTopLeftIcon, TerminalIcon } from "lucide-vue-next";

const store = useAppStore();
const router = useRouter();

const isLoginModalOpen = ref(false);
const isHostModalOpen = ref(false);
const isMcpDrawerOpen = ref(false);

function toggleMcpDrawer() {
  isMcpDrawerOpen.value = !isMcpDrawerOpen.value;
}

const authBadgeLabel = computed(() => {
  if (store.snapshot.auth.connected) {
    return `GPT ${store.snapshot.auth.planType || "接入"}`;
  }
  if (store.snapshot.auth.pending) {
    return "登录中";
  }
  return "未登录";
});

async function startNewThread() {
  if (store.sending) return;
  const ok = await store.resetThread();
  if (!ok) return;
  store.errorMessage = "";
}

function handleGlobalKeydown(e) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "n") {
    e.preventDefault();
    startNewThread();
  }
}

function openTerminal() {
  router.push(`/terminal/${store.snapshot.selectedHostId}`);
}

onMounted(() => {
  store.fetchState();
  store.connectWs();
  window.addEventListener("keydown", handleGlobalKeydown);
});

onBeforeUnmount(() => {
  if (store._socket) {
    store._socket.close();
  }
  window.removeEventListener("keydown", handleGlobalKeydown);
});
</script>

<template>
  <div class="app-layout">
    <!-- Left Sidebar: Navigation & Threads -->
    <aside class="app-sidebar">
      <div class="sidebar-top">
        <button class="nav-button new-thread" @click="startNewThread">
          <MessageSquarePlusIcon size="18" />
          <span>New Thread</span>
          <span class="shortcut">⌘ N</span>
        </button>
      </div>

      <div class="sidebar-scroll">
        <div class="nav-group">
          <div class="nav-group-title">线程</div>
          <button class="nav-item" :class="{ active: $route.name === 'chat' }" @click="router.push('/')">
            <AppWindowIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">Codex Assistant</span>
              <span class="nav-item-time">Active</span>
            </div>
          </button>
        </div>

        <div class="nav-group">
          <div class="nav-group-title">终端</div>
          <button class="nav-item" :class="{ active: $route.name === 'terminal' }" @click="openTerminal">
            <TerminalIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">{{ store.selectedHost.name }}</span>
              <span class="nav-item-time">
                <span class="pill-dot-inline" :class="store.selectedHost.status"></span>
                {{ store.selectedHost.status }}
              </span>
            </div>
          </button>
        </div>
      </div>

      <div class="sidebar-bottom">
        <button class="nav-icon-btn" title="Settings">
          <SettingsIcon size="20" />
        </button>
        <div class="flex-spacer"></div>
        <div class="ws-badge" :class="store.wsStatus" :title="'WS: ' + store.wsStatus"></div>
      </div>
    </aside>

    <!-- Main Canvas -->
    <main class="app-main">
      <header class="main-header">
        <div class="header-left">
          <h1 class="header-title">Codex Workspace</h1>
        </div>
        
        <div class="header-right">
          <button class="header-pill" @click="openTerminal" title="打开终端">
            <TerminalIcon size="14" />
            <span class="pill-text">终端</span>
          </button>

          <button class="header-pill" @click="isHostModalOpen = true">
            <ServerIcon size="14" />
            <span class="pill-text">{{ store.selectedHost.name }}</span>
            <span class="pill-dot" :class="store.selectedHost.status"></span>
          </button>
          
          <button class="header-pill auth-pill" @click="isLoginModalOpen = true" :class="{'connected': store.snapshot.auth.connected}">
            <UserCircleIcon size="16" />
            <span class="pill-text">{{ authBadgeLabel }}</span>
          </button>
          
          <button class="header-icon-btn" @click="toggleMcpDrawer" :class="{ 'is-active': isMcpDrawerOpen }" title="Skills & MCP">
            <PanelsTopLeftIcon size="20" />
          </button>
        </div>
      </header>

      <!-- Router View: ChatPage or TerminalPage -->
      <router-view />
    </main>

    <!-- Right Drawer: MCP & Core Panel -->
    <aside class="app-mcp-drawer" :class="{ 'is-open': isMcpDrawerOpen }">
      <div class="mcp-header">
        <h3>Skills & MCP</h3>
      </div>
      <div class="mcp-body">
         <p class="subtle" style="font-size:13px">No skills configured yet.</p>
      </div>
    </aside>

    <!-- Modals -->
    <LoginModal v-if="isLoginModalOpen" @close="isLoginModalOpen = false" />
    <HostModal v-if="isHostModalOpen" @close="isHostModalOpen = false" />
  </div>
</template>

<style scoped>
.pill-dot-inline {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  margin-right: 4px;
  vertical-align: middle;
}
.pill-dot-inline.online { background: #22c55e; }
.pill-dot-inline.offline { background: #94a3b8; }
</style>
