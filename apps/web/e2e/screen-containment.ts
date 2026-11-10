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
  const result = await measurePaintedText(container, verticalContainer);
  return {
    visible: result.visible,
    contained: result.contained && result.separated,
  };
}

/** Keep painted bounds and distinct-line separation independently testable. */
export async function measurePaintedText(
  container: Locator, verticalContainer?: string,
): Promise<{ visible: boolean; contained: boolean; separated: boolean }> {
  return container.evaluate((root, selector) => {
    const bounds = root.getBoundingClientRect();
    const outer = selector ? root.closest(selector) : root;
    const invalid = { visible: false, contained: false, separated: false };
    if (!outer) {
      return invalid;
    }
    const rootStyle = getComputedStyle(root);
    const rootClipsY = /^(hidden|clip|scroll|auto)$/u.test(rootStyle.overflowY);
    const next = root.nextElementSibling?.getBoundingClientRect();
    let visible = bounds.width > 0 && bounds.height > 0;
    let contained = root.scrollWidth <= root.clientWidth + 1;
    let hasText = false;
    let separated = true;
    const canvas = document.createElement('canvas');
    const context = canvas.getContext('2d')!;
    const inkLines: {
      baseline: number; left: number; right: number;
      top: number; bottom: number;
    }[] = [];
    const segmenter = new Intl.Segmenter(undefined, {
      granularity: 'grapheme',
    });
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
      const rects = Array.from(range.getClientRects()).filter((rect) =>
        rect.width > 0 && rect.height > 0);
      if (rects.length === 0) {
        visible = false;
      }
      const css = getComputedStyle(parent);
      // Preserve the DOM's shaping state, including properties that do not
      // serialize into the font shorthand. Range advances retain justification.
      for (const property of Array.from(css)) {
        canvas.style.setProperty(property, css.getPropertyValue(property));
      }
      canvas.lang = parent.closest('[lang]')?.getAttribute('lang')
        ?? document.documentElement.lang;
      context.font = `${css.fontStyle} ${css.fontWeight} ${css.fontSize} `
        + css.fontFamily;
      const stretches: Record<string, string> = {
        '50%': 'ultra-condensed', '62.5%': 'extra-condensed',
        '75%': 'condensed', '87.5%': 'semi-condensed', '100%': 'normal',
        '112.5%': 'semi-expanded', '125%': 'expanded',
        '150%': 'extra-expanded', '200%': 'ultra-expanded',
      };
      const rendering: Record<string, string> = {
        optimizespeed: 'optimizeSpeed',
        optimizelegibility: 'optimizeLegibility',
        geometricprecision: 'geometricPrecision',
      };
      const state: Record<string, string> = {
        fontKerning: css.fontKerning,
        fontStretch: stretches[css.fontStretch] ?? css.fontStretch,
        fontVariantCaps: css.fontVariantCaps,
        letterSpacing: css.letterSpacing === 'normal'
          ? '0px'
          : css.letterSpacing,
        wordSpacing: css.wordSpacing === 'normal' ? '0px' : css.wordSpacing,
        textRendering: rendering[css.textRendering.toLowerCase()]
          ?? css.textRendering,
        direction: css.direction,
      };
      const drawing = context as unknown as Record<string, string>;
      for (const [key, value] of Object.entries(state)) {
        if (!(key in context)) {
          return invalid;
        }
        drawing[key] = value;
        if (drawing[key] !== value) {
          return invalid;
        }
      }
      // Canvas has no feature/variation override API. Reject unsupported
      // overrides rather than measuring glyphs with a different face.
      if (css.fontFeatureSettings !== 'normal'
        || css.fontVariationSettings !== 'normal'
        || css.fontVariantAlternates !== 'normal'
        || css.fontVariantEastAsian !== 'normal'
        || css.fontVariantPosition !== 'normal'
        || !['normal', 'tabular-nums'].includes(css.fontVariantNumeric)
        || !['normal', 'none', 'no-common-ligatures']
          .includes(css.fontVariantLigatures)
          || css.fontOpticalSizing !== 'auto'
          || css.fontSizeAdjust !== 'none'
          || !['none', 'uppercase', 'lowercase'].includes(css.textTransform)) {
        return invalid;
      }
      context.textAlign = 'left';
      context.textBaseline = 'alphabetic';
      const transformText = (value: string) => css.textTransform === 'uppercase'
        ? value.toLocaleUpperCase(canvas.lang || undefined)
        : css.textTransform === 'lowercase'
          ? value.toLocaleLowerCase(canvas.lang || undefined)
          : value;
      const lines: {
        rect: DOMRect; content: string;
        glyphs: { text: string; rect: DOMRect }[];
      }[] = [];
      for (const { index, segment } of segmenter.segment(node.textContent!)) {
        range.setStart(node, index);
        range.setEnd(node, index + segment.length);
        const rect = Array.from(range.getClientRects()).find((item) =>
          item.width > 0 && item.height > 0);
        if (!rect) {
          continue;
        }
        let line = lines.find((item) => Math.abs(item.rect.top - rect.top) < 0.1
          && Math.abs(item.rect.bottom - rect.bottom) < 0.1);
        if (!line) {
          line = { rect, content: '', glyphs: [] };
          lines.push(line);
        }
        line.content += segment;
        line.glyphs.push({ text: segment, rect });
      }
      for (const line of lines) {
        const metrics = context.measureText(transformText(line.content));
        const height = metrics.fontBoundingBoxAscent
          + metrics.fontBoundingBoxDescent;
        if (!Number.isFinite(height + metrics.actualBoundingBoxAscent
          + metrics.actualBoundingBoxDescent + metrics.actualBoundingBoxLeft
          + metrics.actualBoundingBoxRight) || height <= 0) {
          return invalid;
        }
        const scale = line.rect.height / height;
        const baseline = line.rect.top + metrics.fontBoundingBoxAscent * scale;
        const ink = {
          top: baseline - metrics.actualBoundingBoxAscent * scale,
          bottom: baseline + metrics.actualBoundingBoxDescent * scale,
          left: Infinity, right: -Infinity,
        };
        // Measure edge glyphs at their actual DOM advances. This retains
        // spacing, numeric advances and disabled common ligatures, and includes
        // italic overhang rather than treating Range advances as painted ink.
        for (const glyph of line.glyphs) {
          if (!glyph.text.trim()) {
            continue;
          }
          const metric = context.measureText(transformText(glyph.text));
          if (css.fontVariantLigatures !== 'normal') {
            ink.top = Math.min(ink.top,
              baseline - metric.actualBoundingBoxAscent * scale);
            ink.bottom = Math.max(ink.bottom,
              baseline + metric.actualBoundingBoxDescent * scale);
          }
          ink.left = Math.min(ink.left,
            glyph.rect.left - metric.actualBoundingBoxLeft * scale);
          ink.right = Math.max(ink.right,
            glyph.rect.left + metric.actualBoundingBoxRight * scale);
        }
        if (!Number.isFinite(ink.left + ink.right)) {
          continue;
        }
        if (next && ink.right > next.left + 1 && ink.left < next.right - 1
          && ink.bottom > next.top + 1 && ink.top < next.bottom - 1) {
          contained = false;
        }
        for (const clip of clips) {
          if ((clip.x && (ink.left < clip.rect.left - 1
            || ink.right > clip.rect.right + 1))
          || (clip.y && (ink.top < clip.rect.top - 1
            || ink.bottom > clip.rect.bottom + 1))) {
            contained = false;
          }
        }
        for (const previous of inkLines) {
          if (Math.abs(previous.baseline - baseline) > 0.1
            && Math.min(previous.right, ink.right)
            > Math.max(previous.left, ink.left) + 1
            && Math.min(previous.bottom, ink.bottom)
            > Math.max(previous.top, ink.top) + 1) {
            separated = false;
          }
        }
        inkLines.push({ ...ink, baseline });
      }
    }
    return { visible: visible && hasText, contained, separated };
  }, verticalContainer);
}
