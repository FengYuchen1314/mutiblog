import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    alias: {
      "@mutiblog/markdown": fileURLToPath(new URL("../../packages/markdown/src/index.ts", import.meta.url)),
      "@mutiblog/theme-earth": fileURLToPath(new URL("../../themes/earth/src/index.tsx", import.meta.url)),
    },
  },
});
