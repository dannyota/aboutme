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

interface TextBox {
  left: number;
  right: number;
  top: number;
  bottom: number;
}

interface TextFailure {
  node: string;
  text: string;
  rect: TextBox | null;
  ancestor: string;
  box: TextBox;
  axis: string;
  delta: number;
  allowance: number;
  kind: string;
}

interface TextMeasurement {
  visible: boolean;
  contained: boolean;
  separated: boolean;
  failures: TextFailure[];
}

/** Inspect clips before scrolling can reveal a hidden descendant. */
export async function rejectAncestorClipping(
  text: Locator, scrolling: readonly string[],
): Promise<void> {
  for (const node of await text.all()) {
    await expect.poll(async () => {
      const measured = await measureScreenText(node, undefined, scrolling);
      return measured.failures.filter((failure) => failure.kind === 'clip');
    }, 'Screen containment.').toEqual([]);
  }
}

/** Keep the shared screen and print callers' result shape stable. */
export async function measureTextContainment(
  container: Locator,
  verticalContainer?: string,
): Promise<{ visible: boolean; contained: boolean }> {
  const result = await measureScreenText(container, verticalContainer);
  return {
    visible: result.visible,
    contained: result.contained && result.separated,
  };
}

/** Inspect Range boxes without trying to reconstruct painted glyph bounds. */
export async function measureScreenText(
  container: Locator, verticalContainer?: string,
  scrolling: readonly string[] = [],
): Promise<TextMeasurement> {
  return container.evaluate((root, options) => {
    const box = (rect: DOMRect): TextBox => ({
      left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom,
    });
    const path = (node: Element): string => {
      const parts: string[] = [];
      for (let element: Element | null = node; element;
        element = element.parentElement) {
        const id = element.id ? `#${element.id}` : '';
        parts.unshift(`${element.localName}${id}`);
        if (id || parts.length === 4) {
          break;
        }
      }
      return parts.join(' > ');
    };
    const bounds = root.getBoundingClientRect();
    const failures: TextFailure[] = [];
    let visible = bounds.width > 0 && bounds.height > 0;
    let contained = true;
    let separated = true;
    let hasText = false;
    const lines: {
      rect: DOMRect; baseline: number; fontSize: number;
      block: Element; source: Node; hardLine: number;
    }[] = [];
    const checked = new Set<Element>();
    if (options.selector && !root.closest(options.selector)) {
      return { visible: false, contained: false, separated: false, failures };
    }
    const walker = document.createTreeWalker(root,
      NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
    let hardLine = 0;
    while (walker.nextNode()) {
      const node = walker.currentNode;
      if (node.nodeName === 'BR') {
        hardLine += 1;
      }
      if (node.nodeType !== Node.TEXT_NODE) {
        continue;
      }
      const parent = node.parentElement;
      if (!parent || !node.textContent?.trim()) {
        continue;
      }
      hasText = true;
      const excerpt = node.textContent.trim().replace(/\s+/gu, ' ')
        .slice(0, 80);
      const fail = (
        kind: string, ancestor: string, rect: DOMRect | null,
        clip: DOMRect | TextBox, axis: string, delta: number, allowance = 1,
      ) => {
        if (kind === 'lines') {
          separated = false;
        } else if (kind === 'hidden' || kind === 'empty') {
          visible = false;
        } else {
          contained = false;
        }
        if (failures.length < 12) {
          failures.push({ node: path(parent), text: excerpt,
            rect: rect ? box(rect) : null, ancestor, box: {
              left: clip.left, right: clip.right,
              top: clip.top, bottom: clip.bottom,
            }, axis, delta, allowance, kind });
        }
      };
      let block = parent;
      while (block !== root && block.parentElement
        && /^(inline|contents)$/u.test(getComputedStyle(block).display)) {
        block = block.parentElement;
      }
      const ancestors: {
        node: Element; rect: DOMRect; x: boolean; y: boolean;
      }[] = [];
      let scrollX = false;
      for (let element: Element | null = parent; element;
        element = element.parentElement) {
        const css = getComputedStyle(element);
        const rect = element.getBoundingClientRect();
        const named = options.scrolling.some((selector) =>
          element!.matches(selector));
        const allowedX = named && /^(auto|scroll)$/u.test(css.overflowX);
        const allowedY = named && /^(auto|scroll)$/u.test(css.overflowY);
        scrollX ||= allowedX;
        if (css.display === 'none' || css.visibility !== 'visible'
          || Number(css.opacity) === 0) {
          fail('hidden', path(element), null, rect, 'visibility', 0);
        }
        if (!checked.has(element)) {
          checked.add(element);
          const delta = element.scrollWidth - element.clientWidth;
          if (!allowedX && delta > 1) {
            fail('scroll', path(element), null, rect, 'x', delta);
          }
        }
        ancestors.push({ node: element, rect,
          x: !allowedX && /^(hidden|clip|auto|scroll)$/u.test(css.overflowX),
          y: !allowedY && /^(hidden|clip|auto|scroll)$/u.test(css.overflowY) });
      }
      const range = document.createRange();
      range.selectNodeContents(node);
      const rects = Array.from(range.getClientRects()).filter((rect) =>
        rect.width > 0 && rect.height > 0);
      if (rects.length === 0) {
        fail('empty', path(parent), null, bounds, 'visibility', 0);
      }
      const fontSize = parseFloat(getComputedStyle(parent).fontSize);
      for (const rect of rects) {
        const allowance = (rect.height - fontSize) / 2 + 1;
        if (!scrollX) {
          for (const element of new Set([root, block])) {
            const clip = element.getBoundingClientRect();
            const delta = Math.max(clip.left - rect.left,
              rect.right - clip.right);
            if (delta > 1) {
              fail('container', path(element), rect, clip, 'x', delta);
            }
          }
          const delta = Math.max(-rect.left, rect.right - innerWidth);
          if (delta > 1) {
            fail('container', 'viewport', rect, {
              left: 0, right: innerWidth, top: 0, bottom: innerHeight,
            }, 'x', delta);
          }
        }
        for (const ancestor of ancestors) {
          const clip = ancestor.rect;
          const dx = Math.max(clip.left - rect.left, rect.right - clip.right);
          const dy = Math.max(clip.top - rect.top, rect.bottom - clip.bottom);
          if (ancestor.x && dx > 1) {
            fail('clip', path(ancestor.node), rect, clip, 'x', dx);
          }
          if (ancestor.y && dy > allowance) {
            fail('clip', path(ancestor.node), rect, clip, 'y', dy, allowance);
          }
        }
        // A shared font's Range bottom has a constant offset from its baseline.
        // Wrapped fragments and explicit breaks are distinct logical lines.
        const baseline = rect.bottom - (rect.height - fontSize) / 2;
        for (const previous of lines) {
          const gap = Math.abs(previous.baseline - baseline);
          const minimum = Math.min(previous.fontSize, fontSize) / 2;
          const distinct = previous.hardLine !== hardLine
            || (previous.source === node && gap > 0.1)
            || Math.min(previous.rect.right, rect.right)
            > Math.max(previous.rect.left, rect.left) + 1;
          if (previous.block === block && distinct && gap < minimum) {
            fail('lines', path(block), rect, previous.rect, 'y',
              minimum - gap, 0);
          }
        }
        lines.push({ rect, baseline, fontSize, block, source: node, hardLine });
      }
    }
    return { visible: visible && hasText, contained, separated, failures };
  }, { selector: verticalContainer, scrolling: [...scrolling] });
}
