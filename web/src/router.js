import { createRouter, createWebHistory } from "vue-router";

const ChatPage = () => import("./pages/ChatPage.vue");
const TerminalPage = () => import("./pages/TerminalPage.vue");

const routes = [
  {
    path: "/",
    name: "chat",
    component: ChatPage,
  },
  {
    path: "/terminal/:hostId",
    name: "terminal",
    component: TerminalPage,
    props: true,
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
});

export default router;
