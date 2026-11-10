import { expect, type Locator } from '@playwright/test';

/** Inspect clips before scrolling can reveal a hidden descendant. */
export async function rejectAncestorClipping(
  text: Locator, scrolling: readonly string[],
): Promise<void> {
  await expect.poll(() => text.evaluateAll((nodes, exceptions) => {
    return nodes.every((node) => {
      for (let ancestor: Element | null = node.parentElement; ancestor
        && ancestor !== document.body && ancestor !== document.documentElement;
        ancestor = ancestor.parentElement) {
        const css = getComputedStyle(ancestor);
        const allowed = exceptions.some((selector) =>
          ancestor!.matches(selector));
        // Only actual scrolling, never hidden/clip, qualifies for an exception.
        const x = allowed && /^(auto|scroll)$/u.test(css.overflowX);
        const y = allowed && /^(auto|scroll)$/u.test(css.overflowY);
        if ((!x && /^(auto|scroll|hidden|clip)$/u.test(css.overflowX)
          && ancestor.scrollWidth > ancestor.clientWidth + 1)
        || (!y && /^(auto|scroll|hidden|clip)$/u.test(css.overflowY)
          && ancestor.scrollHeight > ancestor.clientHeight + 1)) {
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
    let contained = root.scrollWidth <= root.clientWidth + 1
      && ((outer !== root && !rootClipsY)
        || root.scrollHeight <= root.clientHeight + 1);
    let hasText = false;
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
      }[] = [
        { rect: bounds, x: true, y: outer === root || rootClipsY },
      ];
      if (outer !== root) {
        clips.push({ rect: outer.getBoundingClientRect(), x: false, y: true });
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
          });
        }
      }
      const range = document.createRange();
      range.selectNodeContents(node);
      const rects = Array.from(range.getClientRects());
      if (!rects.some((rect) => rect.width > 0 && rect.height > 0)) {
        visible = false;
      }
      for (const rect of rects) {
        if (next && rect.right > next.left + 1 && rect.left < next.right - 1
          && rect.bottom > next.top + 1 && rect.top < next.bottom - 1) {
          contained = false;
        }
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
  }, verticalContainer);
}
