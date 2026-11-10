import { test, type Page } from '@playwright/test';

import {
  expectStyleRule, type StyleRule,
} from './screen-expectations';
import { applyStyleMutation, proveMutation } from './style-mutation';

/** Every literal style expectation has its own isolated mutation step. */
export function styleProofs(
  surface: string, open: (page: Page) => Promise<void>,
  rules: readonly StyleRule[],
): void {
  for (const rule of rules) {
    test(`${surface} rejects changed ${rule.message}`, async ({ page }) => {
      test.setTimeout(90_000);
      await open(page);
      await expectStyleRule(page, rule);
      for (const [property, expected] of Object.entries(rule.css)) {
        await test.step(`rejects ${property}`, async () => {
          const assertion = () => expectStyleRule(page, {
            ...rule, css: { [property]: expected },
          });
          const value = changedStyle(property, expected);
          await proveMutation(assertion, () => applyStyleMutation(
            page.locator(rule.selector).first(), { [property]: value },
          ), `${rule.message} ${property}.`);
        });
      }
    });
  }
}

function changedStyle(property: string, expected: string): string {
  if (property === 'font-weight') {
    return expected === '400' ? '700' : '400';
  }
  if (expected.endsWith('px')) {
    return `${Number((parseFloat(expected) + 1).toFixed(4))}px`;
  }
  if (property === 'font-family') {
    return 'monospace';
  }
  if (property === 'text-align') {
    return expected === 'center' ? 'left' : 'center';
  }
  if (property === 'line-height') {
    return '1px';
  }
  if (property === 'letter-spacing') {
    return '1px';
  }
  return expected === 'none' ? 'uppercase' : 'none';
}
