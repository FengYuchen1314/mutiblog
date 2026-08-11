import { createRouter, createWebHistory } from "vue-router";
import { api } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import SetupView from "@/views/SetupView.vue";
import LoginView from "@/views/LoginView.vue";
import ConsoleLayout from "@/layouts/ConsoleLayout.vue";

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
        { path: "dashboard", name: "dashboard", component: () => import("@/views/DashboardView.vue") },
        { path: "posts", name: "posts", component: () => import("@/views/PostsView.vue") },
        { path: "posts/editor/:id?", name: "post-editor", component: () => import("@/views/PostEditorView.vue") },
        { path: "pages", name: "pages", component: () => import("@/views/PagesView.vue") },
        { path: "pages/editor/:id?", name: "page-editor", component: () => import("@/views/PostEditorView.vue") },
        { path: "categories", name: "categories", component: () => import("@/views/TaxonomiesView.vue") },
        { path: "tags", name: "tags", component: () => import("@/views/TaxonomiesView.vue") },
        { path: "comments", name: "comments", component: () => import("@/views/CommentsView.vue") },
        { path: "links", name: "links", component: () => import("@/views/LinksView.vue") },
        { path: "menus", name: "menus", component: () => import("@/views/MenusView.vue") },
        { path: "theme", name: "theme", component: () => import("@/views/ThemesView.vue") },
        { path: "themes", name: "themes", component: () => import("@/views/ThemesView.vue") },
        { path: "backup", name: "backup", component: () => import("@/views/BackupView.vue") },
        { path: "settings", name: "settings", component: () => import("@/views/SettingsView.vue") },
        { path: "attachments", name: "attachments", component: () => import("@/views/AttachmentsView.vue") },
        { path: "locales", name: "locales", component: () => import("@/views/LocalesView.vue") },
        { path: "ai/providers", name: "ai-providers", component: () => import("@/views/ProvidersView.vue") },
        { path: "ai/tasks", name: "ai-tasks", component: () => import("@/views/TranslationTasksView.vue") },
        { path: "overview", name: "overview", component: () => import("@/views/OverviewView.vue") },
        { path: "tools", name: "tools", component: () => import("@/views/ToolsView.vue") },
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
