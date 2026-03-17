import { defineConfig } from 'vitest/config';
import solid from 'vite-plugin-solid';

export default defineConfig({
  plugins: [solid()],
  test: {
    environment: 'jsdom',
    globals: true,
    exclude: ['test/e2e/**', 'node_modules/**'],
  },
  resolve: {
    conditions: ['development', 'browser'],
  },
});
