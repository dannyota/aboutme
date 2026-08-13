import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { HTML_CSP } from './app/utils/csp';

const harnessEnabled = process.env.NUXT_HARNESS === '1';
const isolatedBuildTest = process.env.NUXT_BUILD_TEST === '1';

// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
  modules: ['@nuxt/eslint'],

  // Server-side rendering is required: public resume pages and SEO/GEO
  // surfaces must be crawlable without JS (spec §5).
  ssr: true,

  devtools: { enabled: true },

  css: ['~/assets/css/fonts.css'],

  runtimeConfig: {
    public: {
      // Go API base path. Same-origin in dev/prod (Caddy routes /api/v1/*
      // to the server); override only for local experimentation.
      apiBase: '/api/v1',
    },
  },

  buildDir: harnessEnabled
    ? '.nuxt/harness'
    : isolatedBuildTest
      ? '.nuxt/normal-test'
      : '.nuxt',

  routeRules: harnessEnabled
    ? {
        '/_harness/**': {
          headers: { 'Content-Security-Policy': HTML_CSP },
        },
      }
    : {},

  devServer: {
    port: 3000,
  },

  experimental: {
    entryImportMap: false,
  },
  compatibilityDate: '2026-08-01',

  nitro: {
    output: {
      dir: harnessEnabled
        ? '.output/harness'
        : isolatedBuildTest
          ? '.output/normal-test'
          : '.output',
    },
  },

  hooks: {
    'pages:extend': (pages) => {
      const retained = pages.filter((page) => {
        const file = page.file?.replaceAll('\\', '/');
        if (file === undefined || !file.includes('/app/pages/_harness/')) {
          return true;
        }
        return harnessEnabled && file.endsWith('/_harness/render.vue');
      });
      pages.splice(0, pages.length, ...retained);
      if (harnessEnabled) {
        const harnessRoutes = pages.filter((page) =>
          page.file?.replaceAll('\\', '/').includes('/app/pages/_harness/'),
        );
        if (
          harnessRoutes.length !== 1
          || harnessRoutes[0]?.path !== '/_harness/render'
        ) {
          throw new Error('Harness build must expose only /_harness/render.');
        }
      }
    },
    'ready': (nuxt) => {
      if (!harnessEnabled) return;
      const typeRoot = resolve(nuxt.options.rootDir, '.nuxt');
      mkdirSync(typeRoot, { recursive: true });
      for (const name of ['app', 'node', 'server', 'shared']) {
        const typeConfig = resolve(typeRoot, `tsconfig.${name}.json`);
        if (existsSync(typeConfig)) continue;
        writeFileSync(
          typeConfig,
          `${JSON.stringify({
            extends: `./harness/tsconfig.${name}.json`,
          }, null, 2)}\n`,
          { flag: 'wx' },
        );
      }
    },
  },

  eslint: {
    config: {
      stylistic: {
        indent: 2,
        quotes: 'single',
        semi: true,
        braceStyle: '1tbs',
        arrowParens: true,
        commaDangle: 'always-multiline',
      },
    },
  },
});
