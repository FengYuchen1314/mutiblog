import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({ mode }) => ({
  plugins: [react()],
  ssr: { noExternal: ['react', 'react-dom'] },
  build:
    mode === 'ssr'
      ? {
          ssr: 'src/entry.ssr.tsx',
          outDir: 'dist/ssr',
          emptyOutDir: true,
          rollupOptions: {
            output: { format: 'esm', entryFileNames: 'entry.js' },
          },
        }
      : {
          outDir: 'dist',
          emptyOutDir: true,
          manifest: true,
          rollupOptions: {
            input: {
              client: 'src/entry.client.ts',
              style: 'src/styles/main.css',
            },
            output: {
              entryFileNames: 'client/[name]-[hash].js',
              chunkFileNames: 'client/[name]-[hash].js',
              assetFileNames: 'assets/[name]-[hash][extname]',
              manualChunks: {
                react: ['react', 'react-dom/client'],
              },
            },
          },
        },
}))
