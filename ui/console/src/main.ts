import { VueQueryPlugin } from "@tanstack/vue-query";
import { createPinia } from "pinia";
import { createApp } from "vue";
import App from "./App.vue";
import * as materialComponents from "./components/ui";
import { i18n } from "./i18n";
import { router } from "./router";
import "./styles/tokens.css";
import "./styles/foundations.css";
import "./styles/main.css";

const app = createApp(App);
app.use(createPinia());
app.use(VueQueryPlugin);
app.use(i18n);
app.use(router);
for (const [name, component] of Object.entries(materialComponents)) app.component(name, component);
app.mount("#app");
