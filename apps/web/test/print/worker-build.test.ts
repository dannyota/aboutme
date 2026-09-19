// @vitest-environment node

import {
  existsSync,
  mkdtempSync,
  readFileSync,
  rmSync,
} from 'node:fs';
import { join } from 'node:path';

import { afterEach, describe, expect, it } from 'vitest';

import {
  buildPrintDocumentValidator,
} from '../../server/utils/public-render/worker-build';
import {
  buildPrintWorker,
  printWorkerOutput,
} from '../../server/utils/print/worker-build';
import {
  PRINT_WORKER_DEADLINE_MS,
  runPrintWorker,
} from '../../server/utils/print/runner';
import { printEnvelope } from './fixture';

const directories: string[] = [];

afterEach(() => {
  for (const directory of directories.splice(0)) {
    rmSync(directory, { recursive: true, force: true });
  }
});

describe('print worker build', () => {
  it(
    'bundles shared SSR styles with the server sanitizer boundary',
    async () => {
      const directory = mkdtempSync(
        join(process.cwd(), '.nuxt/print-build-test-'),
      );
      directories.push(directory);
      const validator = buildPrintDocumentValidator(directory);
      await buildPrintWorker(directory, validator);

      const worker = join(directory, 'print.mjs');
      const css = join(directory, 'print.css');
      expect(existsSync(worker)).toBe(true);
      expect(existsSync(css)).toBe(true);
      expect(readFileSync(css, 'utf8')).toContain('.resume-document');
      // The public page loads this stylesheet too; it hides the shell's skip
      // link until the link is focused.
      const skipLink
        = /a\[href=(?:"#public-resume"|\\#public-resume)\]:not\(:focus\)/u;
      expect(readFileSync(css, 'utf8')).toMatch(skipLink);
      const source = readFileSync(worker, 'utf8').toLowerCase();
      expect(source).not.toContain('dompurify');
      expect(source).not.toContain('nested-script-tag-stripping-bypass');
      await expect(runPrintWorker(printEnvelope(), {
        signal: new AbortController().signal,
        deadlineMs: PRINT_WORKER_DEADLINE_MS,
        workerUrl: new URL(`file://${worker}`),
      })).resolves.toContain('data-print-document="true"');
    },
  );

  it('reports the emitted Nitro worker path', () => {
    expect(printWorkerOutput('/tmp/nitro'))
      .toBe('/tmp/nitro/server/workers/print.mjs');
  });
});
