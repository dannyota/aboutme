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

// --- showcase-baselines.spec.ts ---

/** Check grid tracks, gaps, and sibling separation in one layout frame. */
export async function measureShowcaseGrid(
  grid: Locator,
  columns: number,
  gap: number,
): Promise<boolean> {
  return grid.evaluate((element, expected) => {
    const style = getComputedStyle(element);
    const tracks = style.gridTemplateColumns.split(' ');
    const bounds = element.getBoundingClientRect();
    const boxes = Array.from(element.children, (child) =>
      child.getBoundingClientRect());
    const width = (bounds.width - expected.gap * (expected.columns - 1))
      / expected.columns;
    return tracks.length === expected.columns
      && Math.abs(parseFloat(style.columnGap) - expected.gap) <= 1
      && Math.abs(parseFloat(style.rowGap) - expected.gap) <= 1
      && boxes.every((box, index) => {
        const column = index % expected.columns;
        const previous = boxes[index - 1];
        const above = boxes[index - expected.columns];
        return box.width > 0 && box.height > 0
          && Math.abs(box.width - width) <= 1
          && Math.abs(box.left - bounds.left
            - column * (width + expected.gap)) <= 1
          && (!previous || column === 0
            || box.left >= previous.right + expected.gap - 1)
          && (!above || box.top >= above.bottom + expected.gap - 1);
      });
  }, { columns, gap });
}

/** Check that a fixed sheet and its controls fit the viewport. */
export async function measureShowcaseSheet(sheet: Locator): Promise<boolean> {
  return sheet.evaluate((element) => {
    const box = element.getBoundingClientRect();
    const controls = element.querySelectorAll<HTMLElement>(
      '[data-action="showcase-filters-close"], '
      + '[data-action="showcase-filters-clear"], '
      + '[data-action="showcase-filters-apply"]',
    );
    return box.width > 0 && box.height > 0
      && box.left >= -1 && box.right <= innerWidth + 1
      && box.top >= -1 && Math.abs(box.bottom - innerHeight) <= 1
      && box.height <= innerHeight * 0.85 + 1
      && controls.length === 3
      && Array.from(controls).every((control) => {
        const rect = control.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0
          && rect.left >= box.left - 1 && rect.right <= box.right + 1
          && rect.top >= box.top - 1 && rect.bottom <= box.bottom + 1;
      });
  });
}
