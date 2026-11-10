import type { Locator } from '@playwright/test';

/** Check line ink, advances and scroll bounds without changing print layout. */
export async function measurePrintTextContainment(container: Locator) {
  return container.evaluate((root) => {
    type Box = { left: number; right: number; top: number; bottom: number };
    const bounds = (rect: Box): Box => ({ left: rect.left, right: rect.right,
      top: rect.top, bottom: rect.bottom });
    const path = (element: Element): string => {
      const parts: string[] = [];
      for (let node: Element | null = element; node;
        node = node.parentElement) {
        const index = Array.from(node.parentElement?.children ?? [])
          .filter((sibling) => sibling.tagName === node!.tagName)
          .indexOf(node) + 1;
        const classes = Array.from(node.classList, (name) =>
          `.${CSS.escape(name)}`).join('');
        parts.unshift(`${node.localName}${classes}:nth-of-type(${index})`);
        if (node === root) {
          break;
        }
      }
      return parts.join(' > ');
    };
    const blocks = '.resume-header, .section-heading, .entry, .entry-body, '
      + '.entry-body p, .entry-body li, .entry-header, .entry-title, '
      + '.entry-subtitle, .entry-meta, .resume-name, .resume-headline, '
      + '.contact-chip';
    const clips = /^(hidden|clip|scroll|auto)$/u;
    const host = document.createElement('div');
    host.style.cssText = 'all: initial; position: fixed; left: 0; top: 0; '
      + 'visibility: hidden; pointer-events: none; width: max-content; '
      + 'white-space: pre;';
    const canvas = document.createElement('canvas');
    canvas.width = 1;
    canvas.height = 1;
    canvas.style.cssText = 'position: absolute; width: 1px; height: 1px; '
      + 'left: 0; top: 0;';
    const probe = document.createElement('span');
    const marker = document.createElement('span');
    marker.style.cssText = 'display: inline-block; width: 0; height: 0; '
      + 'padding: 0; margin: 0; border: 0; vertical-align: baseline;';
    host.append(probe, marker, canvas);
    document.body.append(host);
    const context = canvas.getContext('2d');
    let visible = true;
    let contained = true;
    let hasText = false;
    const lines: {
      node: string; text: string; range: Box; ink: Box; baseline: number;
      advance: number; referenceWidth: number;
      metrics: { ascent: number; descent: number; left: number; right: number };
    }[] = [];
    const owners: (Element | null)[] = [];
    const failures: {
      node: string; container: string; text: string; kind: string;
      axis: string; delta: number; ink: Box | null; box: Box;
      range: Box | null;
    }[] = [];
    const fail = (line: typeof lines[number], element: Element,
      kind: string, axis: string, delta: number, box: Box) => {
      contained = false;
      if (failures.length < 8) {
        failures.push({ node: line.node, container: path(element),
          text: line.text.slice(0, 100), kind, axis, delta,
          ink: line.ink, box: bounds(box), range: line.range });
      }
    };
    try {
      if (!context) {
        throw new Error('Unsupported print text metrics: canvas.');
      }
      const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
      while (walker.nextNode()) {
        const node = walker.currentNode;
        const parent = node.parentElement;
        const text = node.textContent;
        if (!parent || !text?.trim()) {
          continue;
        }
        hasText = true;
        const nodePath = path(parent) + ' > #text['
          + (Array.from(parent.childNodes).filter((child) =>
            child.nodeType === Node.TEXT_NODE).indexOf(node as Text) + 1)
          + ']';
        const unsupported = (reason: string): never => {
          throw new Error('Unsupported print text metrics. '
            + JSON.stringify({ node: nodePath, text: text.slice(0, 100),
              reason }));
        };
        const css = getComputedStyle(parent);
        const lng = parent.closest('[lang]')?.getAttribute('lang') || 'und';
        if (css.writingMode !== 'horizontal-tb') {
          unsupported('writing mode');
        }
        // An attached canvas retains the CSS font description in Chromium,
        // including OpenType features, variations, synthesis and variants.
        // The context exposes spacing, caps and kerning as separate state.
        for (const key of Array.from(css).filter((key) =>
          key.startsWith('font-') || ['letter-spacing', 'word-spacing',
            'text-rendering', 'direction'].includes(key))) {
          const value = css.getPropertyValue(key);
          probe.style.setProperty(key, value);
          canvas.style.setProperty(key, value);
          host.style.setProperty(key, value);
        }
        canvas.lang = lng;
        probe.lang = lng;
        context.font = `${css.fontStyle} ${css.fontWeight} ${css.fontSize} `
          + css.fontFamily;
        context.fontKerning = css.fontKerning as CanvasFontKerning;
        context.fontVariantCaps = css.fontVariantCaps as CanvasFontVariantCaps;
        const stretches: Record<string, CanvasFontStretch> = {
          '50%': 'ultra-condensed', '62.5%': 'extra-condensed',
          '75%': 'condensed', '87.5%': 'semi-condensed', '100%': 'normal',
          '112.5%': 'semi-expanded', '125%': 'expanded',
          '150%': 'extra-expanded', '200%': 'ultra-expanded',
        };
        const stretch = stretches[css.fontStretch] ?? css.fontStretch;
        if (!Object.values(stretches).includes(stretch as CanvasFontStretch)) {
          unsupported('font stretch');
        }
        context.fontStretch = stretch as CanvasFontStretch;
        context.letterSpacing = css.letterSpacing === 'normal'
          ? '0px'
          : css.letterSpacing;
        context.wordSpacing = css.wordSpacing === 'normal'
          ? '0px'
          : css.wordSpacing;
        context.textRendering = css.textRendering as CanvasTextRendering;
        context.direction = css.direction as CanvasDirection;
        context.textAlign = 'left';
        context.textBaseline = 'alphabetic';
        let scaleX = 1;
        let hidden: Element | undefined;
        for (let ancestor: Element | null = parent; ancestor;
          ancestor = ancestor.parentElement) {
          const style = getComputedStyle(ancestor);
          const matrix = new DOMMatrix(style.transform === 'none'
            ? undefined
            : style.transform);
          if (!matrix.is2D || matrix.b !== 0 || matrix.c !== 0
            || matrix.a <= 0 || matrix.d <= 0) {
            unsupported('non-axis-aligned transform');
          }
          scaleX *= matrix.a * (parseFloat(style.zoom) || 1);
          if (style.display === 'none' || style.visibility !== 'visible'
            || Number(style.opacity) === 0) {
            visible = false;
            hidden = ancestor;
          }
        }
        const range = document.createRange();
        const fragments: { start: number; end: number; rect: Box }[] = [];
        // Grapheme ranges find the browser's actual wrap boundaries without
        // splitting a Vietnamese accent cluster or assuming equal line ink.
        const segments = new Intl.Segmenter(lng, { granularity: 'grapheme' });
        for (const { index, segment } of segments.segment(text)) {
          range.setStart(node, index);
          range.setEnd(node, index + segment.length);
          const rects = Array.from(range.getClientRects()).filter((rect) =>
            rect.width > 0 && rect.height > 0);
          if (rects.length > 1) {
            unsupported('multiple grapheme fragments');
          }
          const rect = rects[0];
          if (!rect) {
            continue;
          }
          const previous = fragments.at(-1);
          if (previous && Math.abs(previous.rect.top - rect.top) < 0.01
            && Math.abs(previous.rect.bottom - rect.bottom) < 0.01) {
            previous.end = index + segment.length;
            previous.rect.left = Math.min(previous.rect.left, rect.left);
            previous.rect.right = Math.max(previous.rect.right, rect.right);
          } else {
            fragments.push({ start: index, end: index + segment.length,
              rect: bounds(rect) });
          }
        }
        if (fragments.length === 0) {
          visible = false;
          if (failures.length < 8) {
            failures.push({ node: nodePath, container: path(hidden ?? parent),
              text: text.slice(0, 100), kind: 'unpainted', axis: 'visibility',
              delta: 0, ink: null, range: null,
              box: bounds((hidden ?? parent).getBoundingClientRect()) });
          }
        }
        for (const fragment of fragments) {
          let shown = text.slice(fragment.start, fragment.end);
          if (['normal', 'nowrap', 'pre-line'].includes(css.whiteSpace)) {
            shown = shown.replace(/[\t\n\r\f ]+/gu, ' ');
          } else if (/[\t\n\r\f]/u.test(shown)) {
            unsupported('preserved control whitespace');
          }
          if (!shown.trim()) {
            continue;
          }
          if (css.textTransform === 'uppercase') {
            shown = shown.toLocaleUpperCase(lng);
          } else if (css.textTransform === 'lowercase') {
            shown = shown.toLocaleLowerCase(lng);
          } else if (css.textTransform === 'capitalize') {
            const prefix = text[fragment.start - 1] ?? ' ';
            shown = (prefix + shown).replace(/(^|\P{L})(\p{L})/gu,
              (_, before: string, letter: string) =>
                before + letter.toLocaleUpperCase(lng)).slice(prefix.length);
          } else if (css.textTransform !== 'none') {
            unsupported('text transform');
          }
          probe.textContent = shown;
          const reference = document.createRange();
          reference.selectNodeContents(probe);
          const referenceBox = reference.getBoundingClientRect();
          const metrics = context.measureText(shown);
          const ascent = metrics.actualBoundingBoxAscent;
          const descent = metrics.actualBoundingBoxDescent;
          const left = metrics.actualBoundingBoxLeft;
          const right = metrics.actualBoundingBoxRight;
          if (![ascent, descent, left, right, metrics.width,
            referenceBox.height].every(Number.isFinite)
            || ascent + descent <= 0 || right + left <= 0
            || referenceBox.height <= 0) {
            unsupported('nonpositive ink');
          }
          if (Math.abs(metrics.width - referenceBox.width) > 1) {
            unsupported(`shaping widths ${metrics.width}`
              + ` canvas / ${referenceBox.width} DOM`);
          }
          const rect = fragment.rect;
          const scaleY = (rect.bottom - rect.top) / referenceBox.height;
          // A zero-height inline block puts its bottom on the DOM baseline.
          // Calibrate against that baseline, never a guessed font/em ratio.
          const baseline = rect.top + (marker.getBoundingClientRect().bottom
            - referenceBox.top) * scaleY;
          const ink = { left: rect.left - left * scaleX,
            right: rect.right + (right - metrics.width) * scaleX,
            top: baseline - ascent * scaleY,
            bottom: baseline + descent * scaleY };
          const line = { node: nodePath, text: shown, range: rect, ink,
            baseline, advance: metrics.width,
            referenceWidth: referenceBox.width,
            metrics: { ascent, descent, left, right } };
          lines.push(line);
          if (hidden) {
            fail(line, hidden, 'unpainted', 'visibility', 0,
              hidden.getBoundingClientRect());
          }
          const heading = parent.closest('.section-heading');
          owners.push(heading);
          for (let ancestor: Element | null = parent; ancestor;
            ancestor = ancestor.parentElement) {
            const style = getComputedStyle(ancestor);
            const box = ancestor.getBoundingClientRect();
            const scoped = root.contains(ancestor)
              && (ancestor === root || ancestor.matches(blocks));
            const x = scoped || clips.test(style.overflowX);
            // The renderer's 1.2-line-height heading has visible accent
            // overhang (docs/design/templates/tokens.md section 3.2).
            // Keep root/ancestor clips and heading collisions strict.
            const headingOverhang = ancestor === heading && ancestor !== root
              && style.overflowY === 'visible'
              && Math.abs(parseFloat(css.lineHeight)
                - parseFloat(css.fontSize) * 1.2) < 0.1;
            const y = (scoped && !headingOverhang)
              || clips.test(style.overflowY);
            if ((x && ancestor.scrollWidth > ancestor.clientWidth + 1)
              || (clips.test(style.overflowY)
                && ancestor.scrollHeight > ancestor.clientHeight + 1)) {
              fail(line, ancestor, 'scroll', 'scroll', Math.max(
                ancestor.scrollWidth - ancestor.clientWidth,
                ancestor.scrollHeight - ancestor.clientHeight,
              ), box);
            }
            const horizontal = Math.max(box.left - rect.left,
              rect.right - box.right);
            if (x && horizontal > 1) {
              fail(line, ancestor, 'advance', 'x', horizontal, box);
            }
            const inkX = Math.max(box.left - ink.left, ink.right - box.right);
            const inkY = Math.max(box.top - ink.top, ink.bottom - box.bottom);
            if (x && inkX > 1) {
              fail(line, ancestor, 'ink', 'x', inkX, box);
            }
            if (y && inkY > 1) {
              fail(line, ancestor, 'ink', 'y', inkY, box);
            }
          }
        }
      }
      for (const [index, line] of lines.entries()) {
        for (let other = 0; other < index; other++) {
          if (!owners[index] && !owners[other]) {
            continue;
          }
          const previous = lines[other]!;
          const x = Math.min(line.ink.right, previous.ink.right)
            - Math.max(line.ink.left, previous.ink.left);
          const y = Math.min(line.ink.bottom, previous.ink.bottom)
            - Math.max(line.ink.top, previous.ink.top);
          if (x > 1 && y > 1) {
            fail(line, owners[index] ?? owners[other]!, 'overlap', 'y', y,
              previous.ink);
          }
        }
      }
      if (!hasText) {
        failures.push({ node: path(root), container: path(root), text: '',
          kind: 'empty', axis: 'visibility', delta: 0, ink: null, range: null,
          box: bounds(root.getBoundingClientRect()) });
      }
      return { visible: visible && hasText, contained, failures, lines };
    } finally {
      host.remove();
    }
  });
}
