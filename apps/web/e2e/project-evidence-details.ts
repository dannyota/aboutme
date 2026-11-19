import { expect, type Page } from '@playwright/test';

import { measureTextContainment } from './display-assertions';
import { PROJECT_COLORS } from './project-page-details';
import { expectStyleRules } from './screen-expectations';

type Lng = 'vi' | 'en';
type Scheme = 'light' | 'dark';

const FAMILY = '"Be Vietnam Pro", Inter, system-ui, sans-serif';

export const EVIDENCE_CONTENT = 'Project evidence content.';
export const EVIDENCE_LINKS = 'Project evidence links.';
export const EVIDENCE_PLACEMENT = 'Project evidence placement.';
export const EVIDENCE_COUNTS = 'Project evidence counts.';
export const EVIDENCE_COLORS = 'Project evidence colors.';
export const EVIDENCE_TYPOGRAPHY = 'Project evidence typography.';
export const EVIDENCE_SHAPE = 'Project evidence shape.';
export const EVIDENCE_ICONS = 'Project evidence icons.';
export const EVIDENCE_ARIA = 'Project evidence ARIA.';
export const EVIDENCE_CONTAINMENT = 'Project evidence containment.';

const SECTION: Record<Lng, string> = {
  vi: 'Bằng chứng trên GitHub', en: 'GitHub evidence',
};

interface EvidenceRowText {
  readonly label: string | null;
  readonly title: string;
  readonly href: string;
  readonly meta: string;
}

const PAYMENTS = 'https://github.com/example/payments';

const ROWS: Record<Lng, readonly EvidenceRowText[]> = {
  vi: [
    { label: 'Chủ sở hữu kho mã này', title: 'example/payments',
      href: PAYMENTS, meta: '03/2022 · kiểm tra 11/2026' },
    { label: 'Tác giả của pull request đã được merge',
      title: 'Thêm cơ chế thử lại có giới hạn cho bước đối soát và hàng đợi '
        + 'thư lỗi cho đối_soát_thất_bại_ở_mọi_đối_tác_thanh_toán_trong_khu_'
        + 'vực',
      href: `${PAYMENTS}/pull/128`,
      meta: 'example/payments · 06/2024 · kiểm tra 11/2026' },
    { label: 'Tác giả của bản phát hành này', title: 'v2.4.0',
      href: `${PAYMENTS}/releases/tag/v2.4.0`,
      meta: 'example/payments · 06/2024 · kiểm tra 11/2026' },
    { label: null, title: 'Sửa lỗi tranh chấp trong bộ nhớ đệm sổ cái',
      href: 'https://github.com/upstream/ledger/pull/7',
      meta: 'upstream/ledger' },
  ],
  en: [
    { label: 'Owner of this repository', title: 'example/payments',
      href: PAYMENTS, meta: 'Mar 2022 · checked Nov 2026' },
    { label: 'Author of a merged pull request',
      title: 'Add bounded settlement retries with a dead-letter queue for '
        + 'reconciliation_failures_across_every_regional_payment_partner',
      href: `${PAYMENTS}/pull/128`,
      meta: 'example/payments · Jun 2024 · checked Nov 2026' },
    { label: 'Author of this release', title: 'v2.4.0',
      href: `${PAYMENTS}/releases/tag/v2.4.0`,
      meta: 'example/payments · Jun 2024 · checked Nov 2026' },
    { label: null, title: 'Fix a race in the ledger cache',
      href: 'https://github.com/upstream/ledger/pull/7',
      meta: 'upstream/ledger' },
  ],
};

export async function expectEvidenceContent(
  page: Page, lng: Lng,
): Promise<void> {
  await expect.poll(() => page.locator('.project-evidence-item')
    .evaluateAll((items) => items.map((item) => {
      const text = (selector: string) => item.querySelector(selector)
        ?.textContent?.normalize('NFC') ?? null;
      return {
        label: text('.project-evidence-label'),
        title: text('a.project-evidence') ?? '',
        meta: text('.project-evidence-meta') ?? '',
      };
    })), EVIDENCE_CONTENT).toEqual(ROWS[lng].map(({ label, title, meta }) =>
    ({ label, title, meta })));
  await expect(page.locator('#project-evidence-title'), EVIDENCE_CONTENT)
    .toHaveText(SECTION[lng]);
}

export async function expectEvidenceLinks(
  page: Page, lng: Lng,
): Promise<void> {
  await expect.poll(() => page.locator('a.project-evidence')
    .evaluateAll((anchors) => anchors.map((anchor) => ({
      attributes: [...anchor.attributes].map(({ name, value }) =>
        `${name}=${value}`).sort(),
      text: anchor.textContent,
      children: anchor.children.length,
    }))), EVIDENCE_LINKS).toEqual(ROWS[lng].map(({ title, href }) => ({
    attributes: ['class=project-evidence', `href=${href}`,
      'rel=noopener noreferrer nofollow'],
    text: title,
    children: 0,
  })));
}

export async function expectEvidencePlacement(page: Page): Promise<void> {
  await expect.poll(() => page.evaluate(() => {
    const section = document.querySelector('.project-evidence-section');
    const about = document.querySelector('.project-about');
    const more = document.querySelector('.project-more');
    const text = document.querySelector('.project-text');
    const facts = document.querySelector('.project-facts');
    if (!section || !about || !more || !text || !facts) return false;
    const box = (element: Element) => element.getBoundingClientRect();
    const rem = parseFloat(getComputedStyle(document.documentElement)
      .fontSize);
    const order = section.parentElement === text
      && section.previousElementSibling === about
      && section.nextElementSibling === more
      && box(section).top >= box(about).bottom - 1
      && box(more).top >= box(section).bottom - 1;
    const inText = Math.abs(box(section).left - box(text).left) <= 1
      && Math.abs(box(section).right - box(text).right) <= 1;
    const beside = innerWidth >= 56 * rem
      ? box(facts).left >= box(section).right - 1
      && box(facts).top <= box(section).top
      : box(section).top >= box(facts).bottom - 1;
    return order && inText && beside;
  }), EVIDENCE_PLACEMENT).toBe(true);
}

export async function expectEvidenceCounts(page: Page): Promise<void> {
  const counts: [string, number][] = [
    ['.project-evidence-section', 1], ['.project-evidence-list', 1],
    ['.project-evidence-item', 4], ['.project-evidence-label', 3],
    ['.project-evidence-heading', 4], ['.project-evidence-meta', 4],
    ['a.project-evidence', 4], ['.project-evidence-item svg', 7],
    ['.project-facts .project-evidence-section', 0],
  ];
  for (const [selector, count] of counts) {
    await expect(page.locator(selector), EVIDENCE_COUNTS).toHaveCount(count);
  }
}

export async function expectEvidenceColors(
  page: Page, scheme: Scheme,
): Promise<void> {
  const color = PROJECT_COLORS[scheme];
  const checks: [string, string, string][] = [
    ['.project-evidence-list', 'background-color', color.card],
    ['.project-evidence-list', 'border-top-color', color.border],
    ['.project-evidence-list', 'border-left-color', color.border],
    ['.project-evidence-item + .project-evidence-item', 'border-top-color',
      color.hairline],
    ['#project-evidence-title', 'color', color.ink],
    ['.project-evidence-label', 'color', color.ink],
    ['.project-evidence-label svg', 'color', color.ink],
    ['.project-evidence-heading svg', 'color', color.muted],
    ['a.project-evidence', 'color', color.link],
    ['.project-evidence-meta', 'color', color.muted],
  ];
  for (const [selector, property, value] of checks) {
    const nodes = page.locator(selector);
    expect(await nodes.count(), EVIDENCE_COLORS).toBeGreaterThan(0);
    for (const node of await nodes.all()) {
      await expect(node, EVIDENCE_COLORS).toHaveCSS(property, value);
    }
  }
}

export async function expectEvidenceTypography(page: Page): Promise<void> {
  await expectStyleRules(page, [
    { selector: 'a.project-evidence', message: EVIDENCE_TYPOGRAPHY,
      css: { 'font-family': FAMILY, 'font-size': '16px',
        'line-height': '24px', 'font-weight': '600' } },
    { selector: '.project-evidence-label', message: EVIDENCE_TYPOGRAPHY,
      css: { 'font-family': FAMILY, 'font-size': '14px',
        'line-height': '20px', 'font-weight': '500' } },
    { selector: '.project-evidence-meta', message: EVIDENCE_TYPOGRAPHY,
      css: { 'font-family': FAMILY, 'font-size': '14px',
        'line-height': '20px', 'font-weight': '400' } },
    { selector: '#project-evidence-title', message: EVIDENCE_TYPOGRAPHY,
      css: { 'font-family': FAMILY, 'font-size': '20px',
        'line-height': '26px', 'font-weight': '600' } },
  ]);
}

export async function expectEvidenceShape(page: Page): Promise<void> {
  const width = page.viewportSize()!.width;
  const card = width >= 1024 ? '28px' : width >= 768 ? '26px' : '22px';
  const [block, inline] = width >= 640 ? ['20px', '24px'] : ['16px', '16px'];
  const list = page.locator('.project-evidence-list');
  for (const [property, value] of [
    ['border-top-left-radius', card], ['border-bottom-right-radius', card],
    ['border-top-width', '1px'], ['border-left-style', 'solid'],
  ] as const) {
    await expect(list, EVIDENCE_SHAPE).toHaveCSS(property, value);
  }
  for (const item of await page.locator('.project-evidence-item').all()) {
    await expect(item, EVIDENCE_SHAPE).toHaveCSS('padding-top', block);
    await expect(item, EVIDENCE_SHAPE).toHaveCSS('padding-left', inline);
  }
  await expect.poll(() => page.locator('.project-evidence-item')
    .evaluateAll((items) => items.every((item) => {
      const meta = item.querySelector('.project-evidence-meta');
      const anchor = item.querySelector('a.project-evidence')!;
      const left = anchor.getBoundingClientRect().left;
      return meta === null
        || Math.abs(meta.getBoundingClientRect().left
          + parseFloat(getComputedStyle(meta).paddingLeft) - left) <= 1;
    })), EVIDENCE_SHAPE).toBe(true);
}

const CHECK = ['pathM3.85 8.', 'pathm16 9-5.'];
const KIND_PARTS: Record<string, readonly string[]> = {
  repository: ['pathM15 6a9 ', 'circle', 'circle'],
  pullRequest: ['circle', 'circle', 'pathM13 6h3a', 'line'],
  release: ['pathM12.586 ', 'circle'],
};
const ICON_ROWS: readonly (readonly [string, boolean])[] = [
  ['repository', true], ['pullRequest', true], ['release', true],
  ['pullRequest', false],
];

const icon = (parts: readonly string[]) => ({
  hidden: 'true', focusable: 'false', box: [16, 16], parts: [...parts],
});

export async function expectEvidenceIcons(page: Page): Promise<void> {
  await expect.poll(() => page.locator('.project-evidence-item')
    .evaluateAll((items) => items.map((item) => {
      const shape = (svg: SVGSVGElement | null) => {
        if (svg === null) return null;
        const box = svg.getBoundingClientRect();
        return {
          hidden: svg.getAttribute('aria-hidden'),
          focusable: svg.getAttribute('focusable'),
          box: [Math.round(box.width), Math.round(box.height)],
          parts: [...svg.querySelectorAll('g > *')].map((part) =>
            part.localName + (part.getAttribute('d')?.slice(0, 8) ?? '')),
        };
      };
      const heading = item.querySelector('.project-evidence-heading')!;
      return {
        label: shape(item.querySelector('.project-evidence-label > svg')),
        kind: shape(heading.querySelector(':scope > svg')),
        before: heading.firstElementChild?.localName ?? null,
      };
    })), EVIDENCE_ICONS).toEqual(ICON_ROWS.map(([kind, confirmed]) => ({
    label: confirmed ? icon(CHECK) : null,
    kind: icon(KIND_PARTS[kind]!),
    before: 'svg',
  })));
}

export async function expectEvidenceAria(
  page: Page, lng: Lng,
): Promise<void> {
  const region = page.getByRole('region', { name: SECTION[lng] });
  await expect(region, EVIDENCE_ARIA).toHaveCount(1);
  await expect(region, EVIDENCE_ARIA).toMatchAriaSnapshot(
    `- region "${SECTION[lng]}":\n`
    + `  - heading "${SECTION[lng]}" [level=2]\n`
    + '  - list:\n'
    + ROWS[lng].map(({ label, title, href, meta }) => [
      '    - listitem:',
      ...(label === null ? [] : [`      - paragraph: ${label}`]),
      '      - paragraph:',
      `        - link ${JSON.stringify(title)}:`,
      `          - /url: ${href}`,
      `      - paragraph: ${meta}`,
    ].join('\n')).join('\n'),
  );
  await expect(region.getByRole('listitem'), EVIDENCE_ARIA).toHaveCount(4);
  await expect(region.getByRole('link'), EVIDENCE_ARIA)
    .toHaveText(ROWS[lng].map(({ title }) => title));
  await expect(region.getByRole('img'), EVIDENCE_ARIA).toHaveCount(0);
}

export async function expectEvidenceContainment(page: Page): Promise<void> {
  await expect.poll(() => measureTextContainment(
    page.locator('.project-evidence-section')), EVIDENCE_CONTAINMENT)
    .toEqual({ visible: true, contained: true });
  await expect.poll(() => page.evaluate(() => {
    const root = document.documentElement;
    const list = document.querySelector('.project-evidence-list')!
      .getBoundingClientRect();
    return root.scrollWidth <= root.clientWidth + 1
      && [...document.querySelectorAll('a.project-evidence')].every(
        (anchor) => anchor.getBoundingClientRect().right <= list.right + 1);
  }), EVIDENCE_CONTAINMENT).toBe(true);
}

export async function expectProjectEvidence(
  page: Page, lng: Lng, scheme: Scheme,
): Promise<void> {
  await expectEvidenceContent(page, lng);
  await expectEvidenceLinks(page, lng);
  await expectEvidencePlacement(page);
  await expectEvidenceCounts(page);
  await expectEvidenceColors(page, scheme);
  await expectEvidenceTypography(page);
  await expectEvidenceShape(page);
  await expectEvidenceIcons(page);
  await expectEvidenceAria(page, lng);
  await expectEvidenceContainment(page);
}

export type EvidenceDefect
  = | 'reorder' | 'duplicate' | 'plainLabel' | 'intoFacts' | 'removeIcon';

export async function mutateEvidence(
  page: Page, defect: EvidenceDefect,
): Promise<() => Promise<void>> {
  const before = await page.locator('.project-evidence-item')
    .evaluateAll((items) => items.map((item) => item.outerHTML).join(''));
  const facts = await page.locator('.project-facts')
    .evaluate((element) => element.childElementCount);
  await page.evaluate((kind) => {
    const list = document.querySelector('.project-evidence-list')!;
    const items = [...list.children];
    const holder = window as unknown as { evidenceRestore?: () => void };
    if (kind === 'reorder') {
      const last = items.at(-1)!;
      list.prepend(last);
      holder.evidenceRestore = () => list.append(last);
    } else if (kind === 'duplicate') {
      const copy = items[0]!.cloneNode(true) as Element;
      list.append(copy);
      holder.evidenceRestore = () => copy.remove();
    } else if (kind === 'plainLabel') {
      const label = items[0]!.querySelector('.project-evidence-label')!
        .cloneNode(true) as Element;
      items.at(-1)!.prepend(label);
      holder.evidenceRestore = () => label.remove();
    } else if (kind === 'intoFacts') {
      const section = document.querySelector('.project-evidence-section')!;
      const next = section.nextElementSibling!;
      document.querySelector('.project-facts')!.append(section);
      holder.evidenceRestore = () => next.before(section);
    } else {
      const svg = items[0]!.querySelector('.project-evidence-heading > svg')!;
      const heading = svg.parentElement!;
      svg.remove();
      holder.evidenceRestore = () => heading.prepend(svg);
    }
  }, defect);
  const after = await page.locator('.project-evidence-item')
    .evaluateAll((items) => items.map((item) => item.outerHTML).join(''));
  const moved = await page.locator('.project-facts')
    .evaluate((element) => element.childElementCount);
  expect(after !== before || moved !== facts, 'Evidence defect took effect.')
    .toBe(true);
  return async () => {
    await page.evaluate(() => {
      (window as unknown as { evidenceRestore: () => void })
        .evidenceRestore();
    });
  };
}
