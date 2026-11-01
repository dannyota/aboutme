import type { Locator } from '@playwright/test';

/** Measure text against a named container, including clipped text. */
export async function measureTextContainment(
  container: Locator,
): Promise<{ visible: boolean; contained: boolean }> {
  return container.evaluate((root) => {
    const bounds = root.getBoundingClientRect();
    let visible = bounds.width > 0 && bounds.height > 0;
    let contained = root.scrollWidth <= root.clientWidth + 1
      && root.scrollHeight <= root.clientHeight + 1;
    let hasText = false;
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const node = walker.currentNode;
      if (!node.textContent?.trim()) continue;
      hasText = true;
      const parent = node.parentElement;
      if (!parent) continue;
      const clips: { rect: DOMRect; x: boolean; y: boolean }[] = [
        { rect: bounds, x: true, y: true },
      ];
      for (
        let element: Element | null = parent;
        element;
        element = element.parentElement
      ) {
        const style = getComputedStyle(element);
        if (style.display === 'none' || style.visibility !== 'visible'
          || Number(style.opacity) === 0) visible = false;
        if (element !== root) {
          clips.push({
            rect: element.getBoundingClientRect(),
            x: /^(hidden|clip|scroll|auto)$/u.test(style.overflowX),
            y: /^(hidden|clip|scroll|auto)$/u.test(style.overflowY),
          });
        }
        if (element === root) break;
      }
      const range = document.createRange();
      range.selectNodeContents(node);
      const rects = Array.from(range.getClientRects());
      if (!rects.some((rect) => rect.width > 0 && rect.height > 0)) {
        visible = false;
      }
      for (const rect of rects) {
        for (const clip of clips) {
          if ((clip.x && (rect.left < clip.rect.left - 1
            || rect.right > clip.rect.right + 1))
          || (clip.y && (rect.top < clip.rect.top - 1
            || rect.bottom > clip.rect.bottom + 1))) {
            contained = false;
          }
        }
      }
    }
    return { visible: visible && hasText, contained };
  });
}

/** Measure all adjacent boxes at once to avoid mixing layout frames. */
export async function measureVerticalFlow(items: Locator): Promise<boolean> {
  return items.evaluateAll((elements) => {
    const boxes = elements.map((element) => element.getBoundingClientRect());
    return boxes.every((box, index) => box.width > 0 && box.height > 0
      && (index === 0 || box.top >= boxes[index - 1]!.bottom - 1));
  });
}

// --- print.spec.ts ---
export async function measurePrintRules(
  page: import('@playwright/test').Page,
): Promise<Record<string, string>> {
  return page.locator('.resume-document').evaluate((root) => {
    const style = (selector: string): CSSStyleDeclaration => {
      const element = root.querySelector(selector);
      if (element === null) throw new Error('Missing print rule fixture.');
      return getComputedStyle(element);
    };
    const paragraph = style('.entry-body p');
    return {
      bodyHeaderAfter: style('.entry-header:not(:last-child)').breakAfter,
      entryInside: style('.entry').breakInside,
      headerInside: style('.entry-header').breakInside,
      orphans: paragraph.orphans,
      printColorAdjust: getComputedStyle(root).printColorAdjust,
      sectionAfter: style('.section-heading').breakAfter,
      sectionInside: style('.resume-section').breakInside,
      widows: paragraph.widows,
    };
  });
}
