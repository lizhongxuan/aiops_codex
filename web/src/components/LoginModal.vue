<script setup>
import { computed } from "vue";
import Modal from "./Modal.vue";
import { useAppStore } from "../store";

const emit = defineEmits(["close"]);
const store = useAppStore();

const loginResultMessages = {
  success: "GPT 登录成功，当前页面会自动恢复登录态。",
  oauth_not_configured: "服务端还没有配置 OAuth，请改用本机 ChatGPT 登录或补齐 OAuth 配置。",
  missing_code: "登录回调缺少 code 参数，请重新发起登录。",
  invalid_state: "登录状态校验失败，请重新点击 Connect GPT。",
  exchange_failed: "OAuth code 换取 token 失败，请检查服务端 OAuth 配置。",
  codex_login_failed: "已完成 OAuth，但注入 Codex 登录态失败，请查看服务端日志。",
};

const loginHint = computed(() => {
  const params = new URLSearchParams(window.location.search);
  return params.get("login");
});

const loginHintText = computed(() => {
  if (!loginHint.value) return "";
  return loginResultMessages[loginHint.value] || `登录结果: ${loginHint.value}`;
});

const loginHintState = computed(() => {
  if (!loginHint.value) return "";
  return loginHint.value === "success" ? "hint" : "error-text";
});

async function login() {
  store.errorMessage = "";
  try {
    const response = await fetch("/api/v1/auth/login", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(store.authForm),
    });
    const data = await response.json();
    if (!response.ok) {
      store.errorMessage = data.error || "login failed";
      return;
    }
    await store.fetchState();
    if (data.authUrl) {
      window.open(data.authUrl, "_blank", "noopener,noreferrer");
    }
    emit("close");
  } catch (e) {
    store.errorMessage = "Network error during login";
  }
}

async function logout() {
  await fetch("/api/v1/auth/logout", { method: "POST", credentials: "include" });
  store.errorMessage = "";
  await store.fetchState();
}

function startConfiguredOAuth() {
  window.location.href = "/api/v1/auth/oauth/start";
}
</script>

<template>
  <Modal title="GPT Status & Login" @close="emit('close')">
    <div class="login-container">
      <div class="status-box" :class="{ connected: store.snapshot.auth.connected }">
        <span class="status-dot"></span>
        {{ store.snapshot.auth.connected ? "已连接 GPT" : "未连接" }}
      </div>

      <p v-if="loginHintText" class="login-hint" :class="loginHintState">{{ loginHintText }}</p>
      
      <div v-if="store.snapshot.auth.email" class="info-row">
        <span class="label">账号</span>
        <span class="value">{{ store.snapshot.auth.email }}</span>
      </div>
      <div v-if="store.snapshot.auth.mode" class="info-row">
        <span class="label">模式</span>
        <span class="value">{{ store.snapshot.auth.mode }}</span>
      </div>
      <div v-if="store.snapshot.auth.planType" class="info-row">
        <span class="label">计划</span>
        <span class="value badge">{{ store.snapshot.auth.planType }}</span>
      </div>
      
      <p v-if="store.snapshot.auth.pending" class="hint-text text-amber">正在等待浏览器认证完成...</p>
      <p v-if="store.snapshot.auth.lastError || store.errorMessage" class="error-text">
        {{ store.snapshot.auth.lastError || store.errorMessage }}
      </p>

      <div class="form-row" v-if="store.snapshot.config.oauthConfigured">
        <button class="btn btn-primary fluid" @click="startConfiguredOAuth">使用已配置 OAuth 登录</button>
      </div>

      <hr class="divider" />

      <div class="field">
        <label>登录模式</label>
        <select v-model="store.authForm.mode" class="input">
          <option value="chatgpt">ChatGPT 登录</option>
          <option value="chatgptAuthTokens">外部 Auth Tokens</option>
          <option value="apiKey">API Key</option>
        </select>
      </div>

      <div v-if="store.authForm.mode === 'chatgptAuthTokens'" class="stack">
        <div class="field">
          <label>Access Token</label>
          <textarea v-model="store.authForm.accessToken" rows="3" class="input"></textarea>
        </div>
        <div class="field">
          <label>ChatGPT Account ID</label>
          <input v-model="store.authForm.chatgptAccountId" class="input" />
        </div>
        <div class="field">
          <label>Plan Type</label>
          <input v-model="store.authForm.chatgptPlanType" placeholder="plus / pro / team" class="input" />
        </div>
        <div class="field">
          <label>Email (Optional)</label>
          <input v-model="store.authForm.email" class="input" />
        </div>
      </div>

      <div v-if="store.authForm.mode === 'apiKey'" class="stack">
        <div class="field">
          <label>API Key</label>
          <input v-model="store.authForm.apiKey" type="password" class="input" />
        </div>
        <div class="field">
          <label>Email (Optional)</label>
          <input v-model="store.authForm.email" class="input" />
        </div>
      </div>

      <div class="actions">
        <button class="btn" @click="logout" v-if="store.snapshot.auth.connected">注销</button>
        <button class="btn btn-primary" style="flex: 1" @click="login">
          {{ store.snapshot.auth.connected ? "更换账号" : "Connect GPT" }}
        </button>
      </div>
    </div>
  </Modal>
</template>

<style scoped>
.login-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.status-box {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 8px;
  background: #f1f5f9;
  font-weight: 500;
  font-size: 14px;
  color: #475569;
}

.status-box.connected {
  background: #f0fdf4;
  color: #166534;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #94a3b8;
}

.status-box.connected .status-dot {
  background: #22c55e;
}

.info-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 14px;
}

.info-row .label {
  color: #64748b;
}

.info-row .value {
  font-weight: 500;
  color: #0f172a;
}

.badge {
  background: #e2e8f0;
  padding: 2px 8px;
  border-radius: 12px;
  font-size: 12px;
}

.divider {
  border: none;
  border-top: 1px solid #e2e8f0;
  margin: 8px 0;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field label {
  font-size: 13px;
  font-weight: 500;
  color: #475569;
}

.input {
  border: 1px solid #cbd5e1;
  border-radius: 8px;
  padding: 10px 12px;
  font-size: 14px;
  outline: none;
  transition: border-color 0.2s;
  background: #f8fafc;
}

.input:focus {
  border-color: #3b82f6;
  background: #ffffff;
}

.stack {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.actions {
  display: flex;
  gap: 12px;
  margin-top: 8px;
}

.btn {
  padding: 10px 16px;
  border-radius: 8px;
  border: none;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.2s, transform 0.1s;
  background: #f1f5f9;
  color: #0f172a;
}

.btn:hover {
  background: #e2e8f0;
}

.btn:active {
  transform: translateY(1px);
}

.btn-primary {
  background: #0f172a;
  color: white;
}

.btn-primary:hover {
  background: #1e293b;
}

.fluid {
  width: 100%;
}

.error-text {
  color: #ef4444;
  font-size: 13px;
  margin: 0;
}

.hint-text {
  color: #64748b;
  font-size: 13px;
  margin: 0;
}

.text-amber {
  color: #d97706;
}
</style>
