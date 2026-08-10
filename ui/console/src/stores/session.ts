import { defineStore } from "pinia";
import { api, type Session } from "@/api/client";

export const useSessionStore = defineStore("session", {
  state: () => ({ session: null as Session | null, loaded: false }),
  actions: {
    async restore() {
      try {
        this.session = await api.session();
      } catch {
        this.session = null;
      } finally {
        this.loaded = true;
      }
    },
    async login(username: string, password: string) {
      this.session = await api.login(username, password);
      this.loaded = true;
    },
    async logout() {
      if (this.session) await api.logout(this.session.csrfToken);
      this.session = null;
    },
  },
});
