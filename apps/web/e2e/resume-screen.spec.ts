import { expect, test, type Locator, type Page } from '@playwright/test';
import { TEMPLATES } from '@aboutme/schema/templates';

import { measureTextContainment } from './display-assertions';
import { denyExternalRequests, waitForImages } from './support';

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
  await expect(flow.locator('.section-heading h2')).toHaveText(
    sections.map((index) => content.headings[index]!),
  );
  await expect(flow.locator('.entry-title')).toHaveText(
    sections.flatMap((index) => [...content.titles[index]!]),
  );
  for (const heading of await flow.locator('.section-heading h2').all()) {
    await expect(heading).toBeVisible();
  }
  for (const title of await flow.locator('.entry-title').all()) {
    await expect(title).toBeVisible();
  }
  const paragraphs = flow.locator('.entry-body p');
  for (const paragraph of await paragraphs.all()) {
    await expect.poll(() => measureTextContainment(paragraph)).toEqual({
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
            const letter = 'paper' in cell
              || preset!.customization.pageFormat === 'letter';
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
    await page.locator('.resume-main').evaluate((flow, kind) => {
      if (kind === 'drop') flow.children[1]!.remove();
      else flow.prepend(flow.children[1]!);
    }, mutation);
    await expect(assertClean()).rejects.toThrow();
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
  await section.evaluate((node) => {
    (node as HTMLElement).style.display = 'none';
  });
  await expect(assertClean()).rejects.toThrow();
  await page.reload();
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
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
  await expect(article).toHaveCSS('background-color', rgb(ground));
  await expect(article).toHaveCSS('color', rgb(foreground));
  await expect(page.locator('.public-toolbar')).toHaveCSS(
    'background-color',
    dark ? 'rgb(12, 16, 32)' : 'rgb(249, 248, 245)',
  );
  const regionSelector = template === 'modern-sidebar'
    ? '.resume-sidebar'
    : '.resume-header';
  await expect(article.locator(regionSelector))
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

for (const [name, css] of [
  ['article ground', '.resume-document { background: red !important; }'],
  ['sidebar ground', '.resume-sidebar { background: red !important; }'],
  ['foreground', '.resume-document { color: red !important; }'],
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
    const mutation = await page.addStyleTag({ content: css });
    await expect(assertClean()).rejects.toThrow();
    await mutation.evaluate((node) => node.remove());
    await assertClean();
  });
}
