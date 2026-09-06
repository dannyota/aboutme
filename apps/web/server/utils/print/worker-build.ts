import { dirname, resolve } from 'node:path';

import vue from '@vitejs/plugin-vue';
import { build as viteBuild } from 'vite';

const editorSanitizer = resolve('app/utils/sanitizeRichText.ts');
const printSanitizer = resolve('server/workers/print/print-sanitize.ts');

const printSanitizerPlugin = () => ({
  name: 'print-sanitizer-boundary',
  resolveId(id: string, importer: string | undefined) {
    if (importer === undefined || !id.startsWith('.')) return undefined;
    const candidate = resolve(dirname(importer), id);
    if (
      candidate === editorSanitizer.slice(0, -3)
      || candidate === editorSanitizer
    ) return printSanitizer;
    return undefined;
  },
});

export async function buildPrintWorker(
  buildDir: string,
  validatorPath: string,
): Promise<void> {
  await viteBuild({
    configFile: false,
    plugins: [printSanitizerPlugin(), vue()],
    resolve: {
      alias: {
        '#print-document-validator': validatorPath,
        [editorSanitizer]: printSanitizer,
        '../../../utils/sanitizeRichText': printSanitizer,
      },
    },
    build: {
      ssr: resolve('server/workers/print/worker-entry.ts'),
      outDir: buildDir,
      emptyOutDir: false,
      cssCodeSplit: false,
      ssrEmitAssets: true,
      rollupOptions: {
        treeshake: { moduleSideEffects: false },
        output: {
          entryFileNames: 'print.mjs',
          assetFileNames: (asset) => asset.names.some((name) =>
            name.endsWith('.css'))
            ? 'print.css'
            : 'assets/[name]-[hash][extname]',
          format: 'es',
        },
      },
    },
    ssr: {
      noExternal: ['vue', 'vue/server-renderer', '@lucide/vue'],
      external: ['@aboutme/schema', '@aboutme/schema/sanitizer'],
    },
  });
}

export const printWorkerOutput = (nitroOutputDir: string): string =>
  resolve(nitroOutputDir, 'server/workers/print.mjs');
