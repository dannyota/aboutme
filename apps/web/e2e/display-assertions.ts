import type { Locator, Page } from '@playwright/test';

import type { ScreenContract } from './screen-contract';

export { measureTextContainment } from './screen-containment';

/** Measure all adjacent boxes at once to avoid mixing layout frames. */
export async function measureVerticalFlow(items: Locator): Promise<boolean> {
  return items.evaluateAll((elements) => {
    const boxes = elements.map((element) => element.getBoundingClientRect());
    return boxes.every((box, index) => box.width > 0 && box.height > 0
      && (index === 0 || box.top >= boxes[index - 1]!.bottom - 1));
  });
}

// --- chrome.spec.ts ---
export async function measureChromeGrid(grid: Locator): Promise<{
  columns: number;
  gapX: number;
  gapY: number;
  separated: boolean;
}> {
  return grid.evaluate((element) => {
    const style = getComputedStyle(element);
    const boxes = Array.from(element.children, (child) =>
      child.getBoundingClientRect());
    const first = boxes[0];
    const columns = first
      ? boxes.filter((box) => Math.abs(box.top - first.top) <= 1).length
      : 0;
    const gapX = Number.parseFloat(style.columnGap);
    const gapY = Number.parseFloat(style.rowGap);
    const separated = boxes.every((box, index) => box.width > 0
      && box.height > 0 && boxes.slice(index + 1).every((other) =>
      Math.min(box.right, other.right) - Math.max(box.left, other.left) <= 1
      || Math.min(box.bottom, other.bottom) - Math.max(box.top, other.top)
      <= 1)) && boxes.every((box, index) => {
      const column = index % columns;
      const rowStart = index - column;
      const row = boxes.slice(rowStart, rowStart + columns);
      if (Math.abs(box.top - row[0]!.top) > 1) {
        return false;
      }
      if (column > 0
        && Math.abs(box.left - boxes[index - 1]!.right - gapX) > 1) {
        return false;
      }
      if (rowStart === 0) {
        return true;
      }
      const previous = boxes.slice(rowStart - columns, rowStart);
      const bottom = Math.max(...previous.map((item) => item.bottom));
      return Math.abs(box.top - bottom - gapY) <= 1;
    });
    return {
      columns,
      gapX,
      gapY,
      separated,
    };
  });
}

export async function measureChromeSheet(stage: Locator): Promise<boolean> {
  return stage.evaluate((element) => {
    const sheet = element.querySelector('[data-testid="landing-sheet"]');
    const ghost = element.querySelector('[data-testid="landing-ghost"]');
    if (!sheet || !ghost) {
      return false;
    }
    const a = element.getBoundingClientRect();
    const b = sheet.getBoundingClientRect();
    const style = getComputedStyle(ghost);
    const offset = style.translate.split(' ').map(Number.parseFloat);
    return a.width > 0 && a.height > 0
      && Math.abs(a.left - b.left) <= 1
      && Math.abs(a.top - b.top) <= 1
      && Math.abs(a.width - b.width) <= 1
      && Math.abs(a.height - b.height) <= 1
      && Math.abs((offset[0] ?? 0) - (innerWidth >= 672 ? 24 : 12)) <= 1
      && Math.abs((offset[1] ?? 0) - (innerWidth >= 672 ? -24 : -12)) <= 1;
  });
}

export async function measureChromeThumbnail(card: Locator): Promise<boolean> {
  return card.evaluate((element) => {
    const frame = element.querySelector('[data-sheet-thumbnail]');
    const sheet = element.querySelector('.scaled-sheet-content');
    if (!frame || !sheet) {
      return false;
    }
    const box = frame.getBoundingClientRect();
    const page = sheet.getBoundingClientRect();
    const transform = new DOMMatrixReadOnly(getComputedStyle(sheet).transform);
    return box.width > 0 && box.height > 0
      && Math.abs(box.width / box.height - 794 / 1123) < 0.001
      && Math.abs(transform.a - box.width / 794) < 0.001
      && Math.abs(page.width - box.width) <= 1
      && getComputedStyle(frame).backgroundColor === 'rgb(255, 255, 255)'
      && getComputedStyle(frame).overflow === 'hidden';
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
    const columns = style.gridTemplateColumns.split(' ').length;
    const columnGap = Number.parseFloat(style.columnGap);
    const rowGap = Number.parseFloat(style.rowGap);
    return {
      columns,
      columnGap,
      rowGap,
      ordered: boxes.every((box, index) => {
        if (box.width <= 0 || box.height <= 0) {
          return false;
        }
        const column = index % columns;
        const rowStart = index - column;
        if (Math.abs(box.top - boxes[rowStart]!.top) > 1) {
          return false;
        }
        if (column > 0
          && Math.abs(box.left - boxes[index - 1]!.right - columnGap) > 1) {
          return false;
        }
        if (rowStart === 0) {
          return true;
        }
        const previous = boxes.slice(rowStart - columns, rowStart);
        const bottom = Math.max(...previous.map((item) => item.bottom));
        return Math.abs(box.top - bottom - rowGap) <= 1;
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

// --- verify and card ---

/** Measure crop geometry in viewport coordinates. */
export async function measureCardCrop(image: Locator): Promise<{
  height: number;
  left: number;
  top: number;
  width: number;
}> {
  return image.evaluate((element) => {
    const imageBox = element.getBoundingClientRect();
    const frame = element.parentElement!.getBoundingClientRect();
    return {
      height: imageBox.height / frame.height,
      left: (imageBox.left - frame.left) / frame.width,
      top: (imageBox.top - frame.top) / frame.height,
      width: imageBox.width / frame.width,
    };
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
              || (Math.abs(box.left - previous.right - expected.gap) <= 1
                && Math.abs(box.top - previous.top) <= 1))
              && (!above
                || Math.abs(box.top - above.bottom - expected.gap) <= 1);
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

// --- print.spec.ts ---
export async function measurePrintRules(
  page: Page,
): Promise<Record<string, string>> {
  return page.locator('.resume-document').evaluate((root) => {
    const style = (selector: string): CSSStyleDeclaration => {
      const element = root.querySelector(selector);
      if (element === null) {
        throw new Error('Missing print rule fixture.');
      }
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

// --- sample-pages.spec.ts ---
export async function measurePdfLayout(
  page: Page,
  margins: { x: number; y: number },
) {
  return page.locator('.resume-document').evaluate((root, inset) => {
    const origin = root.getBoundingClientRect();
    const box = (element: Element) => {
      const rect = element.getBoundingClientRect();
      return {
        left: (rect.left - origin.left) * 0.75 + inset.x,
        right: (rect.right - origin.left) * 0.75 + inset.x,
        top: (rect.top - origin.top) * 0.75 + inset.y,
        bottom: (rect.bottom - origin.top) * 0.75 + inset.y,
      };
    };
    const region = (selector: string) => {
      const element = root.querySelector(selector);
      return element ? box(element) : null;
    };
    const icons = (container: Element) => Array.from(
      container.querySelectorAll<SVGGraphicsElement>(
        '.resume-icon path, .resume-icon circle, .resume-icon rect, '
        + '.resume-icon line, .resume-icon polyline, .resume-icon polygon, '
        + '.resume-icon ellipse',
      ),
    ).flatMap((element) => {
      const css = getComputedStyle(element);
      const matrix = element.getScreenCTM()!;
      return [false, true].flatMap((stroke) => {
        const resolved = stroke ? css.stroke : css.fill;
        if (resolved === 'none') {
          return [];
        }
        const channels = resolved.match(/[\d.]+/gu);
        if (channels?.length !== 3) {
          throw new Error('Unsupported icon ink.');
        }
        const color = '#' + channels.map((channel) => Number(channel)
          .toString(16).padStart(2, '0')).join('');
        const width = stroke ? parseFloat(css.strokeWidth) / 2 : 0;
        const dx = width * Math.hypot(matrix.a, matrix.c) * 0.75;
        const dy = width * Math.hypot(matrix.b, matrix.d) * 0.75;
        const bounds = box(element);
        return [{ left: bounds.left - dx, right: bounds.right + dx,
          top: bounds.top - dy, bottom: bounds.bottom + dy, color, stroke }];
      });
    });
    const style = getComputedStyle(root);
    const chunks = [];
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const node = walker.currentNode;
      const parent = node.parentElement;
      const text = node.textContent?.normalize('NFC').trim();
      if (!parent || !text || parent.closest('.level-widget')) {
        continue;
      }
      const css = getComputedStyle(parent);
      const roles = [
        ['.resume-name', 'name'], ['.resume-headline', 'headline'],
        ['.section-heading', 'heading'], ['.entry-title', 'title'],
        ['.entry-subtitle', 'subtitle'], ['.entry-meta', 'meta'],
      ];
      chunks.push({
        text,
        role: roles.find(([selector]) => parent.closest(selector!))?.[1]
          ?? 'body',
        region: parent.closest('.resume-header')
          ? 'header'
          : parent.closest('.resume-sidebar')
            ? 'sidebar'
            : 'main',
        linked: parent.closest('a') !== null,
        strong: parent.closest('b, strong') !== null,
        color: css.color,
        size: parseFloat(css.fontSize),
        weight: css.fontWeight,
      });
    }
    return {
      width: origin.width * 0.75,
      family: style.fontFamily,
      synthesis: style.fontSynthesis,
      size: parseFloat(style.fontSize),
      lineHeight: parseFloat(style.lineHeight),
      background: style.backgroundColor,
      chunks,
      header: region('.resume-header'),
      sidebar: region('.resume-sidebar'),
      photo: region('.resume-photo'),
      icons: icons(root),
      text: Array.from(root.querySelectorAll(
        '.resume-name, .resume-headline, .section-heading h2, '
        + '.entry-title, .entry-subtitle, .entry-meta, .entry-body p',
      ), (element) => {
        const css = getComputedStyle(element);
        return {
          role: element.matches('h2')
            ? 'heading'
            : element.matches('.entry-body p')
              ? 'body'
              : element.className,
          text: element.textContent?.normalize('NFC').trim() ?? '',
          ...box(element),
          icons: element.matches('h2') ? icons(element.parentElement!) : [],
          color: css.color,
          size: parseFloat(css.fontSize),
          weight: css.fontWeight,
          transform: css.textTransform,
          spacing: parseFloat(css.letterSpacing) || 0,
          align: css.textAlign,
          lineHeight: parseFloat(css.lineHeight),
        };
      }),
      paragraphs: Array.from(root.querySelectorAll('.entry-body p'),
        (element) => ({ text: element.textContent?.normalize('NFC') ?? '' })),
      headings: Array.from(root.querySelectorAll('.section-heading'),
        (element) => {
          const css = getComputedStyle(element);
          const bounds = box(element);
          const rule = parseFloat(css.borderBottomWidth);
          return { ...bounds,
            sidebar: element.closest('.resume-sidebar') !== null,
            rule,
            ruleStyle: css.borderBottomStyle,
            ruleBox: { ...bounds, top: bounds.bottom - rule * 0.75 },
            color: css.borderBottomColor,
            gap: parseFloat(css.marginBottom),
          };
        }),
      entryGaps: Array.from(root.querySelectorAll('.entry'), (element) =>
        parseFloat(getComputedStyle(element).marginBottom)),
      sectionGaps: Array.from(root.querySelectorAll('.resume-section'),
        (element) => parseFloat(getComputedStyle(element).marginBottom)),
      headerAlign: getComputedStyle(root.querySelector('.resume-header')!)
        .textAlign,
      widgets: Array.from(root.querySelectorAll('.entry'), (entry) => {
        const widget = entry.querySelector('.level-widget');
        const css = widget ? getComputedStyle(widget) : null;
        const track = entry.querySelector('.level-track');
        const fill = entry.querySelector('.level-fill');
        return {
          name: entry.querySelector('.entry-title')?.textContent?.trim() ?? '',
          entry: box(entry),
          title: regionForTitle(entry),
          // The bar's inline wrapper includes empty line fragments.
          widget: track ? box(track) : widget ? box(widget) : null,
          size: css ? parseFloat(css.fontSize) : 0,
          paddingX: css ? parseFloat(css.paddingLeft) : 0,
          paddingY: css ? parseFloat(css.paddingTop) : 0,
          border: css ? parseFloat(css.borderLeftWidth) : 0,
          radius: css ? parseFloat(css.borderTopLeftRadius) : 0,
          label: widget?.textContent?.trim() ?? '',
          track: track ? box(track) : null,
          fill: fill ? box(fill) : null,
          dots: Array.from(entry.querySelectorAll('.level-dot'), box),
        };
      }),
    };
    function regionForTitle(entry: Element) {
      const title = entry.querySelector('.entry-title');
      return title ? box(title) : null;
    }
  }, margins);
}

export {
  expectScreen, expectScreenClass, SCREEN_CLASSES,
} from './screen-contract';
export type { ScreenContract, ScreenClass } from './screen-contract';

// Preset fixtures, encoded independently of the production token resolver.
export const RESUME_STYLES = {
  'classic-serif': [
    'Roboto Serif', 'Noto Serif', 13, 1.5, 25, 20, 22, 12, '#23262c',
  ],
  'engineer-compact': [
    'Source Sans 3', 'Noto Sans', 13, 1.3, 11, 12, 12, 6, '#1a1f24',
  ],
  'modern-sidebar': [
    'Be Vietnam Pro', 'Noto Sans', 14, 1.5, 14, 13, 20, 10, '#1c2b33',
  ],
  'executive-band': [
    'Be Vietnam Pro', 'Noto Sans', 15, 1.45, 18, 14, 24, 12, '#1f2937',
  ],
  'consulting-formal': [
    'Inter', 'Noto Sans', 13, 1.45, 25, 15, 15, 9, '#33383d',
  ],
  'academic-dense': [
    'Source Sans 3', 'Noto Sans', 13, 1.3, 20, 12, 20, 6, '#1a1e22',
  ],
  'editorial-wide': [
    'Alegreya', 'Noto Serif', 16, 1.65, 35, 22, 32, 16, '#24211c',
  ],
  'creative-accent': [
    'Source Sans 3', 'Noto Sans', 15, 1.5, 22, 18, 20, 10, '#1f1a17',
  ],
} as const;

export function resumeContract(
  template: string, language: 'vi' | 'en', dark = false,
): ScreenContract {
  const style = RESUME_STYLES[template as keyof typeof RESUME_STYLES];
  const document = '.resume-document:not(.pagination-measurement)';
  const root = `:nth-match(${document}, 1)`;
  const title = language === 'vi' ? 'Nguyễn Thị Minh Châu' : 'Ada Lovelace';
  const grounds: Record<string, string> = {
    'classic-serif': 'rgb(21, 22, 23)', 'modern-sidebar': 'rgb(15, 23, 27)',
    'editorial-wide': 'rgb(25, 21, 18)', 'creative-accent': 'rgb(28, 20, 17)',
    'executive-band': 'rgb(19, 22, 27)',
  };
  return {
    root,
    text: `${document} :is(.resume-name,.resume-headline,.entry-title,`
      // Level wrappers may hold only graphics or an absent widget slot.
      + '.entry-subtitle,.entry-meta > span:not(.level-widget),.level-tag,'
      + '.entry-body p,.entry-body li,'
      + '.section-heading h2)',
    flow: `${document} .pagination-atomic, `
      + `${document}:not(.resume-page) .resume-section`,
    constrained: '.resume-page:not(.pagination-measurement)',
    faces: [style[0], style[1]],
    type: [root, `"${style[0]}", "${style[1]}"`, style[0]],
    paint: [root, 'background-color', dark
      ? grounds[template]!
      : template === 'editorial-wide'
        ? 'rgb(250, 248, 243)'
        : 'rgb(255, 255, 255)'],
    copy: [`${document} .resume-name`, title],
    aria: [`${document} .resume-name`, `- heading "${title}" [level=1]`],
    image: `${document} .resume-photo img`,
  };
}
