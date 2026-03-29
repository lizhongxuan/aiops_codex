import { createRouter, createWebHistory } from "vue-router";

const ChatPage = () => import("./pages/ChatPage.vue");
const HostsPage = () => import("./pages/HostsPage.vue");
const ExperiencePacksPage = () => import("./pages/ExperiencePacksPage.vue");
const ProtocolPage = () => import("./pages/ProtocolPage.vue");
const TerminalPage = () => import("./pages/TerminalPage.vue");
const AgentProfilePage = () => import("./pages/AgentProfilePage.vue");

const routes = [
  {
    path: "/",
    name: "chat",
    component: ChatPage,
  },
  {
    path: "/hosts",
    name: "hosts",
    component: HostsPage,
  },
  {
    path: "/experience-packs",
    name: "experience-packs",
    component: ExperiencePacksPage,
  },
  {
    path: "/protocol",
    name: "protocol",
    component: ProtocolPage,
  },
  {
    path: "/terminal/:hostId",
    name: "terminal",
    component: TerminalPage,
    props: true,
  },
  {
    path: "/settings/agent",
    name: "settings-agent",
    component: AgentProfilePage,
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
});

export default router;
