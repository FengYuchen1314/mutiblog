import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";

export default defineConfig({
  base: "/console/",
  plugins: [vue()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      "@mutiblog/markdown": fileURLToPath(new URL("../../packages/markdown/src/index.ts", import.meta.url)),
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("node_modules") && id.includes("@codemirror+lang-")) return "vendor-codemirror-languages";
          if (id.includes("node_modules") && (id.includes("@codemirror") || /[/\\]codemirror@/.test(id))) return "vendor-codemirror";
          if (id.includes("node_modules") && ["markdown-it", "linkify-it", "mdurl", "punycode.js"].some((name) => id.includes(name))) return "vendor-markdown";
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/health": "http://127.0.0.1:8080",
    },
  },
});
