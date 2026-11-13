// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { describe, expect, it } from 'vitest';

function between(source: string, pattern: RegExp, label: string): string {
  const match = pattern.exec(source);
  if (match?.[1] === undefined) throw new Error(`${label} not found`);
  return match[1];
}

const goExpression = between(
  readFileSync(
    resolve(process.cwd(), '../server/internal/printrender/browser.go'),
    'utf8',
  ),
  /^const pageReadinessExpression = `([^`]*)`$/m,
  'Go pageReadinessExpression',
);

const specExpression = between(
  readFileSync(resolve(process.cwd(), 'e2e/card.spec.ts'), 'utf8'),
  /^const PAGE_READINESS_EXPRESSION = `([^`]*)`;$/m,
  'card.spec.ts PAGE_READINESS_EXPRESSION',
);

describe('card readiness expression', () => {
  it('is a plain template literal in the card spec', () => {
    expect(specExpression).not.toMatch(/\\|\$\{/);
  });

  it('matches the Go print renderer expression byte for byte', () => {
    expect(specExpression).toBe(goExpression);
  });
});
