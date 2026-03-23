import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'jsdom',
    globals: true,
    exclude: ['**/node_modules/**', '**/e2e/**'],
    coverage: {
      provider: 'v8',
      exclude: ['**/*.d.ts', 'src/main.ts', '**/node_modules/**', '**/e2e/**'],
      thresholds: {
        statements: 55,
        branches: 63,
        functions: 30,
        lines: 55,
      },
      reportOnFailure: true,
    },
  },
})
