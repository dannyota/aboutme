import { expect, test, type Locator, type Page } from '@playwright/test';
import { TEMPLATES } from '@aboutme/schema/templates';

import { FIXED_PHOTO_DATA_URL } from '../app/pages/_harness/photo-fixture';

import {
  expectScreen, RESUME_STYLES, resumeContract, measureTextContainment,
} from './display-assertions';
import { applyStyleMutation, screenMutationTests } from './style-mutation';
import {
  denyExternalRequests, expectRendererPageMembership, waitForImages,
} from './support';

const CELLS = [
  { template: 'classic-serif', mode: 'paged' },
  { template: 'classic-serif', mode: 'paged', align: 'justify' },
  { template: 'engineer-compact', mode: 'paged' },
  { template: 'modern-sidebar', mode: 'paged' },
  { template: 'executive-band', mode: 'paged' },
  { template: 'consulting-formal', mode: 'paged' },
  { template: 'consulting-formal', mode: 'paged', paper: 'letter' },
  { template: 'academic-dense', mode: 'paged' },
  { template: 'modern-sidebar', mode: 'continuous' },
] as const;

const CONTENT = {
  en: {
    headings: ['Summary', 'Experience', 'Education', 'Skills', 'Languages',
      'Certifications', 'Projects', 'Awards'],
    titles: [[], ['Principal Engineer'], ['B.Sc. Mathematics'],
      ['Go', 'TypeScript'], ['English', 'Vietnamese'],
      ['Certified Analytical Engineer'], ['Note G Interpreter'],
      ['Lovelace Medal', 'Hackathon Winner']],
  },
  vi: {
    headings: ['Giới thiệu', 'Kinh nghiệm làm việc', 'Học vấn', 'Kỹ năng',
      'Ngôn ngữ', 'Chứng chỉ', 'Dự án', 'Hoạt động cộng đồng'],
    titles: [[], ['Kỹ sư trưởng'], ['Kỹ sư Công nghệ thông tin'],
      ['Thiết kế hệ thống', 'Lập trình TypeScript'],
      ['Tiếng Việt', 'Tiếng Anh'],
      ['Chứng chỉ Kiến trúc phần mềm'], ['Từ điển dữ liệu mở'],
      ['Giải thưởng Sáng tạo trẻ']],
  },
} as const;

async function assertFlow(
  flow: Locator,
  language: keyof typeof CONTENT,
  sections: readonly number[],
): Promise<void> {
  const content = CONTENT[language];
  await expect(flow.locator('.section-heading h2'), 'Renderer section order.')
    .toHaveText(
      sections.map((index) => content.headings[index]!),
    );
  await expect(flow.locator('.entry-title'), 'Renderer entry order.')
    .toHaveText(
      sections.flatMap((index) => [...content.titles[index]!]),
    );
  const text = flow.locator('.section-heading h2, .entry-title, .entry-body p');
  for (const element of await text.all()) {
    await expect.poll(() => measureTextContainment(element),
      'Renderer text containment.').toEqual({
      visible: true, contained: true,
    });
  }
}

async function assertRenderer(
  page: Page,
  template: string,
  language: keyof typeof CONTENT,
  justify = false,
): Promise<void> {
  await expectScreen(page, resumeContract(template, language));
  await assertRendererStyles(page, template, language, justify);
  const documents = page.locator(
    '.resume-document:not(.pagination-measurement)',
  );
  await expect(documents.locator('.resume-name')).toHaveText(
    language === 'vi' ? 'Nguyễn Thị Minh Châu' : 'Ada Lovelace',
  );
  const profile = justify
    ? 'Kỹ sư giàu kinh nghiệm với hơn mười năm xây dựng hệ thống phân tán '
    + 'cho ngân hàng, thương mại điện tử và dịch vụ công. Tôi tập trung '
    + 'vào độ tin cậy, khả năng quan sát và trải nghiệm của nhà phát '
    + 'triển, đồng thời hướng dẫn đội ngũ áp dụng kiểm thử tự động, '
    + 'triển khai liên tục và đánh giá thiết kế định kỳ để sản phẩm dễ '
    + 'sử dụng và dễ bảo trì.'
    : language === 'vi'
      ? 'Kỹ sư giàu kinh nghiệm, tập trung vào hệ thống phân tán '
      + 'và sản phẩm dễ sử dụng.'
      : 'Engineer and mathematician with a focus on analytical engines.';
  await expect(documents.getByText(profile, { exact: true })).toHaveCount(1);
  await expect(documents.getByText(profile, { exact: true })).toBeVisible();
  if (template === 'modern-sidebar' || template === 'engineer-compact') {
    const sidebar = template === 'modern-sidebar' ? [3, 4, 5] : [3, 5, 4, 2];
    const main = template === 'modern-sidebar' ? [0, 1, 2, 6, 7] : [0, 1, 6, 7];
    await assertFlow(documents.locator('.resume-main'), language, main);
    await assertFlow(documents.locator('.resume-sidebar'), language, sidebar);
  } else {
    await assertFlow(documents.locator('.layout-one-column'), language,
      [0, 1, 2, 3, 4, 5, 6, 7]);
  }
}

for (const locale of ['vi', 'en'] as const) {
  test.describe(`renderer interface ${locale}`, () => {
    test.use({ locale: locale === 'vi' ? 'vi-VN' : 'en-US' });
    for (const cell of CELLS) {
      for (const language of ['vi', 'en'] as const) {
        for (const viewer of ['light', 'dark'] as const) {
          const title = `renderer ${cell.template} ${cell.mode} ${language}`
            + ` ${viewer}`
            + ` ${'align' in cell ? cell.align : ''}`
            + ` ${'paper' in cell ? cell.paper : ''}`;
          test(title, async ({ page }) => {
            const preset = TEMPLATES.find(({ id }) => id === cell.template);
            expect(preset).toBeDefined();
            const letter = 'paper' in cell;
            await page.setViewportSize(letter
              ? { width: 816, height: 1056 }
              : { width: 794, height: 1123 });
            await page.emulateMedia({ colorScheme: viewer });
            const external = await denyExternalRequests(page);
            const fixture = language === 'vi' ? 'vn-full' : 'full';
            const response = await page.goto(
              `/_harness/render?fixture=${fixture}&template=${cell.template}`
              + `&mode=${cell.mode}`
              + ('align' in cell ? `&align=${cell.align}` : '')
              + ('paper' in cell ? `&paper=${cell.paper}` : ''),
            );
            expect(response?.ok()).toBe(true);
            await expect(page.locator('[data-fonts-ready="true"]'))
              .toHaveCount(1);
            if (cell.mode === 'paged') {
              await expect(page.locator('[data-pagination-settled="true"]'))
                .toHaveCount(1);
            }
            await waitForImages(page);
            await assertRenderer(page, cell.template, language,
              'align' in cell && cell.align === 'justify');
            if (cell.mode === 'paged' && language === 'vi') {
              await expectRendererPageMembership(page, cell.template,
                'align' in cell, 'paper' in cell);
            }
            expect(external).toEqual([]);
          });
        }
      }
    }
  });
}

for (const mutation of ['drop', 'reorder'] as const) {
  test(`renderer rejects ${mutation} of a section`, async ({ page }) => {
    await page.goto('/_harness/render?fixture=vn-full'
      + '&template=modern-sidebar&mode=continuous');
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const assertClean = () => assertRenderer(page, 'modern-sidebar', 'vi');
    await assertClean();
    const before = await page.locator('.resume-main').innerHTML();
    await page.locator('.resume-main').evaluate((flow, kind) => {
      if (kind === 'drop') flow.children[1]!.remove();
      else flow.prepend(flow.children[1]!);
    }, mutation);
    expect(await page.locator('.resume-main').innerHTML()).not.toBe(before);
    await expect(assertClean()).rejects.toThrow('Renderer section order.');
    await page.reload();
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await assertClean();
  });
}

test('renderer rejects a hidden language section', async ({ page }) => {
  await page.goto('/_harness/render?fixture=vn-full'
    + '&template=modern-sidebar&mode=continuous');
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  const assertClean = () => assertRenderer(page, 'modern-sidebar', 'vi');
  await assertClean();
  const section = page.locator('.resume-sidebar .resume-section').filter({
    has: page.getByRole('heading', { name: 'Ngôn ngữ', exact: true }),
  });
  await expect(section).toHaveCount(1);
  const restore = await applyStyleMutation(section, { display: 'none' });
  await expect(assertClean()).rejects.toThrow('Screen containment.');
  await restore();
  await assertClean();
});

test('renderer rejects a transparent language section', async ({ page }) => {
  await page.goto('/_harness/render?fixture=vn-full'
    + '&template=modern-sidebar&mode=continuous');
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  const assertClean = () => assertRenderer(page, 'modern-sidebar', 'vi');
  await assertClean();
  const section = page.locator('.resume-sidebar .resume-section').filter({
    has: page.getByRole('heading', { name: 'Ngôn ngữ', exact: true }),
  });
  await expect(section).toHaveCount(1);
  const restore = await applyStyleMutation(section, { opacity: '0' });
  await expect(assertClean()).rejects.toThrow('Screen containment.');
  await restore();
  await assertClean();
});

const PUBLIC_PAINT = {
  'classic-serif': {
    light: ['#ffffff', '#23262c', '#ffffff'],
    dark: ['#151617', '#dadee6', '#151617'],
  },
  'modern-sidebar': {
    light: ['#ffffff', '#1c2b33', '#e3ecf0'],
    dark: ['#0f171b', '#d5e0e6', '#1b292f'],
  },
  'editorial-wide': {
    light: ['#faf8f3', '#24211c', '#faf8f3'],
    dark: ['#191512', '#e1ddd7', '#191512'],
  },
  'creative-accent': {
    light: ['#ffffff', '#1f1a17', '#fff0eb'],
    dark: ['#1c1411', '#e4dcd8', '#352019'],
  },
  'executive-band': {
    light: ['#ffffff', '#1f2937', '#16273d'],
    dark: ['#13161b', '#d8dfe8', '#1d2f45'],
  },
} as const;

function rgb(hex: string): string {
  const channels = [1, 3, 5].map((index) =>
    Number.parseInt(hex.slice(index, index + 2), 16));
  return `rgb(${channels.join(', ')})`;
}

async function assertPublicPaint(
  page: Page,
  template: keyof typeof PUBLIC_PAINT,
  dark: boolean,
): Promise<void> {
  const [ground, foreground, region]
    = PUBLIC_PAINT[template][dark ? 'dark' : 'light'];
  const article = page.locator('.resume-document');
  await expect(article, 'Public article ground.')
    .toHaveCSS('background-color', rgb(ground));
  await expect(article, 'Public foreground.')
    .toHaveCSS('color', rgb(foreground));
  await expect(page.locator('.public-toolbar')).toHaveCSS(
    'background-color',
    dark ? 'rgb(12, 16, 32)' : 'rgb(249, 248, 245)',
  );
  const regionSelector = template === 'modern-sidebar'
    ? '.resume-sidebar'
    : '.resume-header';
  await expect(article.locator(regionSelector), 'Public region ground.')
    .toHaveCSS('background-color', rgb(region));
}

for (const language of ['vi', 'en'] as const) {
  test.describe(`public screen ${language}`, () => {
    test.use({ locale: language === 'vi' ? 'vi-VN' : 'en-US' });
    const templates = Object.keys(PUBLIC_PAINT) as
      (keyof typeof PUBLIC_PAINT)[];
    for (const template of templates) {
      for (const owner of ['light', 'dark', 'system'] as const) {
        for (const viewer of ['light', 'dark'] as const) {
          for (const width of [390, 1440]) {
            test(`${template} ${owner} ${viewer} ${width}`, async ({
              page,
            }) => {
              await page.setViewportSize({ width, height: 900 });
              await page.emulateMedia({ colorScheme: viewer });
              const external = await denyExternalRequests(page);
              const fixture = language === 'vi' ? 'vn-full' : 'full';
              const scheme = owner === 'light' ? '' : `&scheme=${owner}`;
              const response = await page.goto(
                `/_harness/render?fixture=${fixture}&template=${template}`
                + `&mode=public${scheme}`,
              );
              expect(response?.ok()).toBe(true);
              await expect(page.locator('[data-fonts-ready="true"]'))
                .toHaveCount(1);
              await waitForImages(page);
              const dark = owner === 'dark'
                || (owner === 'system' && viewer === 'dark');
              const article = page.locator('.resume-document');
              await assertPublicPaint(page, template, dark);
              await expectScreen(page,
                resumeContract(template, language, dark));
              await assertRendererStyles(page, template, language, false, true);
              const geometry = await page.evaluate(() => {
                const rect = (selector: string) =>
                  document.querySelector(selector)!.getBoundingClientRect();
                const measure = rect('.public-measure');
                const bar = rect('.public-toolbar');
                const paper = rect('.resume-document');
                const main = document.querySelector('.resume-main');
                const sidebar = document.querySelector('.resume-sidebar');
                const a = main?.getBoundingClientRect();
                const b = sidebar?.getBoundingClientRect();
                return {
                  centered: Math.abs(measure.left
                    - (innerWidth - measure.right)) <= 2,
                  toolbar: bar.width === innerWidth
                    && bar.bottom <= paper.top + 1,
                  overflow: document.documentElement.scrollWidth
                    <= document.documentElement.clientWidth + 1,
                  columns: !a || !b || (innerWidth === 390
                    ? b.top >= a.bottom - 1
                    : b.left >= a.right - 1),
                };
              });
              expect(geometry).toEqual({
                centered: true, toolbar: true, overflow: true, columns: true,
              });
              await expect(article.locator('.resume-name')).toHaveText(
                language === 'vi' ? 'Nguyễn Thị Minh Châu' : 'Ada Lovelace',
              );
              const paragraphs = article.locator('.entry-body p');
              expect(await paragraphs.count()).toBeGreaterThan(0);
              for (const paragraph of await paragraphs.all()) {
                await expect.poll(() => measureTextContainment(paragraph))
                  .toEqual({ visible: true, contained: true });
              }
              await page.emulateMedia({ media: 'print' });
              await expect(page.locator('.public-toolbar')).toBeHidden();
              await expect(article).toHaveCSS(
                'background-color', rgb(PUBLIC_PAINT[template].light[0]),
              );
              expect(external).toEqual([]);
            });
          }
        }
      }
    }
    for (const viewer of ['light', 'dark'] as const) {
      for (const width of [503, 504]) {
        test(`public column and header boundary ${viewer} ${width}`, async ({
          page,
        }) => {
          await page.setViewportSize({ width: 1440, height: 900 });
          await page.emulateMedia({ colorScheme: viewer });
          const fixture = language === 'vi' ? 'vn-full' : 'full';
          const response = await page.goto(
            `/_harness/render?fixture=${fixture}&template=modern-sidebar`
            + '&mode=public&scheme=system',
          );
          expect(response?.ok()).toBe(true);
          await expect(page.locator('[data-fonts-ready="true"]'))
            .toHaveCount(1);
          const article = page.locator('.resume-document');
          await article.evaluate((element, size) => {
            const node = element as HTMLElement;
            node.style.width = `${size}px`;
            node.style.padding = '0';
            node.querySelector('.resume-header')!
              .setAttribute('data-photo-position', 'left');
          }, width);
          const tracks = (selector: string) => article.locator(selector)
            .evaluate((element) =>
              getComputedStyle(element).gridTemplateColumns.split(' ').length);
          await expect.poll(() => tracks('.layout-two-columns'))
            .toBe(width === 503 ? 1 : 2);
          await expect.poll(() => tracks('.resume-header'))
            .toBe(width === 503 ? 1 : 2);
        });
      }
    }
  });
}

for (const [name, selector, styles] of [
  ['article ground', '.resume-document', {
    'background-color': 'rgb(255, 0, 0)',
    'background-image': 'none',
  }],
  ['sidebar ground', '.resume-sidebar', {
    'background-color': 'rgb(255, 0, 0)',
    'background-image': 'none',
  }],
  ['foreground', '.resume-document', { color: 'rgb(255, 0, 0)' }],
] as const) {
  test(`public paint rejects changed ${name}`, async ({ page }) => {
    const response = await page.goto(
      '/_harness/render?fixture=vn-full&template=modern-sidebar'
      + '&mode=public&scheme=dark',
    );
    expect(response?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const assertClean = () => assertPublicPaint(page, 'modern-sidebar', true);
    await assertClean();
    const restore = await applyStyleMutation(page.locator(selector), styles);
    const failure = name === 'sidebar ground' ? 'region ground' : name;
    await expect(assertClean()).rejects.toThrow(`Public ${failure}.`);
    await restore();
    await assertClean();
  });
}

async function assertRendererStyles(
  page: Page, template: string, language: 'vi' | 'en', justify: boolean,
  publicPage = false,
): Promise<void> {
  const [family, fallback, base, lineHeight, x, y, sectionGap, entryGap]
    = RESUME_STYLES[template as keyof typeof RESUME_STYLES];
  const documents = page.locator(
    '.resume-document:not(.pagination-measurement)',
  );
  for (const root of await documents.all()) {
    await expect.poll(() => root.evaluate((node) =>
      getComputedStyle(node).fontFamily.split(',').map((name) =>
        name.trim().replaceAll('"', ''))), 'Renderer family.')
      .toEqual([family, fallback]);
    await expect(root, 'Renderer synthesis.')
      .toHaveCSS('font-synthesis', 'none');
    await expect(root, 'Renderer base size.')
      .toHaveCSS('font-size', `${base}px`);
    await expect(root, 'Renderer line height.').toHaveCSS('line-height',
      `${Number((base * lineHeight).toFixed(3))}px`);
    for (const image of await root.locator('.resume-photo img').all()) {
      await expect(image, 'Renderer photo identity.')
        .toHaveAttribute('src', FIXED_PHOTO_DATA_URL);
    }
    if (!publicPage) {
      const hex = RESUME_STYLES[template as keyof typeof RESUME_STYLES][8];
      const channels = [1, 3, 5].map((index) =>
        parseInt(hex.slice(index, index + 2), 16));
      await expect(root, 'Renderer body ink.').toHaveCSS(
        'color', `rgb(${channels.join(', ')})`);
      const padding = await root.evaluate((element) => {
        const css = getComputedStyle(element);
        return [parseFloat(css.paddingLeft), parseFloat(css.paddingTop)];
      });
      expect(Math.abs(padding[0]! - x * 96 / 25.4), 'Renderer margin x.')
        .toBeLessThanOrEqual(1);
      expect(Math.abs(padding[1]! - y * 96 / 25.4), 'Renderer margin y.')
        .toBeLessThanOrEqual(1);
    }
    for (const [selector, size, weight] of [
      ['.resume-name', base * 2, '700'],
      ['.resume-headline', base * 1.15, '400'],
      ['.section-heading h2', base * 1.1, '700'],
      ['.entry-title', base, '700'],
    ] as const) {
      for (const node of await root.locator(selector).all()) {
        expect(Math.abs(await node.evaluate((element) =>
          parseFloat(getComputedStyle(element).fontSize)) - size),
        'Renderer type scale.').toBeLessThanOrEqual(0.1);
        await expect(node, 'Renderer weight.').toHaveCSS('font-weight', weight);
      }
    }
    for (const heading of await root.locator('.section-heading h2').all()) {
      await expect(heading, 'Renderer heading transform.')
        .toHaveCSS('text-transform',
          template === 'editorial-wide'
            ? 'none'
            : template === 'consulting-formal'
              ? language === 'vi' ? 'none' : 'capitalize'
              : 'uppercase');
    }
    const paragraphs = root.locator('.entry-body p, .entry-body li');
    for (const paragraph of await paragraphs.all()) {
      await expect(paragraph, 'Renderer body alignment.').toHaveCSS(
        'text-align', justify ? 'justify' : 'left');
    }
    const paged = (await root.getAttribute('class'))?.includes('resume-page');
    for (const [selector, gap] of [
      ['.resume-section', sectionGap], ['.entry', entryGap],
    ] as const) {
      for (const node of await root.locator(selector).all()) {
        await expect(node, 'Renderer spacing.')
          .toHaveCSS('margin-bottom', paged ? '0px' : `${gap}px`);
      }
    }
    const columns = root.locator('.layout-two-columns');
    if (await columns.count()) {
      const geometry = await columns.evaluate((element) => {
        const main = element.querySelector('.resume-main')!
          .getBoundingClientRect();
        const side = element.querySelector('.resume-sidebar')!
          .getBoundingClientRect();
        const box = element.getBoundingClientRect();
        return {
          main: { right: main.right },
          side: { left: side.left, width: side.width }, width: box.width,
        };
      });
      if (geometry.side.left >= geometry.main.right - 1) {
        expect(Math.abs(geometry.side.width - geometry.width * 0.32),
          'Renderer sidebar ratio.').toBeLessThanOrEqual(1);
        expect(Math.abs(geometry.side.left - geometry.main.right
          - 8 * 96 / 25.4),
        'Renderer column gap.').toBeLessThanOrEqual(1);
      }
    }
  }
}

for (const mode of ['continuous', 'public'] as const) {
  screenMutationTests(`renderer ${mode}`, async (page) => {
    await page.setViewportSize({ width: 794, height: 1123 });
    await page.goto('/_harness/render?fixture=vn-full&template=modern-sidebar'
      + `&mode=${mode}`);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  }, resumeContract('modern-sidebar', 'vi'));
}

async function assertWidgets(
  page: Page, mode: 'bar' | 'dots' | 'tag' | 'text',
  engineer = false,
): Promise<void> {
  const entries = page.locator('.resume-document .entry');
  await expect(entries, 'Widget entry count.').toHaveCount(14);
  const solid = engineer ? 'rgb(13, 90, 115)' : 'rgb(15, 107, 125)';
  const trackColor = (prefix: string) => engineer
    ? 'rgb(207, 222, 227)'
    : prefix === 'SKILL' ? 'rgb(207, 225, 229)' : 'rgb(185, 210, 217)';
  for (const prefix of ['SKILL', 'LANGUAGE']) {
    for (let level = 0; level <= 6; level++) {
      const entry = entries.filter({
        has: page.getByText(
          `${prefix}-${level === 6 ? 'NONE' : level}`, { exact: true }),
      });
      const widget = entry.locator('.level-widget');
      await expect(entry, 'Widget entry identity.').toHaveCount(1);
      if (level === 6 || mode === 'text') {
        await expect(widget, 'Widget absence.').toHaveCount(0);
        continue;
      }
      await expect(widget, 'Widget accessible level.').toHaveAccessibleName(
        `${prefix}-${level}: ${level} of 5`);
      await expect(widget, 'Widget accessible role.').toHaveRole('img');
      await expect(widget, 'Widget visibility.').toBeVisible();
      await expect(widget, 'Widget visibility.').toHaveCSS('opacity', '1');
      if (mode === 'bar') {
        const track = widget.locator('.level-track');
        const fill = widget.locator('.level-fill');
        await expect(track, 'Widget track height.').toHaveCSS('height', '4px');
        await expect(track, 'Widget track color.').toHaveCSS('background-color',
          trackColor(prefix));
        await expect(fill, 'Widget fill color.')
          .toHaveCSS('background-color', solid);
        const geometry = await widget.evaluate((element) => {
          const track = element.querySelector('.level-track')!
            .getBoundingClientRect();
          const fill = element.querySelector('.level-fill')!
            .getBoundingClientRect();
          return {
            width: track.width, fill: fill.width,
            left: fill.left - track.left,
          };
        });
        expect(Math.abs(geometry.fill - geometry.width * level / 5),
          'Widget bar length.').toBeLessThanOrEqual(1);
        expect(Math.abs(geometry.left), 'Widget bar start.')
          .toBeLessThanOrEqual(1);
      } else if (mode === 'dots') {
        const dots = widget.locator('.level-dot');
        await expect(dots, 'Widget dot count.').toHaveCount(5);
        for (let index = 0; index < 5; index++) {
          await expect(dots.nth(index), 'Widget dot diameter.')
            .toHaveCSS('width', '7px');
          await expect(dots.nth(index), 'Widget dot diameter.')
            .toHaveCSS('height', '7px');
          await expect(dots.nth(index), 'Widget dot paint.').toHaveCSS(
            'background-color', index < level
              ? solid
              : trackColor(prefix));
        }
        await expect(widget.locator('.level-dots'), 'Widget dot gap.')
          .toHaveCSS('gap', '4px');
      } else {
        await expect(widget, 'Widget tag label.').toHaveText(`${level}/5`);
        await expect(widget, 'Widget tag paint.')
          .toHaveCSS('background-color', solid);
        await expect(widget, 'Widget tag ink.')
          .toHaveCSS('color', 'rgb(255, 255, 255)');
        const padding = await widget.evaluate((element) => {
          const css = getComputedStyle(element);
          return {
            x: parseFloat(css.paddingLeft),
            y: parseFloat(css.paddingTop),
          };
        });
        const size = engineer ? 11.7 : 12.6;
        expect(Math.abs(padding.x - size * 0.5), 'Widget tag padding.')
          .toBeLessThanOrEqual(0.1);
        expect(Math.abs(padding.y - size * 0.15), 'Widget tag padding.')
          .toBeLessThanOrEqual(0.1);
        await expect(widget, 'Widget tag border.')
          .toHaveCSS('border-top-width', '0px');
        await expect(widget, 'Widget tag radius.')
          .toHaveCSS('border-radius', '3px');
        await expect.poll(() => measureTextContainment(widget),
          'Widget tag containment.')
          .toEqual({ visible: true, contained: true });
      }
    }
  }
}

for (const template of ['modern-sidebar', 'engineer-compact']) {
  for (const language of ['vi', 'en'] as const) {
    for (const mode of ['bar', 'dots', 'tag', 'text'] as const) {
      test(`screen widget inputs ${template} ${language} ${mode}`, async ({
        page,
      }) => {
        await page.setViewportSize({ width: 794, height: 1123 });
        await page.goto(`/_harness/render?template=${template}&mode=continuous`
          + `&fixture=print-probe-widgets-${mode}&lng=${language}`);
        await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
        const heading = language === 'vi' ? 'Kỹ năng' : 'Skills';
        const selector = '.resume-main .section-heading h2';
        await expectScreen(page, {
          ...resumeContract(template, language), image: undefined,
          copy: [selector, heading],
          aria: [selector, `- heading "${heading}" [level=2]`],
        });
        await assertWidgets(page, mode, template === 'engineer-compact');
      });
    }
  }
}

for (const mode of ['bar', 'dots', 'tag'] as const) {
  test(`screen widgets reject changed ${mode}`, async ({ page }) => {
    await page.goto('/_harness/render?template=modern-sidebar&mode=continuous'
      + `&fixture=print-probe-widgets-${mode}`);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const assertion = () => assertWidgets(page, mode);
    await assertion();
    const widget = page.getByRole('img', {
      name: 'SKILL-4: 4 of 5', exact: true,
    });
    let restore: () => Promise<void>;
    if (mode === 'tag') {
      await widget.locator('span').evaluate((node) => {
        node.textContent = '3/5';
      });
      await expect(widget).toHaveText('3/5');
      restore = async () => {
        await widget.locator('span').evaluate((node) => {
          node.textContent = '4/5';
        });
      };
    } else {
      const target = widget.locator(mode === 'bar'
        ? '.level-fill'
        : '.level-dot:last-child');
      restore = await applyStyleMutation(target, mode === 'bar'
        ? { width: ['20%', /^\d+(?:\.\d+)?px$/u] }
        : { 'background-color': 'rgb(15, 107, 125)' });
    }
    await expect(assertion()).rejects.toThrow(mode === 'bar'
      ? 'Widget bar length.'
      : mode === 'dots'
        ? 'Widget dot paint.'
        : 'Widget tag label.');
    await restore();
    await assertion();
  });
}

test('renderer rejects moving an entry to another page', async ({ page }) => {
  await page.setViewportSize({ width: 794, height: 1123 });
  await page.goto('/_harness/render?fixture=vn-full'
    + '&template=classic-serif&mode=paged');
  await expect(page.locator('[data-pagination-settled="true"]')).toHaveCount(1);
  const assertion = () => expectRendererPageMembership(page, 'classic-serif');
  await assertion();
  const entry = page.locator('.resume-page:not(.pagination-measurement) .entry')
    .filter({ hasText: 'Tiếng Anh' });
  await expect(entry).toHaveCount(1);
  await entry.evaluate((node) => {
    document.querySelectorAll('.resume-page:not(.pagination-measurement)')[1]!
      .querySelector('.layout-one-column')!.append(node);
  });
  await expect(page.locator('.resume-page:not(.pagination-measurement)')
    .nth(1).getByText('Tiếng Anh', { exact: true })).toHaveCount(1);
  await expect(assertion()).rejects.toThrow('Renderer page membership.');
});
