import { VueQueryPlugin } from "@tanstack/vue-query";
import { createPinia } from "pinia";
import { createApp } from "vue";
import "@halo-dev/components/dist/style.css";
import App from "./App.vue";
import { i18n } from "./i18n";
import { router } from "./router";
import "./styles/main.css";

const app = createApp(App);
app.use(createPinia());
app.use(VueQueryPlugin);
app.use(i18n);
app.use(router);
app.mount("#app");
