import { expect, type Locator, type Page, test } from '@playwright/test';

import {
  expectScreen, expectScreenClass, SCREEN_CLASSES, type ScreenContract,
} from './display-assertions';

/** Register a mutation proof; WEB_E2E_MUTATIONS=0 leaves these out. */
export const mutationTest = (
  title: string, body: Parameters<typeof test>[2],
): void => test(title, { tag: '@mutation' }, body);

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
        };
      }, { property, pseudo }),
      `Style mutation computed ${property}.`).toEqual({
        values: Array.from({ length: count }, () => matcher),
      });
    }
    // Companion declarations and already-correct siblings may already match.
    await expect.poll(() => saved.evaluate((records, options) =>
      records.some(({ element, before }) => element.isConnected
        && options.properties.some((property) =>
          getComputedStyle(element, options.pseudo).getPropertyValue(property)
          !== before[property])),
    { properties: Object.keys(styles), pseudo }),
    'Style mutation changed computed values.').toBe(true);
  } catch (error) {
    await restore();
    throw error;
  }
  return restore;

  async function restore(): Promise<void> {
    await saved.evaluate((records) => {
      for (const record of records) {
        const { element, original, attribute, previous, sheet } = record;
        if (original === null) {
          element.removeAttribute('style');
        } else {
          element.setAttribute('style', original);
        }
        sheet?.remove();
        if (sheet !== undefined) {
          if (previous === null) {
            element.removeAttribute(attribute);
          } else {
            element.setAttribute(attribute, previous);
          }
        }
      }
    });
    await saved.dispose();
  }
}

/** Register a real surface's proof for each shared assertion class. */
export function screenMutationTests(
  name: string,
  open: (page: Page) => Promise<void>,
  contract: ScreenContract,
): void {
  for (const kind of SCREEN_CLASSES) {
    if (kind === 'images' && !contract.image) {
      continue;
    }
    mutationTest(`${name} rejects ${kind} mutation`, async ({ page }) => {
      test.setTimeout(60_000);
      await open(page);
      await expectScreen(page, contract);
      const assertion = () => expectScreenClass(page, contract, kind);
      let restore: () => Promise<void>;
      if (kind === 'content' || kind === 'accessibility' || kind === 'images') {
        const selector = kind === 'content'
          ? contract.copy[0]
          : kind === 'accessibility'
            ? contract.aria[0]
            : contract.image!;
        const target = page.locator(selector).first();
        const attribute = kind === 'images' ? 'src' : 'aria-hidden';
        const original = kind === 'content'
          ? await target.innerHTML()
          : await target.getAttribute(attribute);
        if (kind === 'content') {
          await target.evaluate((node) => {
            node.textContent = 'MUTATED';
          });
          await expect(target).toHaveText('MUTATED');
        } else {
          const value = kind === 'images'
            ? '/missing-display-fixture.png'
            : 'true';
          await target.evaluate((node, input) => {
            node.setAttribute(input.attribute, input.value);
          }, { attribute, value });
          await expect(target).toHaveAttribute(attribute, value);
        }
        restore = async () => {
          await target.evaluate((node, input) => {
            if (input.content) {
              node.innerHTML = input.original!;
            } else if (input.original === null) {
              node.removeAttribute(input.attribute);
            } else {
              node.setAttribute(input.attribute, input.original);
            }
          }, { content: kind === 'content', attribute, original });
          if (kind === 'images') {
            // A failed image also sets component state; remount its clean page.
            await open(page);
          }
        };
      } else if (kind === 'overlap') {
        const nodes = page.locator(contract.flow);
        expect(await nodes.count(), 'Overlap mutation targets.')
          .toBeGreaterThanOrEqual(2);
        const offset = await nodes.evaluateAll((elements) => {
          const first = elements[0]!.getBoundingClientRect();
          const second = elements[1]!.getBoundingClientRect();
          return { x: first.left - second.left, y: first.top - second.top };
        });
        restore = await applyStyleMutation(nodes.nth(1), {
          transform: [`translate(${offset.x}px, ${offset.y}px)`, /matrix/u],
        });
      } else {
        const selector = kind === 'geometry'
          ? contract.root
          : kind === 'containment'
            ? contract.text
            : kind === 'typography'
              ? contract.type[0]
              : contract.paint[0];
        const styles: StyleMutation = kind === 'geometry'
          ? { transform: ['translateX(2000px)', /matrix/u] }
          : kind === 'containment'
            ? { 'display': 'block', 'min-height': '0px', 'padding': '0px',
                'box-sizing': 'content-box', 'height': '1px',
                'overflow': 'hidden' }
            : kind === 'typography'
              ? { 'font-family': 'monospace' }
              : { [contract.paint[1]]: 'rgb(255, 0, 0)' };
        restore = await applyStyleMutation(
          page.locator(selector).first(), styles,
        );
      }
      try {
        await expect(assertion()).rejects.toThrow(`Screen ${kind}.`);
      } finally {
        await restore();
      }
      await assertion();
    });
  }
}

/** Prove one added assertion without running unrelated surface checks first. */
export async function proveMutation(
  assertion: () => Promise<void>,
  mutate: () => Promise<() => Promise<void>>,
  message: string,
): Promise<void> {
  await assertion();
  const restore = await mutate();
  try {
    await expect(assertion()).rejects.toThrow(message);
  } finally {
    await restore();
  }
  await assertion();
}

export async function mutateAttribute(
  target: Locator, attribute: string, value: string,
): Promise<() => Promise<void>> {
  const original = await target.getAttribute(attribute);
  expect(original, 'Attribute mutation changes the fixture.').not.toBe(value);
  await target.evaluate((node, input) => {
    node.setAttribute(input.attribute, input.value);
  }, { attribute, value });
  await expect(target).toHaveAttribute(attribute, value);
  if (attribute === 'role' && value === 'group') {
    await expect(target, 'Role mutation changes accessible semantics.')
      .toHaveRole('group');
  }
  return async () => {
    await target.evaluate((node, input) => {
      if (input.original === null) {
        node.removeAttribute(input.attribute);
      } else {
        node.setAttribute(input.attribute, input.original);
      }
    }, { attribute, original });
  };
}

export async function mutateText(
  target: Locator, value: string,
): Promise<() => Promise<void>> {
  const original = await target.innerHTML();
  await target.evaluate((node, text) => {
    node.textContent = text;
  }, value);
  await expect(target).toHaveText(value);
  return async () => {
    await target.evaluate((node, html) => {
      node.innerHTML = html;
    }, original);
  };
}
