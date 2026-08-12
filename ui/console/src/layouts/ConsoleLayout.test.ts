// @vitest-environment jsdom

import { flushPromises, mount } from "@vue/test-utils";
import { createI18n } from "vue-i18n";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import en from "@/i18n/locales/en";
import ConsoleLayout from "./ConsoleLayout.vue";

const routerPush = vi.hoisted(() => vi.fn());
const routerReplace = vi.hoisted(() => vi.fn());
const sessionLogout = vi.hoisted(() => vi.fn().mockResolvedValue(undefined));
const route = vi.hoisted(() => ({ path: "/dashboard", fullPath: "/dashboard" }));

vi.mock("@/api/client", () => ({
  AUTH_EXPIRED_EVENT: "console:auth-expired",
  PUBLICATION_FAILED_EVENT: "console:publication-failed",
}));
vi.mock("@/stores/session", () => ({
  useSessionStore: () => ({
    session: { username: "admin" },
    logout: sessionLogout,
    expire: vi.fn(),
  }),
}));
vi.mock("vue-router", () => ({
  useRoute: () => route,
  useRouter: () => ({ push: routerPush, replace: routerReplace }),
}));
vi.mock("@iconify/vue", () => ({ Icon: { template: "<svg aria-hidden='true' />" } }));

let mobileViewport = true;
const mediaListeners = new Set<(event: MediaQueryListEvent) => void>();

function installMediaQuery() {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: vi.fn(
      () =>
        ({
          get matches() {
            return mobileViewport;
          },
          media: "(max-width: 767px)",
          onchange: null,
          addEventListener: (_type: string, listener: (event: MediaQueryListEvent) => void) =>
            mediaListeners.add(listener),
          removeEventListener: (_type: string, listener: (event: MediaQueryListEvent) => void) =>
            mediaListeners.delete(listener),
          addListener: (listener: (event: MediaQueryListEvent) => void) => mediaListeners.add(listener),
          removeListener: (listener: (event: MediaQueryListEvent) => void) => mediaListeners.delete(listener),
          dispatchEvent: () => true,
        }) as MediaQueryList,
    ),
  });
}

function mountLayout() {
  const i18n = createI18n({ legacy: false, locale: "en", messages: { en } });
  return mount(ConsoleLayout, {
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: {
        RouterLink: { props: ["to"], template: "<a :href='to'><slot /></a>" },
        RouterView: { template: "<section />" },
      },
    },
  });
}

describe("ConsoleLayout mobile navigation", () => {
  beforeEach(() => {
    mobileViewport = true;
    mediaListeners.clear();
    routerPush.mockReset();
    routerReplace.mockReset();
    sessionLogout.mockClear();
    document.body.style.overflow = "";
    installMediaQuery();
  });

  afterEach(() => {
    mediaListeners.clear();
    document.body.style.overflow = "";
  });

  test("uses a labelled modal drawer, transfers focus, locks scrolling, and restores focus on Escape", async () => {
    const wrapper = mountLayout();
    const menu = wrapper.get("button[aria-controls='console-primary-navigation']");

    expect(menu.attributes("aria-expanded")).toBe("false");
    await menu.trigger("click");
    await flushPromises();

    const drawer = wrapper.get("[role='dialog']");
    const close = drawer.get("button[aria-label='Dismiss']");
    expect(drawer.attributes("aria-modal")).toBe("true");
    expect(menu.attributes("aria-expanded")).toBe("true");
    expect(document.body.style.overflow).toBe("hidden");
    expect(document.activeElement).toBe(close.element);

    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flushPromises();

    expect(wrapper.find("[role='dialog']").exists()).toBe(false);
    expect(document.body.style.overflow).toBe("");
    expect(document.activeElement).toBe(menu.element);
    wrapper.unmount();
  });
});
