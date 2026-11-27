import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Адрес бэкенда для dev-прокси (в docker compose — http://api-dev:8000)
const API_PROXY = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env.API_PROXY ?? 'http://localhost:8000';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // /api и WebSocket — на бэкенд (support-assistant/backend, npm run dev → порт 8000)
    proxy: {
      '/api': { target: API_PROXY, changeOrigin: true, ws: true },
    },
  },
});
