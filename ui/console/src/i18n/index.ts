import { createI18n } from "vue-i18n";
import en from "./locales/en";
import zhCN from "./locales/zh-CN";

export type ConsoleLocale = "en" | "zh-CN";

const storedLocale = localStorage.getItem("mutiblog-console-locale");
const initialLocale: ConsoleLocale = storedLocale === "en" || storedLocale === "zh-CN"
  ? storedLocale
  : navigator.language.toLowerCase().startsWith("zh") ? "zh-CN" : "en";

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale,
  fallbackLocale: "en",
  messages: { "zh-CN": zhCN, en },
});

export function setConsoleLocale(locale: string) {
  const supported: ConsoleLocale = locale === "zh-CN" ? "zh-CN" : "en";
  i18n.global.locale.value = supported;
  localStorage.setItem("mutiblog-console-locale", supported);
}
