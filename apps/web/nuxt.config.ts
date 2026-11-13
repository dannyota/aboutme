import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import vue from '@vitejs/plugin-vue';
import { build as viteBuild } from 'vite';

import { APP_CSP, HTML_CSP } from './app/utils/csp';
import {
  buildPrintDocumentValidator,
  buildPublicResumeValidator,
} from './server/utils/public-render/worker-build';
import { buildPrintWorker } from './server/utils/print/worker-build';
import {
  PROJECT_PAGE_STYLE,
} from './server/workers/public-render/project-style';
import {
  buildPrintAssets,
  publicScriptVersion,
  publicStyleVersion,
} from './server/utils/print/assets';

const harnessEnabled = process.env.NUXT_HARNESS === '1';
const isolatedBuildTest = process.env.NUXT_BUILD_TEST === '1';
const publicRenderBuildDir = resolve('.nuxt/public-render-worker');
const printBuildDir = resolve('.nuxt/print-worker');
const printAssetsDir = resolve('.nuxt/print-assets');
const printWorker = resolve(printBuildDir, 'print.mjs');
const printDocumentValidator = resolve(
  printBuildDir,
  'print-document-validator.mjs',
);
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

const buildPublicRenderWorker = async (
  styleVersion: string,
  scriptVersion: string,
): Promise<void> => {
  await viteBuild({
    configFile: false,
    define: {
      __ABOUTME_PUBLIC_STYLE_VERSION__: JSON.stringify(styleVersion),
      __ABOUTME_PUBLIC_SCRIPT_VERSION__: JSON.stringify(scriptVersion),
    },
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
      // @lucide/vue's inject() needs the one bundled vue instance during SSR.
      noExternal: ['vue', 'vue/server-renderer', '@lucide/vue'],
      external: ['@aboutme/schema', '@aboutme/schema/sanitizer'],
    },
  });
};

const buildPublicResumeHydration = async (): Promise<void> => {
  await viteBuild({
    configFile: false,
    // Vue's runtime emits process.env.NODE_ENV guards; browsers lack process.
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
    if (id === '#print-worker-url') return '\0print-worker-url';
    if (id === '#print-document-validator') return '\0print-document-validator';
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
    if (id === '\0print-document-validator') {
      if (!existsSync(printDocumentValidator)) {
        throw new Error('Print document validator was not built.');
      }
      return readFileSync(printDocumentValidator, 'utf8');
    }
    if (id === '\0print-worker-url') {
      if (!existsSync(printWorker)) {
        throw new Error('Print worker was not built.');
      }
      this.emitFile({
        type: 'asset',
        fileName: 'workers/print.mjs',
        source: readFileSync(printWorker, 'utf8'),
      });
      return `export default new URL('./workers/print.mjs', `
        + 'import.meta.url).href;';
    }
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
      // Rollup's ROLLUP_FILE_URL breaks under nuxt dev and code-split builds.
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

export default defineNuxtConfig({
  modules: ['@nuxt/eslint', '@pinia/nuxt', 'shadcn-nuxt'],

  ssr: true,

  devtools: { enabled: false },

  app: {
    head: {
      htmlAttrs: { lang: 'en' },
      script: [{ src: '/theme-bootstrap.js' }],
      // iOS Safari takes the first suitable icon and skips SVG: PNG goes first.
      link: [
        {
          rel: 'icon',
          type: 'image/png',
          sizes: '32x32',
          href: '/icon-32-v3.png',
        },
        { rel: 'icon', href: '/favicon-v3.svg', type: 'image/svg+xml' },
        {
          rel: 'apple-touch-icon',
          sizes: '180x180',
          href: '/apple-touch-icon-v3.png',
        },
        { rel: 'manifest', href: '/site-v3.webmanifest' },
      ],
      meta: [
        // Safari auto-links digit runs such as date ranges as tel: links.
        {
          name: 'format-detection',
          content: 'telephone=no, date=no, address=no, email=no',
        },
        { name: 'theme-color', content: '#26409c' },
      ],
    },
  },

  css: ['~/assets/css/tailwind.css'],

  runtimeConfig: {
    printOrigin: 'http://127.0.0.1:20082',
    public: {
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
    '#print-document-validator': printDocumentValidator,
  },

  routeRules: {
    '/**': { headers: { 'Content-Security-Policy': APP_CSP } },
    '/app/**': { headers: { 'X-Robots-Tag': 'noindex' } },
    '/showcase': { headers: { 'X-Robots-Tag': 'noindex, nofollow' } },
    '/app/resumes/**': { ssr: false },
    '/app/import/**': { ssr: false },
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
    defaults: {
      nuxtLink: { prefetchOn: { visibility: false, interaction: true } },
    },
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
    publicAssets: [
      { dir: publicRenderAssetsDir, baseURL: '/_nuxt/assets' },
      { dir: resolve(printAssetsDir, 'fonts'), baseURL: '/_nuxt/fonts' },
    ],
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
    plugins: [tailwindcss(), publicRenderWorkerPlugin(false)],
    // Vite's dev server reloads the page when it discovers a new dependency.
    optimizeDeps: {
      include: [
        '@lucide/vue',
        '@vueuse/core',
        'ajv-formats/dist/formats.js',
        'ajv/dist/runtime/equal.js',
        'ajv/dist/runtime/ucs2length.js',
        'class-variance-authority',
        'clsx',
        'dompurify',
        'openapi-fetch',
        'pdfjs-dist/legacy/build/pdf.mjs',
        'prosemirror-commands',
        'prosemirror-history',
        'prosemirror-keymap',
        'prosemirror-model',
        'prosemirror-schema-list',
        'prosemirror-state',
        'prosemirror-view',
        'reka-ui',
        'tailwind-merge',
        'uqr',
      ],
    },
  },

  hooks: {
    'build:before': () => {
      buildPublicResumeValidator(publicRenderBuildDir);
      buildPrintDocumentValidator(printBuildDir);
    },
    'nitro:build:before': async () => {
      buildPublicResumeValidator(publicRenderBuildDir);
      buildPrintDocumentValidator(printBuildDir);
      await buildPrintWorker(printBuildDir, printDocumentValidator);
      await buildPublicResumeHydration();
      await buildPublicRenderWorker(
        publicStyleVersion(
          readFileSync(resolve(printBuildDir, 'print.css'), 'utf8'),
          readFileSync(resolve('app/assets/css/fonts.css'), 'utf8'),
          PROJECT_PAGE_STYLE,
        ),
        publicScriptVersion(readFileSync(publicResumeHydration, 'utf8')),
      );
      buildPrintAssets(
        publicRenderAssetsDir,
        resolve(printAssetsDir, 'fonts'),
        resolve(printBuildDir, 'print.css'),
        PROJECT_PAGE_STYLE,
      );
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

  shadcn: { prefix: 'Ui', componentDir: '@/components/ui' },
});
