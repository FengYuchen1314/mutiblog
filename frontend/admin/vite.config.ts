import { defineConfig } from 'vite'
import { resolve } from 'node:path'
export default defineConfig({base:'/admin/',build:{outDir:resolve(__dirname,'../../backend/web/admin'),emptyOutDir:true,rollupOptions:{output:{manualChunks:{editor:['@codemirror/state','@codemirror/view','@codemirror/commands','@codemirror/lang-markdown','@codemirror/theme-one-dark']}}}}})
