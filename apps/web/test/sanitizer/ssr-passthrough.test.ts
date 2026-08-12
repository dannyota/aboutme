// @vitest-environment node

import {
  existsSync,
  readFileSync,
  readdirSync,
  statSync,
} from 'node:fs';
import { join, resolve } from 'node:path';
import { createSSRApp, defineComponent, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import { sanitizeRichText } from '../../app/utils/sanitizeRichText';

describe('sanitizeRichText server path', () => {
  it('returns input byte-identical without a DOM', () => {
    const input = '<p data-unchanged="yes">already <strong>safe</strong></p>';
    expect(sanitizeRichText(input)).toBe(input);
  });

  it('passes an already-sanitized fragment through Vue SSR', async () => {
    const input
      = '<p>already <a href="https://example.com" rel="noopener noreferrer">safe</a></p>';
    const component = defineComponent(() => () =>
      h('div', { innerHTML: sanitizeRichText(input) }),
    );

    expect(await renderToString(createSSRApp(component))).toBe(
      `<div>${input}</div>`,
    );
  });
});

const assertBuiltBundle = process.env.ASSERT_SANITIZER_SERVER_BUNDLE === '1';

describe('built server sanitizer boundary', () => {
  it.runIf(assertBuiltBundle)(
    'contains neither DOMPurify nor jsdom',
    () => {
      const serverOutput = resolve(process.cwd(), '.output/server');
      const source = resolve(
        process.cwd(),
        'app/utils/sanitizeRichText.ts',
      );
      const entry = join(serverOutput, 'index.mjs');
      expect(
        existsSync(entry),
        'run npm run build before this test',
      ).toBe(true);
      expect(
        statSync(entry).mtimeMs,
        'built output is older than sanitizer source',
      ).toBeGreaterThanOrEqual(statSync(source).mtimeMs);

      const files: string[] = [];
      const walk = (directory: string): void => {
        for (const name of readdirSync(directory)) {
          const path = join(directory, name);
          if (statSync(path).isDirectory()) walk(path);
          else files.push(path);
        }
      };
      walk(serverOutput);

      const offending = files.filter((path) =>
        /dompurify|jsdom/i.test(readFileSync(path, 'utf8')),
      );
      expect(offending).toEqual([]);
    },
  );
});
