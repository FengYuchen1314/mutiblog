import { defineStore } from "pinia";
import { api, type Session } from "@/api/client";
import { setConsoleLocale } from "@/i18n";

export const useSessionStore = defineStore("session", {
  state: () => ({ session: null as Session | null, loaded: false }),
  actions: {
    async restore() {
      try {
        this.session = await api.session();
        setConsoleLocale(this.session.adminLocale);
      } catch {
        this.session = null;
      } finally {
        this.loaded = true;
      }
    },
    async login(username: string, password: string) {
      this.session = await api.login(username, password);
      setConsoleLocale(this.session.adminLocale);
      this.loaded = true;
    },
    establish(session: Session) {
      this.session = session;
      setConsoleLocale(session.adminLocale);
      this.loaded = true;
    },
    async logout() {
      try {
        if (this.session) await api.logout(this.session.csrfToken);
      } finally {
        this.session = null;
        this.loaded = true;
      }
    },
    expire() {
      this.session = null;
      this.loaded = true;
    },
  },
});
