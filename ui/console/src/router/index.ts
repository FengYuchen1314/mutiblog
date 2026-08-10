import { createRouter, createWebHistory } from "vue-router";
import { api } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import SetupView from "@/views/SetupView.vue";
import LoginView from "@/views/LoginView.vue";
import ConsoleLayout from "@/layouts/ConsoleLayout.vue";
import DashboardView from "@/views/DashboardView.vue";
import ResourceView from "@/views/ResourceView.vue";

const resourceRoutes = [
  ["posts", "navigation.posts"],
  ["pages", "navigation.pages"],
  ["comments", "navigation.comments"],
  ["attachments", "navigation.attachments"],
  ["links", "navigation.links"],
  ["theme", "navigation.theme"],
  ["menus", "navigation.menus"],
  ["locales", "navigation.locales"],
  ["ai/providers", "navigation.providers"],
  ["ai/tasks", "navigation.tasks"],
  ["settings", "navigation.settings"],
  ["overview", "navigation.overview"],
  ["backup", "navigation.backup"],
  ["tools", "navigation.tools"],
] as const;

export const router = createRouter({
  history: createWebHistory("/console/"),
  routes: [
    { path: "/setup", name: "setup", component: SetupView, meta: { public: true } },
    { path: "/login", name: "login", component: LoginView, meta: { public: true } },
    {
      path: "/",
      component: ConsoleLayout,
      children: [
        { path: "", redirect: "/dashboard" },
        { path: "dashboard", name: "dashboard", component: DashboardView },
        ...resourceRoutes.map(([path, title]) => ({
          path,
          name: path.replace("/", "-"),
          component: ResourceView,
          meta: { title },
        })),
      ],
    },
    { path: "/:pathMatch(.*)*", redirect: "/dashboard" },
  ],
});

let setupState: boolean | null = null;

router.beforeEach(async (to) => {
  if (setupState === null) {
    try {
      setupState = (await api.setupStatus()).initialized;
    } catch {
      setupState = true;
    }
  }
  if (!setupState && to.name !== "setup") return { name: "setup" };
  if (setupState && to.name === "setup") return { name: "login" };
  if (to.meta.public) return true;

  const session = useSessionStore();
  if (!session.loaded) await session.restore();
  if (!session.session) return { name: "login", query: { redirect: to.fullPath } };
  return true;
});

export function markSetupComplete() {
  setupState = true;
}
