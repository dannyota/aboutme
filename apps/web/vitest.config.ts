import { defineVitestConfig } from '@nuxt/test-utils/config';
import { configDefaults } from 'vitest/config';

export default defineVitestConfig({
  test: {
    environment: 'nuxt',
    exclude: [...configDefaults.exclude, 'e2e/**'],
    // The nuxt environment's per-file setup dominates wall time far more
    // than CPU load, so run one worker per vCPU instead of Vitest's default
    // of reserving one core for the main process.
    minWorkers: 4,
    maxWorkers: 4,
  },
});
