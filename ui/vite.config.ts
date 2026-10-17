import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// Tauri 开发时固定 1420 端口（tauri.conf.json 的 devUrl 指向它）
export default defineConfig({
  plugins: [react()],
  clearScreen: false,
  server: { port: 1420, strictPort: true, host: '127.0.0.1' },
  envPrefix: ['VITE_', 'TAURI_ENV_'],
  build: { target: 'safari15', minify: 'esbuild', sourcemap: false, outDir: 'dist' },
  test: { environment: 'node', include: ['src/test/**/*.test.ts'] },
});
