import { expect, type Locator, type Page } from '@playwright/test';

/** Keep probes from changing a sheet or a retained screenshot's position. */
export async function preserveScreenScroll(
  page: Page, assertion: () => Promise<void>,
): Promise<void> {
  const positions = await page.evaluateHandle(() =>
    Array.from(document.querySelectorAll('*')).filter((node) =>
      node.scrollHeight > node.clientHeight
      || node.scrollWidth > node.clientWidth).map((node) =>
      ({ node, top: node.scrollTop, left: node.scrollLeft })));
  const position = await page.evaluate(() => ({ x: scrollX, y: scrollY }));
  try {
    await assertion();
  } finally {
    await positions.evaluate((records) => {
      for (const { node, top, left } of records) {
        node.scrollTo(left, top);
      }
    });
    await positions.dispose();
    await page.evaluate(({ x, y }) => window.scrollTo(x, y), position);
  }
}

/** Inspect clips before scrolling can reveal a hidden descendant. */
export async function rejectAncestorClipping(
  text: Locator, scrolling: readonly string[],
): Promise<void> {
  await expect.poll(() => text.evaluateAll((nodes, exceptions) => {
    return nodes.every((node) => {
      const box = node.getBoundingClientRect();
      for (let ancestor: Element | null = node.parentElement; ancestor
        && ancestor !== document.body && ancestor !== document.documentElement;
        ancestor = ancestor.parentElement) {
        const css = getComputedStyle(ancestor);
        const allowed = exceptions.some((selector) =>
          ancestor!.matches(selector));
        // Only actual scrolling, never hidden/clip, qualifies for an exception.
        const x = allowed && /^(auto|scroll)$/u.test(css.overflowX);
        const y = allowed && /^(auto|scroll)$/u.test(css.overflowY);
        const clip = ancestor.getBoundingClientRect();
        // Decorative siblings can overflow on purpose; inspect this text box.
        if ((!x && /^(auto|scroll|hidden|clip)$/u.test(css.overflowX)
          && (box.left < clip.left - 1 || box.right > clip.right + 1))
        || (!y && /^(auto|scroll|hidden|clip)$/u.test(css.overflowY)
          && (box.top < clip.top - 1 || box.bottom > clip.bottom + 1))) {
          return false;
        }
      }
      return true;
    });
  }, [...scrolling]), 'Screen ancestor clipping.').toBe(true);
}

/** Measure text against a named container, including clipped text. */
export async function measureTextContainment(
  container: Locator,
  verticalContainer?: string,
): Promise<{
  visible: boolean;
  contained: boolean;
}> {
  return container.evaluate((root, selector) => {
    const bounds = root.getBoundingClientRect();
    const outer = selector ? root.closest(selector) : root;
    if (!outer) {
      return { visible: false, contained: false };
    }
    const rootStyle = getComputedStyle(root);
    const rootClipsY = /^(hidden|clip|scroll|auto)$/u.test(rootStyle.overflowY);
    const next = outer !== root
      ? root.nextElementSibling?.getBoundingClientRect()
      : undefined;
    let visible = bounds.width > 0 && bounds.height > 0;
    let contained = root.scrollWidth <= root.clientWidth + 1;
    let hasText = false;
    const context = document.createElement('canvas').getContext('2d')!;
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const node = walker.currentNode;
      if (!node.textContent?.trim()) {
        continue;
      }
      hasText = true;
      const parent = node.parentElement;
      if (!parent) {
        continue;
      }
      const clips: {
        rect: DOMRect;
        x: boolean;
        y: boolean;
        ink: boolean;
      }[] = [
        { rect: bounds, x: true, y: outer === root || rootClipsY,
          ink: rootClipsY || rootStyle.display === 'inline' },
      ];
      if (outer !== root) {
        clips.push({ rect: outer.getBoundingClientRect(), x: false, y: true,
          ink: false });
      }
      for (
        let element: Element | null = parent;
        element;
        element = element.parentElement
      ) {
        const style = getComputedStyle(element);
        if (style.display === 'none' || style.visibility !== 'visible'
          || Number(style.opacity) === 0) {
          visible = false;
        }
        if (element !== root) {
          clips.push({
            rect: element.getBoundingClientRect(),
            x: /^(hidden|clip|scroll|auto)$/u.test(style.overflowX),
            y: /^(hidden|clip|scroll|auto)$/u.test(style.overflowY),
            ink: true,
          });
        }
      }
      const range = document.createRange();
      range.selectNodeContents(node);
      const rects = Array.from(range.getClientRects()).filter((rect) =>
        rect.width > 0 && rect.height > 0);
      if (rects.length === 0) {
        visible = false;
      }
      const css = getComputedStyle(parent);
      context.font = `${css.fontStyle} ${css.fontWeight} ${css.fontSize} `
        + css.fontFamily;
      const content = css.textTransform === 'uppercase'
        ? node.textContent!.toUpperCase()
        : css.textTransform === 'lowercase'
          ? node.textContent!.toLowerCase()
          : node.textContent!;
      const metrics = context.measureText(content);
      if (!Number.isFinite(metrics.fontBoundingBoxAscent
        + metrics.fontBoundingBoxDescent + metrics.actualBoundingBoxAscent
        + metrics.actualBoundingBoxDescent)
      || metrics.fontBoundingBoxAscent + metrics.fontBoundingBoxDescent <= 0) {
        return { visible: false, contained: false };
      }
      for (const rect of rects) {
        // Range boxes include the font's unused ascent and descent. Compare
        // line boxes for flow and painted ink for clips, without extra slack.
        const scale = rect.height
          / (metrics.fontBoundingBoxAscent + metrics.fontBoundingBoxDescent);
        const baseline = rect.top + metrics.fontBoundingBoxAscent * scale;
        const ink = {
          top: baseline - metrics.actualBoundingBoxAscent * scale,
          bottom: baseline + metrics.actualBoundingBoxDescent * scale,
        };
        const leading = Number.isFinite(parseFloat(css.lineHeight))
          ? (rect.height - parseFloat(css.lineHeight) * scale) / 2
          : 0;
        const line = { top: rect.top + leading, bottom: rect.bottom - leading };
        if (next && rect.right > next.left + 1 && rect.left < next.right - 1
          && ink.bottom > next.top + 1 && ink.top < next.bottom - 1) {
          contained = false;
        }
        for (const clip of clips) {
          const vertical = clip.ink ? ink : line;
          if ((clip.x && (rect.left < clip.rect.left - 1
            || rect.right > clip.rect.right + 1))
          || (clip.y && (vertical.top < clip.rect.top - 1
            || vertical.bottom > clip.rect.bottom + 1))) {
            contained = false;
          }
        }
      }
    }
    return { visible: visible && hasText, contained };
  }, verticalContainer);
}
