import type { Locator } from '@playwright/test';

/** Check painted vertical bounds while retaining horizontal range bounds. */
export async function measurePrintTextContainment(container: Locator) {
  return container.evaluate((root) => {
    const context = document.createElement('canvas').getContext('2d');
    if (!context) {
      throw new Error('Unsupported print text metrics.');
    }
    const blocks = '.resume-header, .section-heading, .entry, .entry-body, '
      + '.entry-body p, .entry-body li, .entry-header, .entry-title, '
      + '.entry-subtitle, .entry-meta, .resume-name, .resume-headline, '
      + '.contact-chip';
    let visible = true;
    let contained = true;
    let hasText = false;
    const failures: { element: string; axis: string; delta: number }[] = [];
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const node = walker.currentNode;
      const parent = node.parentElement;
      const text = node.textContent;
      if (!parent || !text?.trim()) {
        continue;
      }
      hasText = true;
      const css = getComputedStyle(parent);
      context.font = `${css.fontStyle} ${css.fontWeight} ${css.fontSize} `
        + css.fontFamily;
      let shown = text;
      if (css.textTransform === 'uppercase') {
        const lng = root.closest('[lang]')?.getAttribute('lang') ?? 'en';
        shown = text.toLocaleUpperCase(lng);
      } else if (css.textTransform === 'capitalize') {
        shown = text.replace(/(^|\s)\p{L}/gu, (letter) => letter.toUpperCase());
      }
      const metrics = context.measureText(shown);
      const ascent = metrics.fontBoundingBoxAscent;
      const descent = metrics.fontBoundingBoxDescent;
      const inkAscent = metrics.actualBoundingBoxAscent;
      const inkDescent = metrics.actualBoundingBoxDescent;
      if (![ascent, descent, inkAscent, inkDescent].every(Number.isFinite)
        || ascent + descent <= 0 || inkAscent + inkDescent <= 0) {
        throw new Error('Unsupported print text metrics.');
      }
      const range = document.createRange();
      range.selectNodeContents(node);
      const rects = Array.from(range.getClientRects()).filter((rect) =>
        rect.width > 0 && rect.height > 0);
      if (rects.length === 0) {
        visible = false;
      }
      for (let ancestor: Element | null = parent; ancestor;
        ancestor = ancestor.parentElement) {
        const style = getComputedStyle(ancestor);
        if (style.display === 'none' || style.visibility !== 'visible'
          || Number(style.opacity) === 0) {
          visible = false;
        }
        const box = ancestor.getBoundingClientRect();
        const scoped = root.contains(ancestor)
          && (ancestor === root || ancestor.matches(blocks));
        const clips = /^(hidden|clip|scroll|auto)$/u;
        const x = scoped || clips.test(style.overflowX);
        const y = scoped || clips.test(style.overflowY);
        if ((x && ancestor.scrollWidth > ancestor.clientWidth + 1)
          || (clips.test(style.overflowY)
            && ancestor.scrollHeight > ancestor.clientHeight + 1)) {
          contained = false;
          if (failures.length < 8) {
            failures.push({ element: ancestor.className, axis: 'scroll',
              delta: Math.max(ancestor.scrollWidth - ancestor.clientWidth,
                ancestor.scrollHeight - ancestor.clientHeight) });
          }
        }
        for (const rect of rects) {
          // Range includes unused font ascent/descent beyond a line box.
          // Only painted ink can be clipped there (CSSOM View, Range bounds).
          const scale = rect.height / (ascent + descent);
          const baseline = rect.top + ascent * scale;
          const horizontal = Math.max(box.left - rect.left,
            rect.right - box.right);
          const vertical = Math.max(box.top - baseline + inkAscent * scale,
            baseline + inkDescent * scale - box.bottom);
          if ((x && horizontal > 1) || (y && vertical > 1)) {
            contained = false;
            if (failures.length < 8) {
              failures.push({ element: ancestor.className,
                axis: x && horizontal > 1 ? 'x' : 'y',
                delta: x && horizontal > 1 ? horizontal : vertical });
            }
          }
        }
      }
    }
    return { visible: visible && hasText, contained, failures };
  });
}
