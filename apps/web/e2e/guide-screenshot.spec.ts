import { expect, test, type Locator, type Page } from '@playwright/test';

import {
  expectScreen, expectScreenClass, measureVerticalFlow,
} from './display-assertions';
import {
  applyStyleMutation, mutateAttribute, mutateText, proveMutation,
  screenMutationTests,
} from './style-mutation';
import {
  expectGuideCodeScroll, expectGuideDetails, expectGuideIcons,
  expectGuideInstructions,
  expectGuideLists, GUIDE_TYPE,
} from './guide-details';
import { styleProofs } from './screen-style-proofs';
import { expectGuideProse } from './guide-prose';
import { screenRegressionTests } from './screen-regressions';
import {
  chromeScreenContract,
  CHROME_PIXEL_TOLERANCE,
  denyExternalRequests,
  mockSignedInSession,
  mockSignedOutSession,
  verifyScreenshot,
  waitForImages,
} from './support';

const GUIDE_BASE_URL = 'http://127.0.0.1:20090';

async function assertGuideFlow(sections: Locator): Promise<void> {
  await expect.poll(() => measureVerticalFlow(sections),
    'Guide section separation.')
    .toBe(true);
}

interface GuideCell {
  readonly locale: 'en' | 'vi';
  readonly name: string;
  readonly theme: 'light' | 'dark';
  readonly width: number;
}

// The MCP guide page (/guide/mcp), both locales and themes, at phone and
// desktop measures (docs/design/mcp-guide.md, "Layout").
const GUIDE_CELLS: readonly GuideCell[] = [
  { locale: 'vi', name: 'guide--light--390.png', theme: 'light', width: 390 },
  { locale: 'vi', name: 'guide--light--1280.png', theme: 'light', width: 1280 },
  { locale: 'vi', name: 'guide--dark--390.png', theme: 'dark', width: 390 },
  { locale: 'vi', name: 'guide--dark--1280.png', theme: 'dark', width: 1280 },
  {
    locale: 'en',
    name: 'guide--en--light--390.png',
    theme: 'light',
    width: 390,
  },
  {
    locale: 'en',
    name: 'guide--en--light--1280.png',
    theme: 'light',
    width: 1280,
  },
  {
    locale: 'en',
    name: 'guide--en--dark--390.png',
    theme: 'dark',
    width: 390,
  },
  {
    locale: 'en',
    name: 'guide--en--dark--1280.png',
    theme: 'dark',
    width: 1280,
  },
];

test.describe('guide page pixel baselines', () => {
  for (const cell of GUIDE_CELLS) {
    test.describe(`display locale ${cell.locale}`, () => {
      test.use({ locale: cell.locale === 'vi' ? 'vi-VN' : 'en-US' });
      test(cell.name, async ({ page }, testInfo) => {
        await page.setViewportSize({ width: cell.width, height: 900 });
        await page.context().addCookies([
          { name: 'aboutme-locale', value: cell.locale, url: GUIDE_BASE_URL },
          { name: 'aboutme-theme', value: cell.theme, url: GUIDE_BASE_URL },
        ]);
        await mockSignedOutSession(page);
        const external = await denyExternalRequests(page);

        const response = await page.goto('/guide/mcp');
        expect(response?.status()).toBe(200);
        // Chrome transitions must not be mid-flight at capture (chrome.spec.ts
        // carries the same reasoning).
        await page.addStyleTag({
          content: '*, *::before, *::after { transition: none !important; '
            + 'animation: none !important; }',
        });
        await expect(page.locator('[data-testid="guide-mcp-page"]'))
          .toBeVisible();
        if (cell.locale === 'en') {
          await expect(page.getByRole('heading', {
            level: 1,
            name: 'Connect your AI assistant to aboutme.vn',
          })).toBeVisible();
        }
        await page.evaluate(() => document.fonts.ready);
        await waitForImages(page);

        await assertGuide(page, cell.locale, cell.theme, cell.width);
        expect(external).toEqual([]);

        await verifyScreenshot(
          page,
          cell.name,
          testInfo,
          undefined,
          CHROME_PIXEL_TOLERANCE,
        );
      });
    });
  }
});

test('style mutations disable motion and restore inline styles', async ({
  page,
}) => {
  await page.setContent(`
    <style>
      @keyframes fade { from { opacity: 0.5; } to { opacity: 0.5; } }
    </style>
    <div id="target" style="transition: all 60s !important;
      animation: fade 60s infinite; transform: translateY(0px)">Target</div>
  `);
  const target = page.locator('#target');
  await expect(target).toHaveCSS('transform', 'matrix(1, 0, 0, 1, 0, 0)');
  const transitioning = await target.evaluate((element) => {
    (element as HTMLElement).style.setProperty(
      'transform', 'translateY(36px)', 'important',
    );
    return getComputedStyle(element).transform;
  });
  expect(transitioning).not.toBe('matrix(1, 0, 0, 1, 0, 36)');
  const original = await target.getAttribute('style');
  const restore = await applyStyleMutation(target, {
    transform: ['translateY(72px)', 'matrix(1, 0, 0, 1, 0, 72)'],
    opacity: '0',
  });
  await expect(target).toHaveCSS('transition-property', 'none');
  await expect(target).toHaveCSS('animation-name', 'none');
  await expect(target).toHaveCSS('transform', 'matrix(1, 0, 0, 1, 0, 72)');
  await expect(target).toHaveCSS('opacity', '0');
  await restore();
  await expect(target).toHaveAttribute('style', original!);
});

test('style mutations verify and restore pseudo-elements', async ({ page }) => {
  await page.setContent('<div id="target">Target</div>');
  const target = page.locator('#target');
  const original = await target.evaluate((element) =>
    getComputedStyle(element, '::after').content);
  const restore = await applyStyleMutation(target, {
    'content': '"mutated"', 'background-color': 'rgb(255, 0, 0)',
  }, '::after');
  expect(await target.evaluate((element) =>
    getComputedStyle(element, '::after').content)).toBe('"mutated"');
  await restore();
  expect(await target.evaluate((element) =>
    getComputedStyle(element, '::after').content)).toBe(original);
  await expect(target).not.toHaveAttribute('data-e2e-style-mutation');
});

test('style mutations reject unchanged string expectations', async ({
  page,
}) => {
  await page.setContent('<div id="target" style="opacity: 1">Target</div>');
  const target = page.locator('#target');
  await expect(target).toHaveCSS('opacity', '1');
  await expect(applyStyleMutation(target, { opacity: '1' }))
    .rejects.toThrow('Style mutation changed computed values.');
  await expect(target).toHaveAttribute('style', 'opacity: 1');
});

test('guide flow detects overlapping sections', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await mockSignedOutSession(page);
  const response = await page.goto('/guide/mcp');
  expect(response?.status()).toBe(200);
  await page.evaluate(() => document.fonts.ready);
  const sections = page.getByTestId('guide-mcp-page')
    .locator(':scope > header, :scope > section');
  const assertFlow = () => assertGuideFlow(sections);
  await assertFlow();
  const header = page.getByTestId('guide-header');
  const restore = await applyStyleMutation(header, {
    transform: ['translateY(36px)', 'matrix(1, 0, 0, 1, 0, 36)'],
  });
  const gap = await header.evaluate((element) =>
    element.parentElement!.querySelector(':scope > section')!
      .getBoundingClientRect().top - element.getBoundingClientRect().bottom);
  expect(gap).toBeLessThan(0);
  await expect(assertFlow()).rejects.toThrow('Guide section separation.');
  await restore();
  await assertFlow();
});

/**
 * Describes the shell and its direct children, the nav's, and the right-side
 * group's: tag, test id or text, computed display, and width in pixels.
 */
async function describeHeader(page: Page): Promise<string> {
  return page.evaluate(() => {
    const shell = document.querySelector<HTMLElement>(
      '[data-testid="app-shell"]',
    )!;
    const line = (element: Element): string => {
      const box = element.getBoundingClientRect();
      const name = element.getAttribute('data-testid')
        ?? (element.textContent ?? '').trim().replace(/\s+/gu, ' ')
          .slice(0, 24);
      const style = getComputedStyle(element);
      return `${element.tagName.toLowerCase()} "${name}" `
        + `display=${style.display} width=${box.width.toFixed(1)}`;
    };
    const rows = [
      `shell clientWidth=${shell.clientWidth} `
      + `scrollWidth=${shell.scrollWidth} `
      + `viewport=${document.documentElement.clientWidth}`,
    ];
    const walk = (parent: Element, label: string): void => {
      for (const child of parent.children) {
        rows.push(`${label}${line(child)}`);
      }
    };
    walk(shell, 'shell > ');
    const nav = shell.querySelector('nav');
    if (nav !== null) {
      walk(nav, 'nav > ');
    }
    const right = shell.querySelector(':scope > div');
    if (right !== null) {
      walk(right, 'right > ');
    }
    return `\n${rows.join('\n')}`;
  });
}

// The header must not overflow at 704, 768, 896, 1024, or 1152px in either
// language, signed in or out, and the Connect AI link shows exactly where
// docs/design/mcp-guide.md, "Navigation", puts it: signed out, from 44rem
// (704px); signed in, from 64rem (1024px), because the signed-in bar also
// carries Resumes, Views, and Settings. /guide/mcp carries both states,
// since the page reads no API and renders the same content either way.
const HEADER_WIDTHS = [704, 768, 896, 1024, 1152] as const;
const GUIDE_LINK_VISIBLE_AT: Record<'in' | 'out', readonly number[]> = {
  out: [704, 768, 896, 1024, 1152],
  in: [1024, 1152],
};

for (const locale of ['vi', 'en'] as const) {
  test.describe(`interface ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
    for (const session of ['out', 'in'] as const) {
      for (const width of HEADER_WIDTHS) {
        const title = `guide header fits ${width}px signed ${session} `
          + `(${locale})`;
        test(title, async ({ page }) => {
          await page.setViewportSize({ width, height: 900 });
          await page.context().addCookies([
            { name: 'aboutme-locale', value: locale, url: GUIDE_BASE_URL },
          ]);
          if (session === 'in') {
            await mockSignedInSession(page);
          } else {
            await mockSignedOutSession(page);
          }

          const response = await page.goto('/guide/mcp');
          expect(response?.status()).toBe(200);
          const shell = page.locator('[data-testid="app-shell"]');
          await expect(shell).toBeVisible();
          if (session === 'in') {
            // SSR renders the signed-out shell before hydration's own
            // GET /api/v1/me settles, so wait for the signed-in-only account
            // menu before measuring (support.ts mockSignedInSession).
            await expect(page.getByTestId('account-menu')).toBeVisible();
          }

          const overflow = await shell.evaluate((element) =>
            element.scrollWidth - element.clientWidth);
          // A failure lists every part of the header with its width, so the
          // message names what overflows without a trace artifact.
          expect(
            overflow,
            overflow > 0 ? await describeHeader(page) : '',
          ).toBeLessThanOrEqual(0);

          // The logo link never shrinks away, even in the compact header.
          const logo = await page.locator('[data-testid="app-shell"] > a')
            .first().boundingBox();
          expect(logo?.width ?? 0).toBeGreaterThan(0);

          // The Open source link shows signed out from 72rem (1152px), where
          // the full header fits with it
          // (docs/design/ui/landing-and-library.md,
          // Navigation). The signed-in header never carries it.
          const openSource = page.getByTestId('app-shell-open-source');
          if (session === 'out' && width >= 1152) {
            await expect(openSource).toBeVisible();
          } else {
            await expect(openSource).toBeHidden();
          }

          const guideLink = page.locator('nav a[href="/guide/mcp"]');
          await expect(guideLink).toHaveCount(1);
          if (GUIDE_LINK_VISIBLE_AT[session].includes(width)) {
            await expect(guideLink).toBeVisible();
          } else {
            await expect(guideLink).toBeHidden();
          }
        });
      }
    }
  });
}

function guideContract(locale: 'vi' | 'en', theme: 'light' | 'dark') {
  return chromeScreenContract('[data-testid="guide-mcp-page"]',
    '[data-testid="guide-mcp-page"] :is(h1,h2,h3,p,dt,dd,li)'
    + ':not(.sr-only):not(:has(p,pre,ol,ul))',
    '[data-testid="guide-mcp-page"] > header, '
    + '[data-testid="guide-mcp-page"] > section',
    '[data-testid="guide-header"] h1', locale === 'vi'
      ? 'Kết nối trợ lý AI với aboutme.vn'
      : 'Connect your AI assistant to aboutme.vn', theme);
}

async function assertGuide(
  page: Page, locale: 'vi' | 'en', theme: 'light' | 'dark', width: number,
): Promise<void> {
  const guide = page.getByTestId('guide-mcp-page');
  const title = locale === 'vi'
    ? 'Kết nối trợ lý AI với aboutme.vn'
    : 'Connect your AI assistant to aboutme.vn';
  const headings = locale === 'vi'
    ? [
        'Trợ lý AI làm được gì', 'Kết nối Claude', 'Thử một yêu cầu',
        'Quyền riêng tư và quyền kiểm soát', 'Ứng dụng khác',
        'Khắc phục sự cố',
      ]
    : [
        'What your assistant can do', 'Connect Claude', 'Try a request',
        'Privacy and control', 'Other apps', 'Troubleshooting',
      ];
  await expect(guide.getByRole('heading', { level: 1 }))
    .toHaveText(title);
  await expect(page.getByTestId('guide-header')).toMatchAriaSnapshot(
    `- heading "${title}" [level=1]`,
  );
  await expect(guide.getByRole('heading', { level: 2 }))
    .toHaveText(headings);
  await expect(page.getByTestId('guide-can').locator('li')).toHaveCount(5);
  await expect(page.getByTestId('guide-cannot').locator('li'))
    .toHaveCount(5);
  await expect(page.getByTestId('guide-claude-web-disk')).toHaveCount(6);
  await expect(page.getByTestId('guide-troubleshooting').locator('dt'))
    .toHaveCount(6);
  await assertGuideFlow(
    guide.locator(':scope > header, :scope > section'),
  );

  await expectScreen(page, guideContract(locale, theme));
  await expectGuideDetails(page, locale, theme);
  expect(await page.evaluate(() => document.documentElement.scrollWidth
    - document.documentElement.clientWidth)).toBe(0);
  await expect.poll(() => guide.evaluate((element, viewport) => {
    const box = element.getBoundingClientRect();
    const css = getComputedStyle(element);
    const measure = Math.min(viewport, 960);
    const inset = viewport < 672 ? 16 : 24;
    return Math.abs(box.width - measure) <= 1
      && Math.abs(box.left - (viewport - measure) / 2) <= 2
      && Math.abs(parseFloat(css.paddingLeft) - inset) <= 1;
  }, width), 'Guide column geometry.').toBe(true);
  await expect.poll(() => page.getByTestId('guide-can-cannot').evaluate(
    (element, stacked) => {
      const can = element.querySelector('[data-testid="guide-can"]')!
        .getBoundingClientRect();
      const cannot = element.querySelector('[data-testid="guide-cannot"]')!
        .getBoundingClientRect();
      return stacked
        ? cannot.top >= can.bottom - 1
        : Math.abs(cannot.left - can.right - 32) <= 1
          && Math.abs(can.top - cannot.top) <= 1;
    }, width < 672), 'Guide capability layout.').toBe(true);
  for (const [id, icon, color] of [
    ['guide-can', '.lucide-check', theme === 'light'
      ? 'rgb(35, 57, 154)'
      : 'rgb(163, 182, 245)'],
    ['guide-cannot', '.lucide-minus', theme === 'light'
      ? 'rgb(92, 97, 120)'
      : 'rgb(165, 171, 191)'],
  ] as const) {
    const icons = page.getByTestId(id).locator(icon);
    await expect(icons, 'Guide instruction icons.').toHaveCount(5);
    for (const glyph of await icons.all()) {
      await expect(glyph, 'Guide instruction icons.')
        .toHaveCSS('width', '16px');
      await expect(glyph, 'Guide instruction icons.')
        .toHaveCSS('height', '16px');
      await expect(glyph, 'Guide instruction icons.').toHaveCSS('color', color);
    }
  }
  const header = page.getByTestId('guide-header');
  await expect(header).toHaveCSS('background-color', theme === 'light'
    ? 'rgb(236, 238, 247)'
    : 'rgb(21, 29, 54)');
  await expect(header).toHaveCSS('border-radius', '20px');
  await expect(header.locator('h1')).toHaveCSS('font-size',
    width < 672 ? '24px' : '32px');
  await expect(header.locator('h1')).toHaveCSS('font-weight', '700');
  await expect(header.locator('h1')).toHaveCSS('font-family',
    '"Be Vietnam Pro", Inter, system-ui, sans-serif');
  await expect(header.locator('h1')).toHaveCSS('line-height',
    width < 672 ? '31.2px' : '41.6px');
  for (const heading of await guide.locator('h2').all()) {
    await expect(heading).toHaveCSS('font-size', '18px');
    await expect(heading).toHaveCSS('font-weight', '600');
  }
  for (const section of await guide.locator(':scope > section').all()) {
    await expect(section).toHaveCSS('background-color', theme === 'light'
      ? 'rgb(255, 255, 255)'
      : 'rgb(20, 26, 46)');
    await expect(section)
      .toHaveCSS('padding-left', width < 672 ? '20px' : '28px');
  }
  await expect(header.locator(':scope > p').first()).toHaveText(locale === 'vi'
    ? 'Dùng Claude hoặc một trợ lý AI khác hỗ trợ MCP để đọc và chỉnh sửa CV '
    + 'của bạn. Bạn duyệt từng kết nối, còn việc xuất bản vẫn do bạn '
    + 'quyết định.'
    : 'Use Claude or another MCP-capable AI assistant to read and edit your '
      + 'resumes. You approve each connection, and publishing stays '
      + 'your decision.');
  await expect(page.getByTestId('guide-can').locator('li')).toHaveText(
    locale === 'vi'
      ? [
          'Xem danh sách và đọc CV của bạn, kể cả ảnh chân dung.',
          'Tạo CV mới, trong giới hạn ba CV mỗi tài khoản.',
          'Sửa nội dung, các mục, bố cục, thông tin cá nhân và giao diện.',
          'Tải lên, cắt hoặc xóa ảnh chân dung.',
          'Xóa CV. Xóa một CV đã xuất bản cũng gỡ đường dẫn công khai của '
          + 'CV đó.',
        ]
      : [
          'List and read your resumes, including the photo.',
          'Create a resume, within the limit of three per account.',
          'Edit content, sections, layout, personal details, and styling.',
          'Upload, crop, or remove the photo.',
          'Delete a resume. Deleting a published resume also takes its public '
          + 'link down.',
        ]);
  await expect(page.getByTestId('guide-cannot').locator('li')).toHaveText(
    locale === 'vi'
      ? [
          'Xuất bản, hủy xuất bản, hay đổi cài đặt PDF và lập chỉ mục. '
          + 'Bạn làm việc này trong trình chỉnh sửa.',
          'Đọc CV của người khác.',
          'Xem hay đổi mật khẩu, email, phiên đăng nhập hoặc cài đặt tài '
          + 'khoản.',
          'Xem lượt xem, xuất PDF hoặc nhập hồ sơ LinkedIn.',
          'Lưu thứ mà trình chỉnh sửa sẽ từ chối. Mọi thay đổi đều qua cùng '
          + 'các bước kiểm tra.',
        ]
      : [
          'Publish, unpublish, or change PDF and indexing settings. '
          + 'You do that in the editor.',
          'Read anyone else\'s resumes.',
          'See or change your password, email, sessions, or account settings.',
          'See view counts, export a PDF, or import from LinkedIn.',
          'Save anything the editor would reject. Every change passes the '
          + 'same checks.',
        ]);
  await expect(guide.locator('pre code')).toHaveText([
    'https://aboutme.vn/mcp',
    '$ claude mcp add --transport http --scope user aboutme '
    + 'https://aboutme.vn/mcp',
    '$ claude mcp login aboutme',
  ]);
  const blocks = guide.locator('pre');
  await expect(blocks).toHaveCount(3);
  for (const pre of await blocks.all()) {
    await expect(pre).toHaveAttribute('tabindex', '0');
    await expect(pre).toHaveAttribute('aria-label', /.+/u);
    await expect(pre).toHaveCSS('overflow-x', 'auto');
    await expect(pre).toHaveCSS('font-size', '13px');
    await expect(pre).toHaveCSS('font-family', /monospace/u);
    await expect(pre).toHaveCSS('background-color', theme === 'light'
      ? 'rgb(239, 237, 230)'
      : 'rgb(26, 33, 56)');
    await expect(pre).toHaveCSS('border-radius', '10px');
  }
  await expect(header.getByRole('link', {
    name: locale === 'vi'
      ? 'Tạo tài khoản'
      : 'Create an account', exact: true,
  }))
    .toHaveAttribute('href', '/register');
}

for (const locale of ['vi', 'en'] as const) {
  test.describe(`guide display ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
    for (const theme of ['light', 'dark'] as const) {
      for (const width of [390, 1280, 1440]) {
        test(`${theme} ${width}`, async ({ page }) => {
          await page.setViewportSize({ width, height: 900 });
          await page.context().addCookies([
            { name: 'aboutme-locale', value: locale, url: GUIDE_BASE_URL },
            { name: 'aboutme-theme', value: theme, url: GUIDE_BASE_URL },
          ]);
          await mockSignedOutSession(page);
          await page.goto('/guide/mcp');
          await assertGuide(page, locale, theme, width);
        });
      }
    }
  });
}

screenMutationTests('guide', async (page) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await mockSignedOutSession(page);
  await page.goto('/guide/mcp');
}, guideContract('vi', 'light'));

test('style mutations allow an unchanged companion declaration', async ({
  page,
}) => {
  await page.setContent(
    '<div id="target" style="overflow: hidden">Target</div>',
  );
  const target = page.locator('#target');
  await expect(target).toHaveCSS('overflow', 'hidden');
  await expect(target).toHaveCSS('opacity', '1');
  const restore = await applyStyleMutation(target, {
    overflow: 'hidden', opacity: '0',
  });
  await expect(target).toHaveCSS('opacity', '0');
  await restore();
  await expect(target).toHaveCSS('opacity', '1');
});

test('guide rejects a changed instruction icon', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await mockSignedOutSession(page);
  await page.goto('/guide/mcp');
  const assertion = () => assertGuide(page, 'vi', 'light', 390);
  await assertion();
  const icon = page.getByTestId('guide-can').locator('svg').first();
  const original = await icon.getAttribute('class');
  await icon.evaluate((node) => node.classList.remove('lucide-check'));
  await expect(icon).not.toHaveClass(/lucide-check/u);
  await expect(assertion()).rejects.toThrow('Guide instruction icons.');
  await icon.evaluate((node, value) => {
    node.setAttribute('class', value!);
  }, original);
  await assertion();
});

async function openGuide(page: Page): Promise<void> {
  await page.setViewportSize({ width: 390, height: 900 });
  await mockSignedOutSession(page);
  await page.goto('/guide/mcp');
  await page.evaluate(() => document.fonts.ready);
}

styleProofs('guide', openGuide, GUIDE_TYPE);

for (const [name, selector, message] of [
  ['web instruction', '[data-testid="guide-claude"] ol:first-of-type li p',
    'Guide web instruction order.'],
  ['code instruction', '[data-testid="guide-claude"] ol:last-of-type li p',
    'Guide code instruction order.'],
  ['troubleshooting', '[data-testid="guide-troubleshooting"] dd',
    'Guide troubleshooting order.'],
  ['request', '[data-testid="guide-try-request"] li', 'Guide request order.'],
  ['privacy', '[data-testid="guide-privacy"] li', 'Guide privacy order.'],
] as const) {
  test(`guide rejects changed ${name} content`, async ({ page }) => {
    await openGuide(page);
    await proveMutation(() => expectGuideInstructions(page, 'vi'),
      () => mutateText(page.locator(selector).first(), 'MUTATED'), message);
  });
}

for (const selector of [
  '[data-testid="guide-can"] ul', '[data-testid="guide-cannot"] ul',
  '[data-testid="guide-claude"] ol:first-of-type',
  '[data-testid="guide-claude"] ol:last-of-type',
  '[data-testid="guide-try-request"] ul', '[data-testid="guide-privacy"] ul',
]) {
  test(`guide rejects missing list semantics at ${selector}`, async ({
    page,
  }) => {
    await openGuide(page);
    await proveMutation(() => expectGuideLists(page), () => mutateAttribute(
      page.locator(selector), 'role', 'presentation',
    ), 'Guide list structure.');
  });
}

for (const defect of ['geometry', 'stroke'] as const) {
  test(`guide rejects changed SVG ${defect}`, async ({ page }) => {
    await openGuide(page);
    const path = page.getByTestId('guide-can').locator('path').first();
    await proveMutation(() => expectGuideIcons(page, 'light'), () =>
      defect === 'geometry'
        ? mutateAttribute(path, 'd', 'M0 0h100')
        : applyStyleMutation(path, { stroke: 'none' }), 'Guide SVG paint.');
  });
}

test('guide rejects clipping in an instruction ancestor', async ({ page }) => {
  await openGuide(page);
  await proveMutation(() => expectScreenClass(page,
    guideContract('vi', 'light'), 'containment'),
  () => applyStyleMutation(page.getByTestId('guide-can').locator('ul'), {
    height: '24px', overflow: 'hidden',
  }), 'Screen ancestor clipping.');
});

for (const [selector, message] of [
  ['[data-testid="guide-plan-note"]', 'Guide explanatory copy.'],
  ['[data-testid="guide-other-apps"] p', 'Guide other apps copy.'],
] as const) {
  test(`guide rejects replaced prose at ${selector}`, async ({ page }) => {
    await openGuide(page);
    await proveMutation(() => expectGuideProse(page, 'vi'),
      () => mutateText(page.locator(selector).first(), 'MUTATED'), message);
  });
}

screenRegressionTests();

test('guide rejects an unreachable code block', async ({ page }) => {
  await openGuide(page);
  await proveMutation(() => expectGuideCodeScroll(page),
    () => applyStyleMutation(page.getByTestId('guide-mcp-page')
      .locator('pre').nth(1), { 'overflow-x': 'hidden' }),
    'Guide code scrolling.');
});
