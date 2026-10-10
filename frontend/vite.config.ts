import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// In development the backend runs on :8080. Proxying /api and /ws keeps the
// frontend on a single origin, so VITE_BACKEND_URL can stay unset locally.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
});
