import { defineVitestConfig } from '@nuxt/test-utils/config';
import { configDefaults } from 'vitest/config';

export default defineVitestConfig({
  oxc: {
    // An array preserves .mts transforms when the Vue JSX plugin adds .ts.
    include: [/\.m?ts$/],
  },
  test: {
    environment: 'nuxt',
    exclude: [...configDefaults.exclude, 'e2e/**'],
    minWorkers: 4,
    maxWorkers: 4,
  },
});
