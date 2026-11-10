import { expect, type Page } from '@playwright/test';

export interface StyleRule {
  readonly selector: string;
  readonly message: string;
  readonly css: Readonly<Record<string, string>>;
}

export async function expectStyleRule(
  page: Page, rule: StyleRule,
): Promise<void> {
  const nodes = page.locator(rule.selector);
  expect(await nodes.count(), rule.message).toBeGreaterThan(0);
  for (const node of await nodes.all()) {
    for (const [property, value] of Object.entries(rule.css)) {
      const message = `${rule.message} ${property}.`;
      if (['font-size', 'line-height', 'letter-spacing'].includes(property)
        && value.endsWith('px')) {
        await expect.poll(() => node.evaluate((element, key) =>
          parseFloat(getComputedStyle(element).getPropertyValue(key)),
        property), message).toBeCloseTo(parseFloat(value), 1);
      } else {
        await expect(node, message).toHaveCSS(property, value);
      }
    }
  }
}

export async function expectStyleRules(
  page: Page, rules: readonly StyleRule[],
): Promise<void> {
  for (const rule of rules) {
    await expectStyleRule(page, rule);
  }
}
