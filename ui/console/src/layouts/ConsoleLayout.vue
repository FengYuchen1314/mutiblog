<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { Icon } from "@iconify/vue";
import { useSessionStore } from "@/stores/session";
import { AUTH_EXPIRED_EVENT, PUBLICATION_FAILED_EVENT } from "@/api/client";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const mobileOpen = ref(false);
const publicationWarning = ref(false);

const groups = computed(() => [
  {
    label: "",
    items: [{ to: "/dashboard", label: t("navigation.dashboard"), icon: "ri:dashboard-line" }],
  },
  {
    label: t("navigation.content"),
    items: [
      { to: "/posts", label: t("navigation.posts"), icon: "ri:article-line" },
      { to: "/pages", label: t("navigation.pages"), icon: "ri:file-list-3-line" },
      { to: "/categories", label: t("navigation.categories"), icon: "ri:folder-2-line" },
      { to: "/tags", label: t("navigation.tags"), icon: "ri:price-tag-3-line" },
      { to: "/comments", label: t("navigation.comments"), icon: "ri:chat-3-line" },
      { to: "/attachments", label: t("navigation.attachments"), icon: "ri:attachment-2" },
      { to: "/links", label: t("navigation.links"), icon: "ri:links-line" },
    ],
  },
  {
    label: t("navigation.appearance"),
    items: [
      { to: "/theme", label: t("navigation.theme"), icon: "ri:palette-line" },
      { to: "/menus", label: t("navigation.menus"), icon: "ri:menu-2-line" },
    ],
  },
  {
    label: t("navigation.intelligence"),
    items: [
      { to: "/locales", label: t("navigation.locales"), icon: "ri:translate-2" },
      { to: "/ai/providers", label: t("navigation.providers"), icon: "ri:sparkling-2-line" },
      { to: "/tasks", label: t("navigation.tasks"), icon: "ri:git-merge-line" },
    ],
  },
  {
    label: t("navigation.system"),
    items: [
      { to: "/settings", label: t("navigation.settings"), icon: "ri:settings-3-line" },
      { to: "/overview", label: t("navigation.overview"), icon: "ri:information-line" },
      { to: "/backup", label: t("navigation.backup"), icon: "ri:archive-line" },
      { to: "/tools", label: t("navigation.tools"), icon: "ri:tools-line" },
    ],
  },
]);

async function logout() {
  await session.logout();
  await router.push({ name: "login" });
}

function openSearch(event: KeyboardEvent) {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    void router.push("/tools");
  }
}

function handleAuthExpired() {
  if (!session.session) return;
  session.expire();
  void router.replace({ name: "login", query: { redirect: route.fullPath } });
}

function handlePublicationFailed() {
  publicationWarning.value = true;
}

onMounted(() => {
  window.addEventListener("keydown", openSearch);
  window.addEventListener(AUTH_EXPIRED_EVENT, handleAuthExpired);
  window.addEventListener(PUBLICATION_FAILED_EVENT, handlePublicationFailed);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", openSearch);
  window.removeEventListener(AUTH_EXPIRED_EVENT, handleAuthExpired);
  window.removeEventListener(PUBLICATION_FAILED_EVENT, handlePublicationFailed);
});
</script>

<template>
  <div class="console-shell">
    <div v-if="mobileOpen" class="mobile-backdrop" @click="mobileOpen = false" />
    <aside class="sidebar" :class="{ 'sidebar--open': mobileOpen }">
      <a class="brand" href="/" target="_blank">
        <span class="brand-mark">M</span>
        <span>MutiBlog</span>
      </a>
      <a class="visit-site" href="/" target="_blank">
        <Icon icon="ri:external-link-line" />
        {{ t("visitSite") }}
      </a>
      <RouterLink class="global-search" to="/tools">
        <Icon icon="ri:search-line" />
        <span>{{ t("search") }}</span>
        <kbd>⌘ K</kbd>
      </RouterLink>
      <nav class="sidebar-nav">
        <section v-for="group in groups" :key="group.label || 'root'" class="nav-group">
          <div v-if="group.label" class="nav-label">{{ group.label }}</div>
          <RouterLink
            v-for="item in group.items"
            :key="item.to"
            class="nav-item"
			:class="{ 'nav-item--active': route.path === item.to || (item.to === '/theme' && route.path === '/themes') }"
            :to="item.to"
            @click="mobileOpen = false"
          >
            <Icon :icon="item.icon" />
            <span>{{ item.label }}</span>
          </RouterLink>
        </section>
      </nav>
      <div class="profile">
        <div class="profile-avatar">{{ session.session?.username.slice(0, 1).toUpperCase() }}</div>
        <div class="profile-copy">
          <strong>{{ session.session?.username }}</strong>
          <span>{{ t("administrator") }}</span>
        </div>
        <button class="icon-button" type="button" :title="t('logout')" @click="logout">
          <Icon icon="ri:logout-box-r-line" />
        </button>
      </div>
    </aside>
    <main class="console-main">
      <div v-if="publicationWarning" class="global-warning"><span>{{ t("common.publicationFailed") }}</span><button type="button" @click="publicationWarning = false">{{ t("common.dismiss") }}</button></div>
      <button class="mobile-menu" type="button" @click="mobileOpen = true">
        <Icon icon="ri:menu-line" />
      </button>
      <RouterView v-slot="{ Component }">
        <!-- Editor routes share one component. Keying by path prevents a
             post draft, page draft, or another entity ID from reusing the
             previous editor instance and its in-memory Markdown. Query-only
             list filters still retain their component state. -->
        <component :is="Component" :key="route.path" />
      </RouterView>
    </main>
  </div>
</template>
