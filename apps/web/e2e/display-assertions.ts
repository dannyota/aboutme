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

// --- gallery.spec.ts and gallery-baselines.spec.ts ---
export async function measureGalleryGrid(grid: Locator): Promise<{
  columns: number;
  columnGap: number;
  rowGap: number;
  ordered: boolean;
}> {
  return grid.evaluate((element) => {
    const style = getComputedStyle(element);
    const boxes = Array.from(element.children)
      .filter((child) => !child.hasAttribute('hidden'))
      .map((child) => child.getBoundingClientRect());
    return {
      columns: style.gridTemplateColumns.split(' ').length,
      columnGap: Number.parseFloat(style.columnGap),
      rowGap: Number.parseFloat(style.rowGap),
      ordered: boxes.every((box, index) => {
        if (box.width <= 0 || box.height <= 0) return false;
        return boxes.slice(0, index).every((previous) => {
          const sameRow = Math.abs(previous.top - box.top) <= 1;
          return sameRow
            ? box.left >= previous.right - 1
            : box.top >= previous.bottom - 1;
        });
      }),
    };
  });
}

export async function measureGalleryDetail(layout: Locator): Promise<{
  stacked: boolean;
  columns: boolean;
  contained: boolean;
}> {
  return layout.evaluate((element) => {
    const info = element.querySelector('.template-detail__info')!
      .getBoundingClientRect();
    const sheet = element.querySelector('.template-detail__sheet')!
      .getBoundingClientRect();
    const root = element.getBoundingClientRect();
    return {
      stacked: sheet.top >= info.bottom - 1,
      columns: info.left >= sheet.right - 1
        && Math.abs(info.top - sheet.top) <= 1,
      contained: [info, sheet].every((box) => box.width > 0 && box.height > 0
        && box.left >= root.left - 1 && box.right <= root.right + 1),
    };
  });
}

export async function galleryFontLoaded(root: Locator): Promise<boolean> {
  return root.evaluate(() => Array.from(document.fonts).some((face) =>
    face.family.replaceAll('"', '') === 'Be Vietnam Pro'
    && face.status === 'loaded'));
}
