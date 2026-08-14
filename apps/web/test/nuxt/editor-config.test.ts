// @vitest-environment node

import currentSchema from '@aboutme/schema/current-schema';
import { validateDocument } from '@aboutme/schema/validation';
import { loadNuxtConfig } from 'nuxt/kit';
import { defineStore } from 'pinia';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const webRoot = resolve(import.meta.dirname, '../..');

describe('editor runtime prerequisites', () => {
  it(
    'loads the schema, Pinia, editor SPA rule, and renderer harness',
    async () => {
      expect(currentSchema.$id).toBe('https://aboutme.vn/schema/resume/v2');
      expect(validateDocument).toBeTypeOf('function');
      expect(defineStore).toBeTypeOf('function');

      const previousHarness = process.env.NUXT_HARNESS;
      process.env.NUXT_HARNESS = '1';
      try {
        const config = await loadNuxtConfig({ cwd: webRoot });
        expect(config.routeRules?.['/app/resumes/**']).toEqual({ ssr: false });
        expect(config.modules).toContain('@pinia/nuxt');
        expect(config.routeRules?.['/_harness/**']?.headers).toHaveProperty(
          'Content-Security-Policy',
        );
      } finally {
        if (previousHarness === undefined) {
          delete process.env.NUXT_HARNESS;
        } else {
          process.env.NUXT_HARNESS = previousHarness;
        }
      }
    },
  );
});
