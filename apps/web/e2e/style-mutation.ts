import { expect, type Locator } from '@playwright/test';

export type StyleMutation = Readonly<Record<string,
  string | readonly [value: string, computed: string | RegExp]>>;

let marker = 0;

/** Applies a temporary style and proves its computed value before returning. */
export async function applyStyleMutation(
  target: Locator,
  styles: StyleMutation,
  pseudo?: '::before' | '::after',
): Promise<() => Promise<void>> {
  const elements = await target.elementHandles();
  const count = elements.length;
  expect(count, 'Style mutation targets.').toBeGreaterThan(0);
  const declarations = Object.entries(styles).map(([property, value]) =>
    [property, typeof value === 'string' ? value : value[0]] as const);
  const saved = await target.page().evaluateHandle((options) => {
    const { elements } = options;
    return elements.map((node, index) => {
      const element = node as HTMLElement;
      const original = element.getAttribute('style');
      const attribute = 'data-e2e-style-mutation';
      const previous = element.getAttribute(attribute);
      let sheet: HTMLStyleElement | undefined;
      let style = element.style;
      if (options.pseudo !== undefined) {
        const id = `${options.marker}-${index}`;
        element.setAttribute(attribute, id);
        sheet = document.createElement('style');
        document.head.append(sheet);
        sheet.sheet!.insertRule(`[${attribute}="${id}"]${options.pseudo} {}`);
        style = (sheet.sheet!.cssRules[0] as CSSStyleRule).style;
      }
      // Reduced motion can leave a nonzero transition duration on every node.
      style.setProperty('transition', 'none', 'important');
      style.setProperty('animation', 'none', 'important');
      const computed = getComputedStyle(element, options.pseudo);
      void computed.transitionProperty;
      const before = Object.fromEntries(options.declarations.map(([key]) =>
        [key, computed.getPropertyValue(key)]));
      for (const [property, value] of options.declarations) {
        style.setProperty(property, value, 'important');
      }
      return { element, original, attribute, previous, sheet, before };
    });
  }, { elements, declarations, pseudo, marker: marker++ });
  await Promise.all(elements.map((element) => element.dispose()));
  try {
    for (const [property, value] of Object.entries(styles)) {
      const expected = typeof value === 'string' ? value : value[1];
      const matcher = typeof expected === 'string'
        ? expected
        : expect.stringMatching(expected);
      await expect.poll(() => saved.evaluate((records, options) => {
        const values = records.map(({ element }) => element.isConnected
          ? getComputedStyle(element, options.pseudo)
              .getPropertyValue(options.property)
          : null);
        return {
          values,
          changed: !options.requireChange || records.some((record, index) =>
            values[index] !== record.before[options.property]),
        };
      }, { property, pseudo, requireChange: typeof expected !== 'string' }),
      `Style mutation computed ${property}.`).toEqual({
        values: Array.from({ length: count }, () => matcher), changed: true,
      });
    }
  } catch (error) {
    await restore();
    throw error;
  }
  return restore;

  async function restore(): Promise<void> {
    await saved.evaluate((records) => {
      for (const record of records) {
        const { element, original, attribute, previous, sheet } = record;
        if (original === null) element.removeAttribute('style');
        else element.setAttribute('style', original);
        sheet?.remove();
        if (sheet !== undefined) {
          if (previous === null) element.removeAttribute(attribute);
          else element.setAttribute(attribute, previous);
        }
      }
    });
    await saved.dispose();
  }
}
