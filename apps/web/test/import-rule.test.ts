import { ESLint } from 'eslint';
import { describe, expect, test } from 'vitest';

const rendererPath = 'app/components/resume/__lint__/fixture.ts';
const taskRuleIds = new Set([
  'aboutme/no-renderer-nondeterminism',
  'no-restricted-globals',
  'no-restricted-imports',
]);
const eslint = new ESLint({
  cwd: process.cwd(),
  overrideConfigFile: 'eslint.config.mjs',
});

async function lint(code: string, filePath = rendererPath) {
  const normalizedCode = `${code.trim()}\n`;
  const [result] = await eslint.lintText(normalizedCode, { filePath });
  return result.messages;
}

async function expectRuleError(code: string, ruleId: string) {
  const messages = await lint(code);

  expect(messages).toEqual(expect.arrayContaining([
    expect.objectContaining({ ruleId, severity: 2 }),
  ]));
}

describe('renderer import boundary', () => {
  test.each([
    [
      'store modules',
      `import { useAppStore } from '~/stores/app';`,
    ],
    [
      'composable modules',
      `import { useApi } from '~/composables/useApi';`,
    ],
    ['Pinia', `import { defineStore } from 'pinia';`],
    ['Nuxt runtime', `import { useNuxtApp } from '#app';`],
    [
      'editor components',
      `import Toolbar from '~/components/editor/Toolbar.vue';`,
    ],
  ])('rejects %s', async (_dependency, code) => {
    await expectRuleError(code, 'no-restricted-imports');
  });
});

describe('renderer determinism boundary', () => {
  test.each([
    ['Date.now', 'Date.now();'],
    ['new Date', 'new Date();'],
    ['performance.now', 'performance.now();'],
    ['Math.random', 'Math.random();'],
    ['crypto.getRandomValues', 'crypto.getRandomValues(new Uint8Array(1));'],
    ['crypto.randomUUID', 'crypto.randomUUID();'],
    ['Intl constructor', `new Intl.DateTimeFormat('en-US');`],
    [
      'Intl constructor static method',
      `Intl.DateTimeFormat.supportedLocalesOf('en-US');`,
    ],
    ['Intl namespace static method', `Intl.supportedValuesOf('calendar');`],
    ['Intl namespace reference', 'const localeNamespace = Intl;'],
    ['toLocaleString', '(1).toLocaleString();'],
    ['toLocaleDateString', 'new Date(0).toLocaleDateString();'],
    ['toLocaleTimeString', 'new Date(0).toLocaleTimeString();'],
  ])('rejects %s', async (_dependency, code) => {
    await expectRuleError(code, 'aboutme/no-renderer-nondeterminism');
  });
});

describe('renderer network boundary', () => {
  test.each([
    ['fetch', `fetch('/api/resumes');`],
    ['$fetch', `$fetch('/api/resumes');`],
    ['XMLHttpRequest', 'new XMLHttpRequest();'],
    ['WebSocket', `new WebSocket('wss://example.test');`],
    ['EventSource', `new EventSource('/events');`],
  ])('rejects %s', async (_dependency, code) => {
    await expectRuleError(code, 'no-restricted-globals');
  });
});

test.each([
  ['ts', 'export const now = Date.now();'],
  ['mts', 'export const now = Date.now();'],
  [
    'vue',
    [
      '<script setup lang="ts">',
      'Date.now();',
      '</script>',
      '',
      '<template>',
      '  <div />',
      '</template>',
    ].join('\n'),
  ],
])('renderer purity rules cover .%s files', async (extension, code) => {
  const filePath = `app/components/resume/__lint__/fixture.${extension}`;
  const messages = await lint(code, filePath);

  expect(messages).toEqual(expect.arrayContaining([
    expect.objectContaining({
      ruleId: 'aboutme/no-renderer-nondeterminism',
      severity: 2,
    }),
  ]));
});

test('allows renderer dependencies with deterministic inputs', async () => {
  const messages = await lint(`
import type { ResumeDocument } from '@aboutme/schema';
import { h } from 'vue';
import DateRange from './primitives/DateRange.vue';

export function render(document: ResumeDocument) {
  return h(DateRange, { document });
}
`);

  expect(messages).toEqual([]);
});

test('does not apply renderer rules outside the renderer tree', async () => {
  const messages = await lint(`
import { useAppStore } from '~/stores/app';
import { useApi } from '~/composables/useApi';
import { defineStore } from 'pinia';
import { useNuxtApp } from '#app';
import Toolbar from '~/components/editor/Toolbar.vue';

Date.now();
new Date();
performance.now();
Math.random();
crypto.getRandomValues(new Uint8Array(1));
crypto.randomUUID();
new Intl.DateTimeFormat('en-US');
Intl.supportedValuesOf('calendar');
(1).toLocaleString();
fetch('/api/resumes');
$fetch('/api/resumes');
new XMLHttpRequest();
new WebSocket('wss://example.test');
new EventSource('/events');
`, 'app/components/editor/__lint__/fixture.ts');

  expect(messages.filter(({ ruleId }) => (
    ruleId !== null && taskRuleIds.has(ruleId)
  ))).toEqual([]);
});
