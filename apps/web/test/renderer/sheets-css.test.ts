import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const dir = resolve(import.meta.dirname, '../../app/components/resume');
const read = (name: string): string =>
  readFileSync(resolve(dir, name), 'utf8');

interface Block {
  readonly prelude: string;
  readonly body: string;
  readonly children: readonly Block[];
}

const parse = (css: string): Block[] => {
  const blocks: Block[] = [];
  let start = 0;
  let index = 0;
  while (index < css.length) {
    const char = css[index]!;
    if (char === ';' || char === '}') {
      start = index + 1;
      index += 1;
      continue;
    }
    if (char !== '{') {
      index += 1;
      continue;
    }
    let depth = 1;
    let end = index + 1;
    while (depth > 0 && end < css.length) {
      if (css[end] === '{') depth += 1;
      if (css[end] === '}') depth -= 1;
      end += 1;
    }
    const prelude = css.slice(start, index).trim();
    const body = css.slice(index + 1, end - 1);
    blocks.push({
      prelude,
      body,
      children: prelude.startsWith('@') ? parse(body) : [],
    });
    start = end;
    index = end;
  }
  return blocks;
};

const sheetsCss = read('resumeSheets.css');
const documentVue = read('ResumeDocument.vue');
const screenMargins = read('resumeScreenMargins.css');
const top = parse(sheetsCss);

const atRules = (prelude: string): Block[] =>
  top.filter((block) => block.prelude === prelude);

const rulesIn = (prelude: string): Block[] =>
  atRules(prelude).flatMap((block) => block.children);

const rule = (blocks: readonly Block[], selector: string): Block => {
  const found = blocks.filter((block) =>
    block.prelude.split(',').map((part) => part.trim()).includes(selector));
  expect(found, selector).toHaveLength(1);
  return found[0]!;
};

const declarations = (block: Block): string[] =>
  block.body.split(';').map((part) => part.trim()).filter(Boolean);

const screen = rulesIn('@media screen');
const print = rulesIn('@media print');
const narrow = rulesIn('@container (width < 480px)');
const ARTICLE = '.resume-document.resume-sheets';

const ROLES = [
  'surface',
  'surface-header',
  'surface-sidebar',
  'heading',
  'body',
  'meta',
  'accent',
  'accent-text',
  'accent-solid',
  'on-accent',
  'link',
  'rule',
  'track',
];

const SIZES = [
  'base',
  'name',
  'headline',
  'heading',
  'title',
  'subtitle',
  'body',
  'meta',
];

const screenOnly = (needle: string): void => {
  const inScreen = atRules('@media screen')
    .map((block) => block.body)
    .join('\n');
  const rest = top
    .filter((block) => block.prelude !== '@media screen')
    .map((block) => `${block.prelude}{${block.body}}`)
    .join('\n');
  expect(inScreen, needle).toContain(needle);
  expect(rest, needle).not.toContain(needle);
};

describe('sheets stylesheet wiring', () => {
  it('loads inside the first style block that print.css carries', () => {
    const imported = '@import \'./resumeSheets.css\';';
    const firstStyle = documentVue.indexOf('<style>');
    expect(documentVue.indexOf(imported)).toBeGreaterThan(firstStyle);
    expect(documentVue.indexOf(imported))
      .toBeLessThan(documentVue.indexOf('.public-resume-page {'));
    expect(documentVue.match(/<style/gu)).toHaveLength(1);
  });

  it('keeps the continuous screen margins off the sheets article', () => {
    const selectors = /([^{}]+)\{\s*padding: var\(--screen-margin-y\)/u
      .exec(screenMargins)?.[1] ?? '';
    const parts = selectors.split(',').map((part) => part.trim());
    expect(parts.length).toBeGreaterThan(0);
    for (const part of parts) expect(part).toContain(':not(.resume-sheets)');
  });

  it('guards the continuous photo restack against sheets', () => {
    const restack = [...documentVue.matchAll(
      /@container \(width < 36em\) \{([\s\S]*?)\n\}/gu,
    )].map((match) => match[1]!);
    expect(restack).toHaveLength(2);
    const photo = restack.find((body) => body.includes('data-photo-position'));
    const selectors = photo!.match(/\.resume-document[^\n{]*/gu)!;
    expect(selectors.length).toBeGreaterThan(0);
    for (const selector of selectors) {
      expect(selector).toContain(':not(.resume-page):not(.resume-sheets)');
    }
  });
});

describe('sheets geometry on screen', () => {
  it('lays the article flat on the desk at full width', () => {
    const article = declarations(rule(screen, ARTICLE));
    expect(article).toContain('display: flow-root');
    expect(article).toContain('padding: 0');
    expect(article).toContain('background: var(--sheet-desk)');
  });

  it('sizes, pads, rounds, spaces and shades each sheet', () => {
    const sheet = declarations(rule(screen, `${ARTICLE} .resume-sheet`));
    expect(sheet).toContain('width: min(100% - 20px, 46rem)');
    expect(sheet).toContain('margin: 0 auto clamp(14px, 2.5cqi, 26px)');
    expect(sheet).toContain(
      'padding: clamp(28px, 6cqi, 60px) clamp(22px, 9cqi, 84px) '
      + 'clamp(20px, 3cqi, 30px)',
    );
    expect(sheet).toContain('border-radius: clamp(14px, 1.5cqi, 18px)');
    expect(sheet).toContain('background: var(--color-surface)');
    const shadow = sheet.find((item) => item.startsWith('box-shadow:'))!;
    expect(shadow.replace(/\s+/gu, ' ')).toBe(
      'box-shadow: 0 0 0 0.5px rgb(var(--sheet-shadow) / 0.14), '
      + '0 1px 2px rgb(var(--sheet-shadow) / 0.08), '
      + '0 10px 26px -8px rgb(var(--sheet-shadow) / 0.2), '
      + '0 32px 60px -28px rgb(var(--sheet-shadow) / 0.22)',
    );
  });

  it('never hides a sheet overflow', () => {
    expect(sheetsCss).not.toMatch(/overflow(-[xy])?:\s*hidden/u);
  });

  it('makes each sheet content box an inline-size container', () => {
    expect(declarations(rule(top, `${ARTICLE} .resume-sheet-body`)))
      .toContain('container-type: inline-size');
    expect(sheetsCss).not.toMatch(/\.resume-sheet \{[^}]*container-type/u);
  });

  it('draws a CanvasText border in forced colors', () => {
    const forced = rulesIn('@media (forced-colors: active)');
    expect(declarations(rule(forced, `${ARTICLE} .resume-sheet`)))
      .toContain('border: 1px solid CanvasText');
  });

  it('zeroes the bottom margin of the last section in a sheet', () => {
    expect(declarations(rule(
      screen,
      `${ARTICLE} .resume-sheet-body > .resume-section:last-child`,
    ))).toEqual(['margin-block-end: 0']);
  });
});

describe('desk and paper', () => {
  it('sets the light desk and shadow, and the dark ones per scheme', () => {
    const light = declarations(rule(top, ARTICLE));
    expect(light).toContain('--sheet-desk: #e6dfd0');
    expect(light).toContain('--sheet-shadow: 74 54 22');
    const dark = declarations(
      rule(screen, `${ARTICLE}[data-color-scheme="dark"]`),
    );
    expect(dark).toEqual(['--sheet-desk: #141210', '--sheet-shadow: 0 0 0']);
    const system = declarations(rule(
      rulesIn('@media screen and (prefers-color-scheme: dark)'),
      `${ARTICLE}[data-color-scheme="system"]`,
    ));
    expect(system).toEqual(dark);
  });

  it('paints the page root with the desk so overscroll shows it', () => {
    expect(declarations(rule(
      screen,
      ':root:has(.public-resume-page .resume-sheets)',
    ))).toEqual(['background: #e6dfd0']);
    const darkRoot = [
      'color-scheme: dark',
      'background: #141210',
    ];
    expect(declarations(rule(
      screen,
      ':root:has(.public-resume-page .resume-sheets[data-color-scheme="dark"])',
    ))).toEqual(darkRoot);
    expect(declarations(rule(
      rulesIn('@media screen and (prefers-color-scheme: dark)'),
      ':root:has(.public-resume-page .resume-sheets'
      + '[data-color-scheme="system"])',
    ))).toEqual(darkRoot);
  });

  it('points every role at its paper role on screen only, below dark', () => {
    const selector = ':where(.resume-sheets, .resume-sheets *)'
      + '[style*="--paper-color-surface"]';
    const paper = screen.filter((block) => block.prelude === selector);
    expect(paper).toHaveLength(1);
    expect(declarations(paper[0]!)).toEqual(ROLES.map((role) =>
      `--color-${role}: var(--paper-color-${role}) !important`));
    screenOnly('--paper-color-');
  });
});

describe('type, measure and rows on screen', () => {
  it('floors the type through the sheet sizes on screen only', () => {
    expect(declarations(rule(screen, ARTICLE)).filter((item) =>
      item.startsWith('--fs-'))).toEqual(SIZES.map((size) =>
      `--fs-${size}: var(--sheet-fs-${size}) !important`));
    screenOnly('--sheet-fs-');
  });

  it('limits entry bodies, profile text and the headline to 62ch', () => {
    expect(declarations(rule(screen, `${ARTICLE} .entry-body`)))
      .toContain('max-width: 62ch');
    const headline = declarations(rule(screen, `${ARTICLE} .resume-headline`));
    expect(headline).toContain('max-width: 62ch');
    expect(headline).toContain('margin-inline: var(--headline-margin-inline)');
    screenOnly('62ch');
  });

  it('gives each contact row a 44 px target on screen only', () => {
    expect(declarations(rule(screen, `${ARTICLE} .contact-chip--sheets`)))
      .toEqual(['min-height: 44px']);
    screenOnly('44px');
  });
});

describe('contact rows', () => {
  const ROW = `${ARTICLE} .contact-chip--sheets`;

  it('stacks rows start-aligned without a shared label column', () => {
    const rows = declarations(rule(top, `${ARTICLE} .details-rows`));
    expect(rows).toContain('display: grid');
    expect(rows).toContain('text-align: start');
    expect(rows).toContain('justify-items: stretch');
    expect(rows.join(';')).not.toContain('grid-template-columns');
    expect(sheetsCss).not.toContain('subgrid');
  });

  it('puts the label at the start and the value at the end of a row', () => {
    const row = declarations(rule(top, ROW));
    expect(row).toContain('display: flex');
    expect(row).toContain('flex-wrap: wrap');
    expect(row).toContain('justify-content: space-between');
    expect(row).toContain('align-items: baseline');
    expect(row).toContain('align-content: center');
    expect(row).toContain('position: relative');
    expect(row).toContain('overflow-x: clip');
  });

  it('draws a dotted leader border that ends where the value starts', () => {
    const leader = declarations(rule(top, `${ROW} .contact-value::before`));
    expect(leader).toContain('content: ""');
    expect(leader).toContain('border-block-end: 1px dotted var(--color-rule)');
    expect(leader).toContain('width: 100vw');
    expect(leader).toContain('margin-inline-start: -100vw');
    const label = declarations(rule(top, `${ROW} .contact-label`));
    expect(label).toContain('background: var(--color-surface)');
    expect(label).toContain('z-index: 1');
    expect(declarations(rule(top, `${ROW} .contact-label::after`)))
      .toEqual(['content: none']);
  });

  it('wraps a long value below the label', () => {
    const value = declarations(rule(top, `${ROW} .contact-value`));
    expect(value).toContain('overflow-wrap: anywhere');
    expect(value).toContain('min-width: 0');
  });

  it('stretches the anchor hit area over the row and rings the row', () => {
    const hit = declarations(rule(top, `${ROW} a::after`));
    expect(hit).toContain('content: ""');
    expect(hit).toContain('position: absolute');
    expect(hit).toContain('inset: 0');
    expect(hit).toContain('z-index: 2');
    const supports = rulesIn('@supports selector(:has(a))');
    expect(declarations(rule(supports, `${ROW}:has(a:focus-visible)`)))
      .toEqual([
        'outline: 2px solid var(--color-link)',
        'outline-offset: 2px',
      ]);
    expect(declarations(rule(supports, `${ROW} a:focus-visible`)))
      .toEqual(['outline: none']);
  });
});

describe('narrow content box', () => {
  it('keeps a side photo beside the text at 66 px with a 14 px gap', () => {
    expect(declarations(rule(
      narrow,
      `${ARTICLE} .resume-header[data-photo-position]`,
    ))).toEqual(['--photo-size: 66px', '--header-photo-gap-side: 14px']);
    expect(sheetsCss).not.toMatch(/grid-template-columns: minmax\(0, 1fr\);/u);
  });

  it('start-aligns justified body text', () => {
    const body = rule(narrow, `${ARTICLE} .entry-body p`);
    expect(body.prelude).toContain(`${ARTICLE} .entry-body li`);
    expect(declarations(body)).toEqual([
      'text-align: var(--body-align-narrow)',
      'hyphens: manual',
    ]);
    expect(top.some((block) => block.prelude === '@container (width < 480px)'))
      .toBe(true);
  });
});

describe('folio', () => {
  it('sets the folio row at the sheet foot on screen', () => {
    const folio = declarations(rule(screen, `${ARTICLE} .resume-folio`));
    expect(folio).toContain('display: flex');
    expect(folio).toContain('justify-content: space-between');
    expect(folio).toContain('margin-block-start: clamp(30px, 4cqi, 44px)');
    expect(folio).toContain('color: var(--color-meta)');
    expect(folio).toContain('font-size: 0.7em');
    expect(folio).toContain('text-transform: uppercase');
    expect(folio.some((item) => item.startsWith('letter-spacing:'))).toBe(true);
  });

  it.each([
    ['::before', 'data-folio-name'],
    ['::after', 'data-folio-page'],
  ])('draws %s from %s with empty alternative text', (pseudo, attribute) => {
    expect(declarations(rule(screen, `${ARTICLE} .resume-folio${pseudo}`)))
      .toEqual([
        `content: attr(${attribute})`,
        `content: attr(${attribute}) / ""`,
      ]);
  });
});

describe('flattened print', () => {
  it('drops folios and keeps the page margins as article padding', () => {
    expect(declarations(rule(print, `${ARTICLE} .resume-folio`)))
      .toEqual(['display: none']);
    const article = declarations(rule(print, ARTICLE));
    expect(article).toContain(
      'padding: var(--page-margin-y) var(--page-margin-x)',
    );
    expect(article).toContain('background: var(--color-surface)');
  });

  it('forces no breaks and keeps desk, shadow, radius and gap off', () => {
    expect(sheetsCss).not.toMatch(/break-(before|after|inside)/u);
    for (const needle of ['--sheet-desk)', 'box-shadow', 'border-radius',
      'cqi']) {
      const printed = atRules('@media print').map((block) => block.body)
        .join('\n');
      expect(printed, needle).not.toContain(needle);
    }
    screenOnly('box-shadow');
    screenOnly('border-radius');
    screenOnly('var(--sheet-desk)');
  });
});
