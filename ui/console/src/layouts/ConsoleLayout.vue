<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { Icon } from "@iconify/vue";
import { AUTH_EXPIRED_EVENT, PUBLICATION_FAILED_EVENT } from "@/api/client";
import { useSessionStore } from "@/stores/session";

const MOBILE_QUERY = "(max-width: 767px)";
const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const mobileOpen = ref(false);
const publicationWarning = ref(false);
const drawerRef = ref<HTMLElement | null>(null);
const menuButtonRef = ref<HTMLButtonElement | null>(null);
const closeButtonRef = ref<HTMLButtonElement | null>(null);
const previousFocus = ref<HTMLElement | null>(null);
const isMobile = ref(getIsMobileViewport());

let mediaQuery: MediaQueryList | undefined;
let savedBodyOverflow: string | null = null;

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

const activeNavigation = computed(() => groups.value.flatMap((group) => group.items).find(({ to }) => isActive(to)));
const pageTitle = computed(() => activeNavigation.value?.label ?? t("brand"));
const pageIcon = computed(() => activeNavigation.value?.icon ?? "ri:dashboard-line");
const usernameInitial = computed(() => session.session?.username?.slice(0, 1).toUpperCase() || "A");

function getIsMobileViewport() {
  if (typeof window === "undefined") return false;
  if (typeof window.matchMedia === "function") return window.matchMedia(MOBILE_QUERY).matches;
  return window.innerWidth <= 767;
}

function isActive(to: string) {
  if (route.path === to) return true;
  if (to === "/theme" && route.path === "/themes") return true;
  if (to === "/posts" && route.path.startsWith("/posts/editor")) return true;
  return to === "/pages" && route.path.startsWith("/pages/editor");
}

function lockPageScroll() {
  if (typeof document === "undefined" || savedBodyOverflow !== null) return;
  savedBodyOverflow = document.body.style.overflow;
  document.body.style.overflow = "hidden";
}

function unlockPageScroll() {
  if (typeof document === "undefined" || savedBodyOverflow === null) return;
  document.body.style.overflow = savedBodyOverflow;
  savedBodyOverflow = null;
}

async function openNavigation() {
  if (!isMobile.value || mobileOpen.value) return;
  const activeElement = document.activeElement;
  previousFocus.value =
    activeElement instanceof HTMLElement && activeElement !== document.body ? activeElement : menuButtonRef.value;
  mobileOpen.value = true;
  await nextTick();
  if (mobileOpen.value && isMobile.value) closeButtonRef.value?.focus();
}

async function closeNavigation({ restoreFocus = true }: { restoreFocus?: boolean } = {}) {
  if (!mobileOpen.value) return;
  mobileOpen.value = false;
  unlockPageScroll();

  const target = previousFocus.value ?? menuButtonRef.value;
  previousFocus.value = null;
  if (!restoreFocus) return;
  await nextTick();
  if (target?.isConnected) target.focus();
}

function handleDrawerNavigation() {
  if (isMobile.value) void closeNavigation({ restoreFocus: false });
}

async function logout() {
  if (isMobile.value) await closeNavigation({ restoreFocus: false });
  await session.logout();
  await router.push({ name: "login" });
}

function openSearch(event: KeyboardEvent) {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    if (mobileOpen.value) void closeNavigation({ restoreFocus: false });
    void router.push("/tools");
  }
}

function trapDrawerFocus(event: KeyboardEvent) {
  if (!isMobile.value || !mobileOpen.value || event.key !== "Tab") return;
  const drawer = drawerRef.value;
  if (!drawer) return;

  const focusable = Array.from(drawer.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (element) => !element.hasAttribute("disabled") && element.getAttribute("aria-hidden") !== "true",
  );
  if (focusable.length === 0) {
    event.preventDefault();
    return;
  }

  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  const activeElement = document.activeElement;
  if (!drawer.contains(activeElement)) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
  } else if (event.shiftKey && activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

function handleKeydown(event: KeyboardEvent) {
  if (mobileOpen.value && event.key === "Escape") {
    event.preventDefault();
    void closeNavigation();
    return;
  }
  if (event.defaultPrevented) return;
  trapDrawerFocus(event);
  if (!event.defaultPrevented) openSearch(event);
}

function handleAuthExpired() {
  if (!session.session) return;
  session.expire();
  void router.replace({ name: "login", query: { redirect: route.fullPath } });
}

function handlePublicationFailed() {
  publicationWarning.value = true;
}

function updateViewportMode() {
  const nextMobile = mediaQuery?.matches ?? getIsMobileViewport();
  if (!nextMobile && mobileOpen.value) void closeNavigation({ restoreFocus: false });
  isMobile.value = nextMobile;
}

watch([isMobile, mobileOpen], ([mobile, open]) => {
  if (mobile && open) lockPageScroll();
  else unlockPageScroll();
});

watch(
  () => route.fullPath,
  () => {
    if (mobileOpen.value) void closeNavigation({ restoreFocus: false });
  },
);

onMounted(() => {
  mediaQuery = window.matchMedia?.(MOBILE_QUERY);
  updateViewportMode();
  mediaQuery?.addEventListener("change", updateViewportMode);
  window.addEventListener("resize", updateViewportMode);
  window.addEventListener("keydown", handleKeydown);
  window.addEventListener(AUTH_EXPIRED_EVENT, handleAuthExpired);
  window.addEventListener(PUBLICATION_FAILED_EVENT, handlePublicationFailed);
});

onBeforeUnmount(() => {
  mediaQuery?.removeEventListener("change", updateViewportMode);
  window.removeEventListener("resize", updateViewportMode);
  window.removeEventListener("keydown", handleKeydown);
  window.removeEventListener(AUTH_EXPIRED_EVENT, handleAuthExpired);
  window.removeEventListener(PUBLICATION_FAILED_EVENT, handlePublicationFailed);
  unlockPageScroll();
});
</script>

<template>
  <div class="console-layout">
    <Transition name="console-layout-backdrop">
      <div
        v-if="isMobile && mobileOpen"
        class="console-layout__backdrop"
        aria-hidden="true"
        @click="closeNavigation()"
      />
    </Transition>

    <Transition name="console-layout-drawer">
      <aside
        v-if="!isMobile || mobileOpen"
        id="console-primary-navigation"
        ref="drawerRef"
        class="console-layout__drawer"
        :class="{ 'console-layout__drawer--modal': isMobile }"
        :role="isMobile ? 'dialog' : undefined"
        :aria-modal="isMobile ? 'true' : undefined"
        :aria-hidden="isMobile && !mobileOpen ? 'true' : undefined"
        :inert="isMobile && !mobileOpen ? true : undefined"
        aria-labelledby="console-navigation-title"
      >
        <div class="console-layout__drawer-header">
          <a class="console-layout__brand" href="/" target="_blank" rel="noopener noreferrer" aria-label="MutiBlog">
            <span class="console-layout__brand-mark" aria-hidden="true">M</span>
            <span class="console-layout__brand-copy">MutiBlog</span>
          </a>
          <button
            v-if="isMobile"
            ref="closeButtonRef"
            class="console-layout__icon-button"
            type="button"
            :aria-label="t('common.dismiss')"
            :title="t('common.dismiss')"
            @click="closeNavigation()"
          >
            <Icon icon="ri:close-line" />
          </button>
        </div>

        <h2 id="console-navigation-title" class="console-layout__sr-only">{{ t("menusPage.menu") }}</h2>

        <div class="console-layout__quick-actions">
          <a
            class="console-layout__quick-action"
            href="/"
            target="_blank"
            rel="noopener noreferrer"
            :aria-label="t('visitSite')"
            :title="t('visitSite')"
          >
            <Icon icon="ri:external-link-line" />
            <span>{{ t("visitSite") }}</span>
          </a>
          <RouterLink
            class="console-layout__quick-action"
            to="/tools"
            :aria-label="t('search')"
            :title="t('search')"
            @click="handleDrawerNavigation"
          >
            <Icon icon="ri:search-line" />
            <span>{{ t("search") }}</span>
            <kbd>⌘ K</kbd>
          </RouterLink>
        </div>

        <nav class="console-layout__navigation" :aria-label="t('menusPage.menu')">
          <section v-for="group in groups" :key="group.label || 'root'" class="console-layout__navigation-group">
            <div v-if="group.label" class="console-layout__navigation-label">{{ group.label }}</div>
            <RouterLink
              v-for="item in group.items"
              :key="item.to"
              class="console-layout__navigation-item"
              :class="{ 'console-layout__navigation-item--active': isActive(item.to) }"
              :to="item.to"
              :aria-label="item.label"
              :title="item.label"
              @click="handleDrawerNavigation"
            >
              <Icon :icon="item.icon" />
              <span>{{ item.label }}</span>
            </RouterLink>
          </section>
        </nav>

        <div class="console-layout__profile">
          <div class="console-layout__avatar" aria-hidden="true">{{ usernameInitial }}</div>
          <div class="console-layout__profile-copy">
            <strong>{{ session.session?.username }}</strong>
            <span>{{ t("administrator") }}</span>
          </div>
          <button
            class="console-layout__icon-button"
            type="button"
            :aria-label="t('logout')"
            :title="t('logout')"
            @click="logout"
          >
            <Icon icon="ri:logout-box-r-line" />
          </button>
        </div>
      </aside>
    </Transition>

    <main
      id="console-main-content"
      class="console-layout__main"
      :inert="isMobile && mobileOpen ? true : undefined"
      tabindex="-1"
    >
      <header class="console-layout__app-bar">
        <div class="console-layout__app-bar-start">
          <button
            v-if="isMobile"
            ref="menuButtonRef"
            class="console-layout__menu-button"
            type="button"
            aria-controls="console-primary-navigation"
            :aria-expanded="mobileOpen"
            :aria-label="t('menusPage.menu')"
            :title="t('menusPage.menu')"
            @click="openNavigation"
          >
            <Icon icon="ri:menu-line" />
          </button>
          <div class="console-layout__page-identity">
            <Icon :icon="pageIcon" aria-hidden="true" />
            <span>{{ pageTitle }}</span>
          </div>
        </div>

        <div class="console-layout__app-bar-actions">
          <RouterLink
            class="console-layout__app-bar-action console-layout__app-bar-search"
            to="/tools"
            :aria-label="t('search')"
            :title="t('search')"
          >
            <Icon icon="ri:search-line" />
            <span>{{ t("search") }}</span>
            <kbd>⌘ K</kbd>
          </RouterLink>
          <a
            class="console-layout__app-bar-action"
            href="/"
            target="_blank"
            rel="noopener noreferrer"
            :aria-label="t('visitSite')"
            :title="t('visitSite')"
          >
            <Icon icon="ri:external-link-line" />
            <span>{{ t("visitSite") }}</span>
          </a>
          <button
            class="console-layout__app-bar-action console-layout__app-bar-icon-button"
            type="button"
            :aria-label="t('logout')"
            :title="t('logout')"
            @click="logout"
          >
            <Icon icon="ri:logout-box-r-line" />
            <span>{{ t("logout") }}</span>
          </button>
        </div>
      </header>

      <div v-if="publicationWarning" class="console-layout__warning" role="status">
        <span>{{ t("common.publicationFailed") }}</span>
        <button type="button" @click="publicationWarning = false">{{ t("common.dismiss") }}</button>
      </div>

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

<style scoped>
.console-layout {
  --console-surface: var(--m-sys-color-surface, #f9f9ff);
  --console-surface-container: var(--m-sys-color-surface-container, #ededf4);
  --console-surface-container-high: var(--m-sys-color-surface-container-high, #e7e8ee);
  --console-on-surface: var(--m-sys-color-on-surface, #191c20);
  --console-on-surface-variant: var(--m-sys-color-on-surface-variant, #44474e);
  --console-outline: var(--m-sys-color-outline-variant, #c4c6d0);
  --console-primary: var(--m-sys-color-primary, #0b57d0);
  --console-on-primary: var(--m-sys-color-on-primary, #ffffff);
  --console-primary-container: var(--m-sys-color-primary-container, #d8e2ff);
  --console-on-primary-container: var(--m-sys-color-on-primary-container, #001a41);
  min-height: 100vh;
  background:
    radial-gradient(circle at 9% -12%, rgb(216 226 255 / 0.68), transparent 27rem),
    radial-gradient(circle at 94% 3%, rgb(219 226 255 / 0.52), transparent 24rem), var(--console-surface);
  color: var(--console-on-surface);
}

.console-layout__drawer {
  position: fixed;
  inset: 0 auto 0 0;
  z-index: 60;
  display: flex;
  width: min(20.5rem, calc(100vw - 2.75rem));
  flex-direction: column;
  overflow: hidden;
  border: 1px solid rgb(121 116 126 / 0.2);
  border-left: 0;
  border-radius: 0 1.5rem 1.5rem 0;
  background: rgb(254 251 255 / 0.91);
  box-shadow: var(--m-sys-elevation-level4, 0 16px 40px rgb(25 28 32 / 0.2));
  backdrop-filter: blur(22px) saturate(1.18);
}

.console-layout__drawer-header {
  display: flex;
  min-height: 5rem;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0 1.15rem;
}

.console-layout__brand {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 0.75rem;
  border-radius: 999px;
  color: var(--console-on-surface);
  font-size: 1.06rem;
  font-weight: 760;
  letter-spacing: -0.02em;
  outline: none;
}

.console-layout__brand:focus-visible,
.console-layout__quick-action:focus-visible,
.console-layout__navigation-item:focus-visible,
.console-layout__icon-button:focus-visible,
.console-layout__menu-button:focus-visible,
.console-layout__app-bar-action:focus-visible,
.console-layout__warning button:focus-visible {
  box-shadow: var(--m-sys-focus-ring, 0 0 0 3px rgb(11 87 208 / 0.36));
}

.console-layout__brand-mark,
.console-layout__avatar {
  display: inline-flex;
  width: 2.35rem;
  height: 2.35rem;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border-radius: 0.78rem;
  background: var(--console-primary);
  box-shadow: var(--m-sys-elevation-level2, 0 3px 8px rgb(11 87 208 / 0.26));
  color: var(--console-on-primary);
  font-size: 0.92rem;
  font-weight: 820;
}

.console-layout__sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  clip-path: inset(50%);
}

.console-layout__quick-actions {
  display: grid;
  gap: 0.45rem;
  padding: 0 0.8rem 0.85rem;
}

.console-layout__quick-action,
.console-layout__app-bar-action {
  display: inline-flex;
  min-height: 2.65rem;
  align-items: center;
  gap: 0.65rem;
  border: 1px solid transparent;
  border-radius: 0.85rem;
  color: var(--console-on-surface-variant);
  font-size: 0.84rem;
  font-weight: 640;
  text-decoration: none;
  transition:
    background-color 160ms ease,
    color 160ms ease,
    transform 160ms ease;
}

.console-layout__app-bar-icon-button {
  border: 0;
  background: transparent;
  cursor: pointer;
  font-family: inherit;
}

.console-layout__quick-action {
  padding: 0 0.85rem;
}

.console-layout__quick-action:hover,
.console-layout__app-bar-action:hover {
  background: var(--console-primary-container);
  color: var(--console-primary);
}

.console-layout__quick-action svg,
.console-layout__app-bar-action svg,
.console-layout__navigation-item svg,
.console-layout__icon-button svg,
.console-layout__menu-button svg,
.console-layout__page-identity svg {
  width: 1.2rem;
  height: 1.2rem;
  flex: 0 0 auto;
}

.console-layout__quick-action kbd,
.console-layout__app-bar-action kbd {
  margin-left: auto;
  border: 1px solid var(--console-outline);
  border-radius: 0.4rem;
  background: rgb(255 255 255 / 0.54);
  padding: 0.12rem 0.34rem;
  color: var(--console-on-surface-variant);
  font-family: inherit;
  font-size: 0.68rem;
  font-weight: 650;
}

.console-layout__navigation {
  min-height: 0;
  flex: 1;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 0.15rem 0.8rem 1rem;
}

.console-layout__navigation-group + .console-layout__navigation-group {
  margin-top: 0.9rem;
}

.console-layout__navigation-label {
  padding: 0.25rem 0.9rem 0.45rem;
  color: var(--console-on-surface-variant);
  font-size: 0.68rem;
  font-weight: 730;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.console-layout__navigation-item {
  display: flex;
  min-height: 2.78rem;
  align-items: center;
  gap: 0.85rem;
  border-radius: 1.39rem;
  padding: 0.25rem 0.9rem;
  color: var(--console-on-surface-variant);
  font-size: 0.88rem;
  font-weight: 600;
  outline: none;
  text-decoration: none;
  transition:
    background-color 160ms ease,
    color 160ms ease,
    transform 160ms ease;
}

.console-layout__navigation-item:hover {
  background: var(--console-primary-container);
  color: var(--console-on-surface);
}

.console-layout__navigation-item:active,
.console-layout__quick-action:active,
.console-layout__app-bar-action:active,
.console-layout__icon-button:active,
.console-layout__menu-button:active {
  transform: scale(0.97);
}

.console-layout__navigation-item--active {
  background: var(--console-primary-container);
  color: var(--console-on-primary-container);
  font-weight: 760;
}

.console-layout__navigation-item--active svg {
  color: var(--console-primary);
}

.console-layout__profile {
  display: flex;
  min-height: 5.15rem;
  align-items: center;
  gap: 0.72rem;
  border-top: 1px solid rgb(121 116 126 / 0.18);
  padding: 0.82rem 1rem;
}

.console-layout__avatar {
  width: 2.3rem;
  height: 2.3rem;
  border-radius: 50%;
  background: var(--console-primary-container);
  box-shadow: none;
  color: var(--console-on-primary-container);
}

.console-layout__profile-copy {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: 0.12rem;
}

.console-layout__profile-copy strong,
.console-layout__profile-copy span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.console-layout__profile-copy strong {
  color: var(--console-on-surface);
  font-size: 0.82rem;
}

.console-layout__profile-copy span {
  color: var(--console-on-surface-variant);
  font-size: 0.7rem;
}

.console-layout__icon-button,
.console-layout__menu-button {
  display: inline-flex;
  width: 2.55rem;
  height: 2.55rem;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: var(--console-on-surface-variant);
  cursor: pointer;
  outline: none;
  transition:
    background-color 160ms ease,
    color 160ms ease,
    transform 160ms ease;
}

.console-layout__icon-button:hover,
.console-layout__menu-button:hover {
  background: var(--console-primary-container);
  color: var(--console-primary);
}

.console-layout__main {
  min-width: 0;
  min-height: 100vh;
}

.console-layout__app-bar {
  position: sticky;
  z-index: 30;
  top: 0;
  display: flex;
  min-height: 4.25rem;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  border-bottom: 1px solid rgb(121 116 126 / 0.14);
  background: rgb(254 251 255 / 0.76);
  padding: 0.55rem clamp(1rem, 2.4vw, 2rem);
  backdrop-filter: blur(18px) saturate(1.14);
}

.console-layout__app-bar-start,
.console-layout__app-bar-actions,
.console-layout__page-identity {
  display: flex;
  min-width: 0;
  align-items: center;
}

.console-layout__app-bar-start {
  gap: 0.55rem;
}

.console-layout__page-identity {
  gap: 0.62rem;
  color: var(--console-on-surface);
  font-size: 1.02rem;
  font-weight: 720;
  letter-spacing: -0.01em;
}

.console-layout__page-identity svg {
  color: var(--console-primary);
}

.console-layout__app-bar-actions {
  gap: 0.25rem;
}

.console-layout__app-bar-action {
  min-height: 2.45rem;
  padding: 0 0.75rem;
}

.console-layout__warning {
  position: sticky;
  z-index: 20;
  top: 4.25rem;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  border-bottom: 1px solid var(--m-sys-color-warning, #855400);
  background: var(--m-sys-color-warning-container, #ffddb0);
  padding: 0.7rem clamp(1rem, 2.4vw, 2rem);
  color: var(--m-sys-color-on-warning-container, #2a1700);
  font-size: 0.82rem;
}

.console-layout__warning button {
  border: 0;
  border-radius: 999px;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font-weight: 700;
  text-decoration: underline;
}

.console-layout__backdrop {
  position: fixed;
  z-index: 50;
  inset: 0;
  background: rgb(29 27 32 / 0.42);
  backdrop-filter: blur(2px);
}

.console-layout-drawer-enter-active,
.console-layout-drawer-leave-active {
  transition:
    transform 240ms cubic-bezier(0.2, 0, 0, 1),
    opacity 180ms ease;
}

.console-layout-drawer-enter-from,
.console-layout-drawer-leave-to {
  opacity: 0;
  transform: translateX(-108%);
}

.console-layout-backdrop-enter-active,
.console-layout-backdrop-leave-active {
  transition: opacity 180ms ease;
}

.console-layout-backdrop-enter-from,
.console-layout-backdrop-leave-to {
  opacity: 0;
}

@media (min-width: 1200px) {
  .console-layout__drawer {
    width: 17.25rem;
    border-radius: 0;
    box-shadow: 1px 0 2px rgb(29 27 32 / 0.1);
  }

  .console-layout__main {
    margin-left: 17.25rem;
  }
}

@media (min-width: 768px) and (max-width: 1199px) {
  .console-layout__drawer {
    width: 5.25rem;
    align-items: center;
    border-radius: 0;
    box-shadow: 1px 0 2px rgb(29 27 32 / 0.1);
  }

  .console-layout__main {
    margin-left: 5.25rem;
  }

  .console-layout__drawer-header {
    min-height: 4.65rem;
    justify-content: center;
    padding: 0;
  }

  .console-layout__brand-copy,
  .console-layout__quick-action > span,
  .console-layout__quick-action kbd,
  .console-layout__navigation-label,
  .console-layout__navigation-item > span,
  .console-layout__profile-copy {
    display: none;
  }

  .console-layout__quick-actions {
    width: 100%;
    padding: 0 0.75rem 0.8rem;
  }

  .console-layout__quick-action,
  .console-layout__navigation-item {
    width: 100%;
    justify-content: center;
    padding: 0;
  }

  .console-layout__navigation {
    width: 100%;
    padding: 0.15rem 0.75rem 0.85rem;
  }

  .console-layout__navigation-group + .console-layout__navigation-group {
    margin-top: 0.65rem;
  }

  .console-layout__profile {
    width: 100%;
    justify-content: center;
    padding: 0.78rem 0;
  }

  .console-layout__profile .console-layout__icon-button {
    display: none;
  }
}

@media (max-width: 767px) {
  .console-layout__drawer {
    padding-bottom: env(safe-area-inset-bottom);
  }

  .console-layout__app-bar {
    min-height: 4rem;
    gap: 0.5rem;
    padding: 0.48rem 0.85rem;
  }

  .console-layout__page-identity {
    overflow: hidden;
    font-size: 0.95rem;
  }

  .console-layout__page-identity span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .console-layout__app-bar-actions {
    gap: 0;
  }

  .console-layout__app-bar-action {
    width: 2.45rem;
    justify-content: center;
    padding: 0;
  }

  .console-layout__app-bar-action span,
  .console-layout__app-bar-action kbd {
    display: none;
  }

  .console-layout__warning {
    top: 4rem;
    padding: 0.65rem 0.9rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .console-layout *,
  .console-layout-drawer-enter-active,
  .console-layout-drawer-leave-active,
  .console-layout-backdrop-enter-active,
  .console-layout-backdrop-leave-active {
    scroll-behavior: auto !important;
    transition-duration: 1ms !important;
  }
}
</style>
