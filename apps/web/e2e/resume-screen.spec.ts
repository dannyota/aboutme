import { expect, test, type Locator, type Page } from '@playwright/test';
import { TEMPLATES } from '@aboutme/schema/templates';

import { captureReviewImage } from './review-images';
import { FIXED_PHOTO_DATA_URL } from '../app/pages/_harness/photo-fixture';

import {
  expectScreen, RESUME_STYLES, resumeContract, measureTextContainment,
} from './display-assertions';
import {
  applyStyleMutation, mutateAttribute, mutateText, proveMutation,
  screenMutationTests,
} from './style-mutation';
import { expectRendererContent } from './renderer-content';
import { rendererWidgetTests } from './renderer-widgets';
import { expectRendererPaint, rendererTypeRules } from './renderer-details';
import { expectStyleRules } from './screen-expectations';
import { styleProofs } from './screen-style-proofs';
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
  await expectRendererContent(page, language, justify, template);
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
    for (const [cellIndex, cell] of CELLS.entries()) {
      for (const [languageIndex, language] of
        (['vi', 'en'] as const).entries()) {
        for (const [viewerIndex, viewer] of
          (['light', 'dark'] as const).entries()) {
          const continuous = cell.template === 'modern-sidebar'
            && cell.mode === 'continuous';
          const rotatedLocale = (Math.floor(cellIndex / 2) + languageIndex) % 2;
          const rotatedViewer = (cellIndex + languageIndex) % 2;
          if (!continuous && (locale !== (rotatedLocale === 0 ? 'vi' : 'en')
            || viewerIndex !== rotatedViewer)) {
            continue;
          }
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
            if (cell.mode === 'paged') {
              await expectRendererPageMembership(page, cell.template,
                'align' in cell, 'paper' in cell, language);
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
      if (kind === 'drop') {
        flow.children[1]!.remove();
      } else {
        flow.prepend(flow.children[1]!);
      }
    }, mutation);
    expect(await page.locator('.resume-main').innerHTML()).not.toBe(before);
    await expect(assertFlow(page.locator('.resume-main'), 'vi',
      [0, 1, 2, 6, 7])).rejects.toThrow('Renderer section order.');
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
    for (const [templateIndex, template] of templates.entries()) {
      const offsets = [0, 2, 1, 2, 3];
      const pairs = [
        ['vi', 390], ['vi', 1440], ['en', 390], ['en', 1440],
      ] as const;
      for (const [ownerIndex, owner] of
        (['light', 'dark', 'system'] as const).entries()) {
        for (const [viewerIndex, viewer] of
          (['light', 'dark'] as const).entries()) {
          for (const width of [390, 1440]) {
            const pair = pairs[(ownerIndex * 2 + viewerIndex
              + offsets[templateIndex]!) % pairs.length]!;
            if (language !== pair[0] || width !== pair[1]) {
              continue;
            }
            test(`${template} ${owner} ${viewer} ${width}`, async ({
              page,
            }, testInfo) => {
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
              await expectRendererContent(page, language, true, template);
              if (language === 'vi' && template === 'modern-sidebar'
                && owner === 'dark' && viewer === 'light' && width === 390) {
                await captureReviewImage(testInfo,
                  'public-modern-sidebar-vi-dark-390', () =>
                    page.screenshot({
                      animations: 'disabled', timeout: 750,
                    }));
              }
              if (language === 'en' && template === 'executive-band'
                && owner === 'light' && viewer === 'light' && width === 1440) {
                await captureReviewImage(testInfo,
                  'public-executive-band-en-light-1440', () =>
                    page.screenshot({
                      animations: 'disabled', timeout: 750,
                    }));
              }
              if (template === 'modern-sidebar') {
                await assertFlow(article.locator('.resume-main'), language,
                  [0, 1, 2, 6, 7]);
                await assertFlow(article.locator('.resume-sidebar'), language,
                  [3, 4, 5]);
              } else {
                await assertFlow(article.locator('.layout-one-column'),
                  language, [0, 1, 2, 3, 4, 5, 6, 7]);
              }
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
  await expectStyleRules(page, rendererTypeRules(template));
  if (!publicPage) {
    await expectRendererPaint(page, template);
  }
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

rendererWidgetTests();

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

for (const mode of ['continuous', 'public'] as const) {
  test(`renderer ${mode} rejects replaced body content`, async ({ page }) => {
    await page.goto('/_harness/render?fixture=full&template=modern-sidebar'
      + `&mode=${mode}`);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await proveMutation(() =>
      expectRendererContent(page, 'en', mode === 'public'),
    () => mutateText(page.locator('.entry-body p').last(), 'MUTATED'),
    'Renderer body content.');
  });
}

for (const template of ['modern-sidebar', 'executive-band']) {
  styleProofs(`renderer ${template}`, async (page) => {
    await page.goto(`/_harness/render?fixture=full&template=${template}`
      + '&mode=continuous');
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  }, rendererTypeRules(template));
}

test('public rejects changed section order', async ({ page }) => {
  await page.goto('/_harness/render?fixture=full&template=modern-sidebar'
    + '&mode=public');
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  const flow = page.locator('.resume-main');
  const assertion = () => assertFlow(flow, 'en', [0, 1, 2, 6, 7]);
  await assertion();
  await flow.evaluate((node) => node.prepend(node.children[1]!));
  await expect(flow.locator('.section-heading h2').first())
    .toHaveText('Experience');
  await expect(assertion()).rejects.toThrow('Renderer section order.');
});

for (const [selector, message] of [
  ['.resume-headline', 'Renderer headline content.'],
  ['.contact-chip a', 'Renderer contact content.'],
  ['.contact-label', 'Renderer contact label.'],
] as const) {
  test(`renderer rejects replaced ${selector} content`, async ({ page }) => {
    await page.goto('/_harness/render?fixture=vn-full&template=modern-sidebar'
      + '&mode=continuous');
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await proveMutation(() => expectRendererContent(page, 'vi'),
      () => mutateText(page.locator(selector).first(), 'MUTATED'), message);
  });
}

for (const [selector, property, message] of [
  ['.section-heading h2', 'color', 'Renderer heading paint.'],
  ['.resume-sidebar', 'background-color', 'Renderer region paint.'],
  ['.resume-name', 'color', 'Renderer name paint.'],
] as const) {
  test(`renderer rejects changed ${message}`, async ({ page }) => {
    await page.goto('/_harness/render?fixture=full&template=modern-sidebar'
      + '&mode=continuous');
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await proveMutation(() => expectRendererPaint(page, 'modern-sidebar'),
      () => applyStyleMutation(page.locator(selector).first(), {
        [property]: 'rgb(255, 0, 0)',
      }), message);
  });
}

test('renderer rejects changed English page membership', async ({ page }) => {
  await page.setViewportSize({ width: 794, height: 1123 });
  await page.goto('/_harness/render?fixture=full&template=classic-serif'
    + '&mode=paged');
  await expect(page.locator('[data-pagination-settled="true"]')).toHaveCount(1);
  const assertion = () => expectRendererPageMembership(page, 'classic-serif',
    false, false, 'en');
  await assertion();
  const pages = page.locator('.resume-page:not(.pagination-measurement)');
  const entry = pages.nth(0).locator('.entry').filter({
    hasText: 'Vietnamese',
  });
  await expect(entry).toHaveCount(1);
  await entry.evaluate((node) => {
    document.querySelectorAll('.resume-page:not(.pagination-measurement)')[1]!
      .querySelector('.layout-one-column')!.append(node);
  });
  await expect(pages.nth(1).getByText('Vietnamese', { exact: true }))
    .toHaveCount(1);
  await expect(assertion()).rejects.toThrow('Renderer page membership.');
});

test('renderer rejects a changed contact destination', async ({ page }) => {
  await page.goto('/_harness/render?fixture=vn-full&template=modern-sidebar'
    + '&mode=continuous');
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  await proveMutation(() => expectRendererContent(page, 'vi'),
    () => mutateAttribute(page.locator('.contact-chip a').first(), 'href',
      'mailto:mutated@example.invalid'), 'Renderer contact link.');
});

test('public rejects changed entry order', async ({ page }) => {
  await page.goto('/_harness/render?fixture=full&template=modern-sidebar'
    + '&mode=public');
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  const flow = page.locator('.resume-main');
  const assertion = () => assertFlow(flow, 'en', [0, 1, 2, 6, 7]);
  await assertion();
  const entries = flow.locator('.entry').filter({
    has: page.locator('.entry-title'),
  });
  const first = await entries.nth(0).innerHTML();
  const second = await entries.nth(1).innerHTML();
  await entries.nth(0).evaluate((node, html) => {
    node.innerHTML = html;
  }, second);
  await entries.nth(1).evaluate((node, html) => {
    node.innerHTML = html;
  }, first);
  await expect(entries.first().locator('.entry-title'))
    .toHaveText('B.Sc. Mathematics');
  await expect(assertion()).rejects.toThrow('Renderer entry order.');
});
