import { expect, type Locator } from '@playwright/test';

export async function visiblePaint(target: Locator): Promise<boolean> {
  return target.evaluate((node) => {
    const box = node.getBoundingClientRect();
    if (box.width <= 0 || box.height <= 0) {
      return false;
    }
    for (let element: Element | null = node; element;
      element = element.parentElement) {
      const css = getComputedStyle(element);
      const clip = element.getBoundingClientRect();
      if (element !== node
        && ((/^(hidden|clip|auto|scroll)$/u.test(css.overflowX)
          && (box.right <= clip.left || box.left >= clip.right))
        || (/^(hidden|clip|auto|scroll)$/u.test(css.overflowY)
          && (box.bottom <= clip.top || box.top >= clip.bottom)))) {
        return false;
      }
      if (css.display === 'none' || css.visibility !== 'visible'
        || Number(css.opacity) <= 0) {
        return false;
      }
    }
    return true;
  });
}

/** Include every clipping ancestor and the widget's own frame. */
export async function containedPaint(
  target: Locator, frame: string, empty = false,
): Promise<boolean> {
  return target.evaluate((node, options) => {
    const box = node.getBoundingClientRect();
    const owner = node.parentElement?.closest(options.frame);
    if (!owner || box.height <= 0
      || (options.empty ? box.width < 0 : box.width <= 0)) {
      return false;
    }
    const inside = (rect: DOMRect, x: boolean, y: boolean) =>
      (!x || (box.left >= rect.left - 1 && box.right <= rect.right + 1))
      && (!y || (box.top >= rect.top - 1 && box.bottom <= rect.bottom + 1));
    if (!inside(owner.getBoundingClientRect(), true, true)) {
      return false;
    }
    for (let element: Element | null = node; element;
      element = element.parentElement) {
      const css = getComputedStyle(element);
      if (css.display === 'none' || css.visibility !== 'visible'
        || Number(css.opacity) <= 0) {
        return false;
      }
      if (element !== node && element !== document.documentElement
        && element !== document.body
        && !inside(element.getBoundingClientRect(),
          /^(hidden|clip|auto|scroll)$/u.test(css.overflowX),
          /^(hidden|clip|auto|scroll)$/u.test(css.overflowY))) {
        return false;
      }
    }
    return true;
  }, { frame, empty });
}

interface SvgPrimitive {
  readonly selector: string;
  readonly attributes: Readonly<Record<string, string>>;
  readonly stroke: string;
  readonly fill: string;
  readonly width: string;
}

export interface SvgExpectation {
  readonly viewBox: string;
  readonly primitives: readonly SvgPrimitive[];
}

/** Check explicit primitives rather than trusting an icon's class name. */
export async function expectSvg(
  icons: Locator, expected: SvgExpectation, message: string,
): Promise<void> {
  expect(await icons.count(), message).toBeGreaterThan(0);
  for (const icon of await icons.all()) {
    await expect(icon, message).toHaveAttribute('viewBox', expected.viewBox);
    await expect.poll(() => visiblePaint(icon), message).toBe(true);
    await expect(icon.locator('path,circle,rect,line,polyline,polygon,ellipse'),
      message).toHaveCount(expected.primitives.length);
    for (const primitive of expected.primitives) {
      const node = icon.locator(primitive.selector);
      await expect(node, message).toHaveCount(1);
      for (const [attribute, value] of Object.entries(primitive.attributes)) {
        await expect(node, message).toHaveAttribute(attribute, value);
      }
      await expect(node, message).toHaveCSS('stroke', primitive.stroke);
      await expect(node, message).toHaveCSS('fill', primitive.fill);
      await expect(node, message).toHaveCSS('stroke-width', primitive.width);
      await expect.poll(() => node.evaluate((element) => {
        const graphic = element as SVGGraphicsElement;
        const box = graphic.getBBox();
        const svg = graphic.ownerSVGElement!;
        const view = svg.viewBox.baseVal;
        const rect = graphic.getBoundingClientRect();
        const frame = svg.getBoundingClientRect();
        for (let parent: Element | null = graphic; parent;
          parent = parent.parentElement) {
          const css = getComputedStyle(parent);
          if (css.display === 'none' || css.visibility !== 'visible'
            || Number(css.opacity) <= 0) {
            return false;
          }
        }
        const css = getComputedStyle(graphic);
        if ((css.stroke !== 'none' && Number(css.strokeOpacity) <= 0)
          || (css.fill !== 'none' && Number(css.fillOpacity) <= 0)) {
          return false;
        }
        return rect.left >= frame.left - 1 && rect.right <= frame.right + 1
          && rect.top >= frame.top - 1 && rect.bottom <= frame.bottom + 1
          && Number.isFinite(box.x + box.y + box.width + box.height)
          && (box.width > 0 || box.height > 0)
          && box.x >= view.x - 1 && box.y >= view.y - 1
          && box.x + box.width <= view.x + view.width + 1
          && box.y + box.height <= view.y + view.height + 1;
      }), message).toBe(true);
    }
  }
}

export function lucideExpectation(
  paths: readonly string[], stroke: string,
): SvgExpectation {
  return {
    viewBox: '0 0 24 24',
    primitives: paths.map((d, index) => ({
      selector: `path:nth-of-type(${index + 1})`, attributes: { d },
      stroke, fill: 'none', width: '2px',
    })),
  };
}
