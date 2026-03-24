<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch, nextTick } from "vue";
import { useAppStore } from "./store";
import CardItem from "./components/CardItem.vue";
import LoginModal from "./components/LoginModal.vue";
import HostModal from "./components/HostModal.vue";
import Omnibar from "./components/Omnibar.vue";
import { MessageSquarePlusIcon, AppWindowIcon, SettingsIcon, UserCircleIcon, ServerIcon, PanelsTopLeftIcon, BotIcon } from "lucide-vue-next";

const store = useAppStore();

const isLoginModalOpen = ref(false);
const isHostModalOpen = ref(false);
const isMcpDrawerOpen = ref(false);

const composerMessage = ref("");
const scrollContainer = ref(null);
let isUserScrolling = false;

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

const composerPlaceholder = computed(() => {
  if (!store.snapshot.auth.connected) return "请先登录 GPT 账号后再开始对话";
  if (!store.snapshot.config.codexAlive) return "Codex app-server 当前不可用";
  if (!store.selectedHost.executable) return "当前主机仅展示，不支持执行";
  if (store.selectedHost.status !== "online") return "当前主机离线，暂时不可执行";
  return "Ask Codex to build something";
});

async function startNewThread() {
  if (store.sending) return;
  const ok = await store.resetThread();
  if (!ok) return;
  composerMessage.value = "";
  store.errorMessage = "";
  isUserScrolling = false;
}

function handleGlobalKeydown(e) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "n") {
    e.preventDefault();
    startNewThread();
  }
}

async function sendMessage() {
  if (!store.canSend || !composerMessage.value.trim()) return;
  
  store.sending = true;
  store.errorMessage = "";
  
  try {
    const response = await fetch("/api/v1/chat/message", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        message: composerMessage.value,
        hostId: store.snapshot.selectedHostId,
      }),
    });
    
    if (!response.ok) {
      const data = await response.json();
      store.errorMessage = data.error || "message send failed";
    } else {
      composerMessage.value = "";
      // Reset scroll lock when sending message
      isUserScrolling = false;
    }
  } catch (e) {
    store.errorMessage = "Network error";
  } finally {
    store.sending = false;
  }
}

async function decideApproval({ approvalId, decision }) {
  try {
    const response = await fetch(`/api/v1/approvals/${approvalId}/decision`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decision }),
    });
    if (!response.ok) {
      const data = await response.json();
      store.errorMessage = data.error || "approval failed";
    } else {
      isUserScrolling = false; // Scroll down after approval
    }
  } catch(e) {
    console.error(e);
  }
}

function handleScroll(e) {
  const el = e.target;
  // If user scrolls up from the bottom more than 10px, freeze auto-scroll
  if (el.scrollHeight - el.scrollTop - el.clientHeight > 10) {
    isUserScrolling = true;
  } else {
    isUserScrolling = false;
  }
}

// Smart Auto-Scroll
watch(
  () => store.snapshot.cards,
  () => {
    if (!isUserScrolling) {
      nextTick(() => {
        if (scrollContainer.value) {
          scrollContainer.value.scrollTop = scrollContainer.value.scrollHeight;
        }
      });
    }
  },
  { deep: true }
);

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
          <button class="nav-item active">
            <AppWindowIcon size="16" />
            <div class="nav-item-content">
              <span class="nav-item-title">Codex Assistant</span>
              <span class="nav-item-time">Active</span>
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

      <div class="chat-container" ref="scrollContainer" @scroll="handleScroll">
        <div class="chat-stream-inner">
          <div v-if="store.loading" class="chat-banner loading-banner">
            <span class="spinner"></span> 正在初始化...
          </div>
          
          <div v-if="!store.snapshot.cards.length && !store.loading" class="empty-state-canvas">
            <BotIcon size="48" class="empty-icon" />
            <h2>What can I help you build?</h2>
            <p>I can help you write code, manage servers, execute commands, and orchestrate complex tasks.</p>
          </div>
          
          <p v-if="store.errorMessage" class="chat-banner error">{{ store.errorMessage }}</p>

          <div class="chat-stream">
            <div
              v-for="card in store.snapshot.cards"
              :key="card.id"
              class="stream-row"
              :class="{
                'row-user': card.type === 'MessageCard' && card.role === 'user',
                'row-assistant': !(card.type === 'MessageCard' && card.role === 'user'),
              }"
            >
              <CardItem :card="card" @approval="decideApproval" />
            </div>
          </div>
        </div>
      </div>

      <!-- Omnibar Base -->
      <footer class="omnibar-dock">
         <Omnibar 
           v-model="composerMessage"
           :placeholder="composerPlaceholder"
           @send="sendMessage"
         />
      </footer>
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
