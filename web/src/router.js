import { createRouter, createWebHistory } from "vue-router";

const ChatPage = () => import("./pages/ChatPage.vue");
const HostsPage = () => import("./pages/HostsPage.vue");
const ExperiencePacksPage = () => import("./pages/ExperiencePacksPage.vue");
const ProtocolWorkspacePage = () => import("./pages/ProtocolWorkspacePage.vue");
const TerminalPage = () => import("./pages/TerminalPage.vue");
const AgentProfilePage = () => import("./pages/AgentProfilePage.vue");
const SkillCatalogPage = () => import("./pages/SkillCatalogPage.vue");
const McpCatalogPage = () => import("./pages/McpCatalogPage.vue");
const SettingsPage = () => import("./pages/SettingsPage.vue");

const routes = [
  {
    path: "/",
    name: "chat",
    component: ChatPage,
  },
  {
    path: "/hosts",
    redirect: "/settings/hosts",
  },
  {
    path: "/experience-packs",
    redirect: "/settings/experience-packs",
  },
  {
    path: "/protocol",
    name: "protocol",
    component: ProtocolWorkspacePage,
  },
  {
    path: "/terminal/:hostId",
    name: "terminal",
    component: TerminalPage,
    props: true,
  },
  {
    path: "/settings",
    name: "settings",
    component: SettingsPage,
  },
  {
    path: "/settings/hosts",
    name: "settings-hosts",
    component: HostsPage,
  },
  {
    path: "/settings/experience-packs",
    name: "settings-experience-packs",
    component: ExperiencePacksPage,
  },
  {
    path: "/settings/agent",
    name: "settings-agent",
    component: AgentProfilePage,
  },
  {
    path: "/settings/skills",
    name: "settings-skills",
    component: SkillCatalogPage,
  },
  {
    path: "/settings/mcp",
    name: "settings-mcp",
    component: McpCatalogPage,
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
});

export default router;
