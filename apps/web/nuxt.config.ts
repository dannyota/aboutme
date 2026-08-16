import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import vue from '@vitejs/plugin-vue';
import { build as viteBuild } from 'vite';

import { HTML_CSP } from './app/utils/csp';
import {
  buildPublicResumeValidator,
} from './server/utils/public-render/worker-build';

const harnessEnabled = process.env.NUXT_HARNESS === '1';
const isolatedBuildTest = process.env.NUXT_BUILD_TEST === '1';
const publicRenderBuildDir = resolve('.nuxt/public-render-worker');
const publicRenderAssetsDir = resolve('.nuxt/public-render-assets');
const publicRenderWorker = resolve(publicRenderBuildDir, 'public-render.mjs');
const publicResumeHydration = resolve(
  publicRenderAssetsDir,
  'public-resume.mjs',
);
const publicResumeValidator = resolve(
  publicRenderBuildDir,
  'public-resume-validator.mjs',
);
const editorSanitizer = resolve('app/utils/sanitizeRichText.ts');
const publicSanitizer = resolve(
  'server/workers/public-render/public-sanitize.ts',
);

const publicRenderSanitizerPlugin = () => ({
  name: 'public-render-sanitizer-boundary',
  resolveId(id: string, importer: string | undefined) {
    if (importer === undefined || !id.startsWith('.')) return undefined;
    const candidate = resolve(dirname(importer), id);
    if (
      candidate === editorSanitizer.slice(0, -3)
      || candidate === editorSanitizer
    ) {
      return publicSanitizer;
    }
    return undefined;
  },
});

const buildPublicRenderWorker = async (): Promise<void> => {
  await viteBuild({
    configFile: false,
    plugins: [publicRenderSanitizerPlugin(), vue()],
    resolve: {
      alias: {
        '#public-render-validator': publicResumeValidator,
        [editorSanitizer]: publicSanitizer,
        '../../../utils/sanitizeRichText': publicSanitizer,
      },
    },
    build: {
      ssr: resolve('server/workers/public-render/worker-entry.ts'),
      outDir: publicRenderBuildDir,
      // The validator and hydration asset share this private build directory.
      emptyOutDir: false,
      rollupOptions: {
        treeshake: { moduleSideEffects: false },
        output: {
          entryFileNames: 'public-render.mjs',
          format: 'es',
          inlineDynamicImports: true,
        },
      },
    },
    ssr: {
      // @lucide/vue must be bundled with this build's single vue instance;
      // externalizing it leaves its inject() calling a second node_modules
      // vue, which has no current instance during SSR and fails the render.
      noExternal: ['vue', 'vue/server-renderer', '@lucide/vue'],
      external: ['@aboutme/schema', '@aboutme/schema/sanitizer'],
    },
  });
};

const buildPublicResumeHydration = async (): Promise<void> => {
  await viteBuild({
    configFile: false,
    // This client bundle runs in a browser, where `process` does not exist.
    // Vue's runtime emits `process.env.NODE_ENV` dev-warning guards, so
    // replace them with a literal to keep the bundle browser-safe.
    define: { 'process.env.NODE_ENV': JSON.stringify('production') },
    plugins: [
      publicRenderSanitizerPlugin(),
      vue(),
      publicRenderWorkerPlugin(false),
    ],
    resolve: {
      alias: {
        [editorSanitizer]: publicSanitizer,
        '../../../utils/sanitizeRichText': publicSanitizer,
      },
    },
    build: {
      lib: {
        entry: resolve('app/public/public-resume.client.ts'),
        formats: ['es'],
        fileName: () => 'public-resume.mjs',
      },
      outDir: publicRenderAssetsDir,
      emptyOutDir: false,
      rollupOptions: {
        output: { inlineDynamicImports: true },
      },
    },
  });
};

const publicRenderWorkerPlugin = (emitAssets = true) => ({
  name: 'public-render-worker-modules',
  buildStart(
    this: {
      emitFile: (asset: {
        type: 'asset';
        name: string;
        source: string;
      }) => string;
    },
  ) {
    if (!emitAssets) return;
    if (!existsSync(publicResumeHydration)) {
      throw new Error('Public resume hydration asset was not built.');
    }
    this.emitFile({
      type: 'asset',
      name: 'public-resume.mjs',
      source: readFileSync(publicResumeHydration, 'utf8'),
    });
  },
  resolveId(id: string) {
    if (id === '#public-render-worker-url') return '\0public-render-worker-url';
    if (id === '#public-render-validator') return '\0public-render-validator';
    return undefined;
  },
  load(
    this: {
      emitFile: (asset: {
        type: 'asset';
        fileName: string;
        source: string;
      }) => string;
    },
    id: string,
  ) {
    if (id === '\0public-render-validator') {
      if (!existsSync(publicResumeValidator)) {
        throw new Error('PublicResume validator was not built.');
      }
      return readFileSync(publicResumeValidator, 'utf8');
    }
    if (id === '\0public-render-worker-url') {
      if (!existsSync(publicRenderWorker)) {
        throw new Error('Public render worker was not built.');
      }
      // Resolve against the server entry (import.meta.url), not the importing
      // chunk: ROLLUP_FILE_URL breaks under nuxt dev and code-split builds.
      this.emitFile({
        type: 'asset',
        fileName: 'workers/public-render.mjs',
        source: readFileSync(publicRenderWorker, 'utf8'),
      });
      return `export default new URL('./workers/public-render.mjs', `
        + 'import.meta.url).href;';
    }
    return undefined;
  },
});

// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
  modules: ['@nuxt/eslint', '@pinia/nuxt'],

  // Server-side rendering is required: public resume pages and SEO/GEO
  // surfaces must be crawlable without JS (spec §5).
  ssr: true,

  devtools: { enabled: false },

  app: {
    head: {
      htmlAttrs: { lang: 'en' },
      script: [{ src: '/theme-bootstrap.js' }],
    },
  },

  css: ['~/assets/css/fonts.css', '~/assets/css/app.css'],

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

  alias: {
    '#public-render-validator': publicResumeValidator,
  },

  routeRules: {
    '/app/resumes/**': { ssr: false },
    ...(harnessEnabled
      ? {
          '/_harness/**': {
            headers: { 'Content-Security-Policy': HTML_CSP },
          },
        }
      : {}),
  },

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
    publicAssets: [{
      dir: publicRenderAssetsDir,
      baseURL: '/_nuxt/assets',
    }],
    rollupConfig: {
      plugins: [publicRenderWorkerPlugin()],
      output: {
        assetFileNames: (asset) => asset.name === 'public-resume.mjs'
          ? 'assets/public-resume.mjs'
          : 'assets/[name]-[hash][extname]',
      },
    },
  },

  vite: {
    plugins: [publicRenderWorkerPlugin(false)],
  },

  hooks: {
    'build:before': () => {
      buildPublicResumeValidator(publicRenderBuildDir);
    },
    'nitro:build:before': async () => {
      buildPublicResumeValidator(publicRenderBuildDir);
      await buildPublicRenderWorker();
      await buildPublicResumeHydration();
    },
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
          `${JSON.stringify(
            {
              extends: `./harness/tsconfig.${name}.json`,
            },
            null,
            2,
          )}\n`,
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
