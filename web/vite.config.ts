import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: { proxy: { '/api': 'http://127.0.0.1:8080' } },
  build: { outDir: 'dist', emptyOutDir: true },
  test: {
    maxWorkers: 2, environment: 'jsdom', setupFiles: './src/test/setup.ts', restoreMocks: true,
    // Hosted Windows DOM regressions exceed the default; keep a finite cross-platform budget.
    testTimeout: 15_000,
  },
});
