/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// Dev'de /api ve /health istekleri host'taki api'ye (8080) proxy'lenir;
// compose'ta aynı işi nginx yapar. Böylece frontend her zaman aynı origin'e konuşur.
const apiTarget = process.env.API_URL ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  server: {
    proxy: {
      '/api': apiTarget,
      '/health': apiTarget,
    },
  },
  test: {
    environment: 'jsdom',
  },
})
