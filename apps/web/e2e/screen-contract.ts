import { expect, type Page } from '@playwright/test';

import { expectStyleRules, type StyleRule } from './screen-expectations';

import { visiblePaint } from './screen-paint';
import {
  measureTextContainment, preserveScreenScroll, rejectAncestorClipping,
} from './screen-containment';

export interface ScreenContract {
  readonly root: string;
  readonly styles?: readonly StyleRule[];
  readonly structure?: (page: Page) => Promise<void>;
  readonly text: string;
  readonly flow: string;
  readonly type: readonly [selector: string, family: string, face: string];
  readonly paint: readonly [selector: string, property: string, value: string];
  readonly copy: readonly [selector: string, text: string];
  readonly aria: readonly [selector: string, snapshot: string];
  readonly image?: string;
  readonly scrolling?: readonly string[];
  readonly controls?: readonly (readonly [string, string])[];
  readonly faces?: readonly string[];
  readonly constrained?: string;
  readonly counts?: readonly (readonly [selector: string, count: number])[];
}

export const SCREEN_CLASSES = [
  'geometry', 'containment', 'overlap', 'typography', 'color', 'content',
  'accessibility', 'images',
] as const;
export type ScreenClass = typeof SCREEN_CLASSES[number];

/** Use the same named checks for captured cells and their seeded defects. */
export async function expectScreenClass(
  page: Page,
  contract: ScreenContract,
  kind: ScreenClass,
): Promise<void> {
  await preserveScreenScroll(page, () =>
    checkScreenClass(page, contract, kind));
}

async function checkScreenClass(
  page: Page,
  contract: ScreenContract,
  kind: ScreenClass,
): Promise<void> {
  const message = `Screen ${kind}.`;
  if (kind === 'geometry') {
    await expect.poll(() => page.locator(contract.root).evaluate((element) => {
      const box = element.getBoundingClientRect();
      return box.width > 0 && box.height > 0 && box.left >= -1
        && box.right <= innerWidth + 1;
    }), message).toBe(true);
  } else if (kind === 'containment') {
    await expect.poll(() => page.evaluate(() =>
      document.documentElement.scrollWidth
      <= document.documentElement.clientWidth + 1), message).toBe(true);
    const boxes = page.locator(contract.constrained ?? contract.root);
    for (const box of await boxes.all()) {
      await expect.poll(() => box.evaluate((element, vertical) =>
        element.scrollWidth <= element.clientWidth + 1
        && (!vertical || element.scrollHeight <= element.clientHeight + 1),
      contract.constrained !== undefined), message).toBe(true);
    }
    const text = page.locator(contract.text);
    await rejectAncestorClipping(text, contract.scrolling ?? []);
    expect(await text.count(), message).toBeGreaterThan(0);
    for (const [selector, name] of contract.controls ?? []) {
      const control = page.locator(selector);
      await expect(control, 'Screen control name.')
        .toHaveAccessibleName(name);
      await expect.poll(() => visiblePaint(control),
        'Screen control visibility.').toBe(true);
      await expect.poll(() => control.evaluate((node, selector) => {
        const box = node.getBoundingClientRect();
        const root = document.querySelector(selector)!
          .getBoundingClientRect();
        return box.left >= root.left - 1 && box.right <= root.right + 1
          && box.top >= root.top - 1 && box.bottom <= root.bottom + 1;
      }, contract.root), 'Screen control box.').toBe(true);
    }
    for (const [index, node] of (await text.all()).entries()) {
      if (await node.isVisible()) {
        await node.scrollIntoViewIfNeeded();
      }
      await expect.poll(() => measureTextContainment(node),
        `${message} Text box ${index}.`)
        .toEqual({ visible: true, contained: true });
    }
  } else if (kind === 'overlap') {
    await expect.poll(() => page.locator(contract.flow).evaluateAll((nodes) => {
      const boxes = nodes.map((node) => node.getBoundingClientRect());
      return boxes.length > 0 && boxes.every((box, index) =>
        box.width > 0 && box.height > 0
        && boxes.slice(index + 1).every((other) =>
          Math.min(box.right, other.right) - Math.max(box.left, other.left) <= 1
          || Math.min(box.bottom, other.bottom) - Math.max(box.top, other.top)
          <= 1));
    }), message).toBe(true);
  } else if (kind === 'typography') {
    const families = (value: string) => value.split(',').map((family) =>
      family.trim().replaceAll('"', ''));
    await expect.poll(() => page.locator(contract.type[0]).evaluate((node) =>
      getComputedStyle(node).fontFamily.split(',').map((family) =>
        family.trim().replaceAll('"', ''))), message)
      .toEqual(families(contract.type[1]));
    await expect.poll(() => page.locator(contract.type[0]).evaluate(
      (element, face) => {
        const style = getComputedStyle(element);
        return document.fonts.check(`${style.fontWeight} ${style.fontSize} `
          + `"${face}"`, element.textContent ?? '')
        && Array.from(document.fonts).some((font) =>
          font.family.replaceAll('"', '') === face
          && font.status === 'loaded')
        && !Array.from(document.fonts).some((font) =>
          font.status === 'error');
      }, contract.type[2]), message).toBe(true);
    for (const face of contract.faces ?? []) {
      await expect.poll(() => page.evaluate((family) =>
        Array.from(document.fonts).some((font) =>
          font.family.replaceAll('"', '') === family
          && font.status === 'loaded')
        && [400, 700].every((weight) =>
          document.fonts.check(`${weight} 14px "${family}"`)), face),
      message).toBe(true);
    }
    await expectStyleRules(page, contract.styles ?? []);
  } else if (kind === 'color') {
    await expect(page.locator(contract.paint[0]), message)
      .toHaveCSS(contract.paint[1], contract.paint[2]);
  } else if (kind === 'content') {
    await expect(page.locator(contract.copy[0]), message)
      .toHaveText(contract.copy[1]);
    for (const [selector, count] of contract.counts ?? []) {
      await expect(page.locator(selector), message).toHaveCount(count);
    }
  } else if (kind === 'accessibility') {
    await expect(page.locator(contract.aria[0]), message)
      .toMatchAriaSnapshot(contract.aria[1]);
    await contract.structure?.(page);
  } else if (contract.image) {
    const images = page.locator(contract.image);
    expect(await images.count(), message).toBeGreaterThan(0);
    for (const image of await images.all()) {
      await expect.poll(() => visiblePaint(image), message).toBe(true);
      await image.scrollIntoViewIfNeeded();
      await expect.poll(() => image.evaluate(async (element) => {
        const img = element as HTMLImageElement;
        try {
          await img.decode();
        } catch {
          return false;
        }
        return img.complete && img.naturalWidth > 0 && img.naturalHeight > 0;
      }), message).toBe(true);
      await expect.poll(() => visiblePaint(image), message).toBe(true);
    }
  }
}

export async function expectScreen(
  page: Page,
  contract: ScreenContract,
): Promise<void> {
  await preserveScreenScroll(page, async () => {
    for (const kind of SCREEN_CLASSES) {
      await checkScreenClass(page, contract, kind);
    }
  });
}
