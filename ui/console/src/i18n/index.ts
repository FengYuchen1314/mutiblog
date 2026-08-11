import { createI18n } from "vue-i18n";
import en from "./locales/en";
import zhCN from "./locales/zh-CN";

export type ConsoleLocale = "en" | "zh-CN";

const storageKey = "mutiblog-console-locale";

function readStoredLocale(): string | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage.getItem(storageKey);
  } catch {
    return null;
  }
}

const storedLocale = readStoredLocale();
const initialLocale: ConsoleLocale = storedLocale === "en" || storedLocale === "zh-CN"
  ? storedLocale
  : "zh-CN";

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale,
  fallbackLocale: "zh-CN",
  messages: { "zh-CN": zhCN, en },
});

export function setConsoleLocale(locale: string) {
  const supported: ConsoleLocale = locale === "en" ? "en" : "zh-CN";
  i18n.global.locale.value = supported;
  try {
    if (typeof localStorage !== "undefined") localStorage.setItem(storageKey, supported);
  } catch {
    // Rendering and language switching must remain available when storage is disabled.
  }
}
