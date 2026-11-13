import { expect, test, type Page } from '@playwright/test';

import { expectScreen } from './display-assertions';
import {
  expectProjectColors,
  expectProjectColumns,
  expectProjectMeasure,
  expectProjectStructure,
  expectProjectStylesheet,
  expectRowMeta,
  type Lng,
  openProjectPage,
  projectContract,
  type ProjectPageKind,
  type Scheme,
} from './project-page-details';
import {
  applyStyleMutation, mutateAttribute, mutationTest, proveMutation,
  screenMutationTests,
} from './style-mutation';

const KINDS: readonly ProjectPageKind[] = ['project-list', 'project'];
const SCHEMES: readonly Scheme[] = ['light', 'dark'];
const WIDTHS = [320, 768, 1280] as const;

async function assertProjectPage(
  page: Page, kind: ProjectPageKind, lng: Lng, scheme: Scheme,
  external: readonly string[],
): Promise<void> {
  expect(external).toEqual([]);
  await expect(page.locator('html')).toHaveAttribute('lang', lng);
  await expectProjectStylesheet(page);
  await expectScreen(page, projectContract(kind, lng, scheme));
  await expectProjectMeasure(page, kind);
  if (kind === 'project') {
    await expectProjectColumns(page);
  } else {
    await expectRowMeta(page);
  }
  await expectProjectColors(page, kind, scheme);
}

for (const [kindIndex, kind] of KINDS.entries()) {
  for (const [schemeIndex, scheme] of SCHEMES.entries()) {
    for (const [widthIndex, width] of WIDTHS.entries()) {
      const lng: Lng = (kindIndex + schemeIndex + widthIndex) % 2 === 0
        ? 'vi'
        : 'en';
      test(`${kind} display ${scheme} ${width} ${lng}`, async ({ page }) => {
        test.setTimeout(60_000);
        await page.setViewportSize({ width, height: 900 });
        const external = await openProjectPage(page, kind, lng, scheme);
        await expect(page.locator('html'))
          .toHaveAttribute('data-scheme', scheme);
        await assertProjectPage(page, kind, lng, scheme, external);
      });
    }
  }
}

for (const [kindIndex, kind] of KINDS.entries()) {
  for (const viewer of SCHEMES) {
    const lng: Lng = kindIndex === 0 ? 'en' : 'vi';
    test(`${kind} display system ${viewer} 768 ${lng}`, async ({ page }) => {
      test.setTimeout(60_000);
      await page.emulateMedia({ colorScheme: viewer });
      await page.setViewportSize({ width: 768, height: 900 });
      const external = await openProjectPage(page, kind, lng, 'system');
      await expect(page.locator('html'))
        .toHaveAttribute('data-scheme', 'system');
      await assertProjectPage(page, kind, lng, viewer, external);
    });
  }
}

const openWide = (kind: ProjectPageKind) => async (page: Page) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await openProjectPage(page, kind, 'vi', 'light');
};

for (const kind of KINDS) {
  screenMutationTests(`${kind} page`, openWide(kind),
    projectContract(kind, 'vi', 'light'));
}

mutationTest('project list rejects a column off its measure', async ({
  page,
}) => {
  await openWide('project-list')(page);
  await proveMutation(() => expectProjectMeasure(page, 'project-list'),
    () => applyStyleMutation(page.locator('main#public-project'),
      { 'max-width': 'none' }), 'Project measure.');
});

mutationTest('project page rejects stacked columns on a wide screen', async ({
  page,
}) => {
  await openWide('project')(page);
  await proveMutation(() => expectProjectColumns(page),
    () => applyStyleMutation(page.locator('.project-columns'),
      { display: 'block' }), 'Project columns.');
});

mutationTest('project list rejects role and dates off the name line',
  async ({ page }) => {
    await openWide('project-list')(page);
    await proveMutation(() => expectRowMeta(page),
      () => applyStyleMutation(
        page.locator('.project-rows > li').nth(1)
          .locator('.project-row-meta'),
        { 'flex-basis': '100%' }), 'Project row meta.');
  });

mutationTest('project page rejects a color outside its scheme', async ({
  page,
}) => {
  await openWide('project')(page);
  await proveMutation(() => expectProjectColors(page, 'project', 'light'),
    () => applyStyleMutation(page.locator('.project-external-primary'),
      { 'background-color': 'rgb(255, 0, 0)' }), 'Project scheme colors.');
});

mutationTest('project page rejects a style attribute', async ({ page }) => {
  await openWide('project')(page);
  await proveMutation(() => expectProjectStylesheet(page),
    () => mutateAttribute(page.locator('main h1'), 'style', 'color: red'),
    'Project stylesheet.');
});

mutationTest('project list rejects a missing project stylesheet', async ({
  page,
}) => {
  await openWide('project-list')(page);
  await proveMutation(() => expectProjectStylesheet(page), async () => {
    const toggle = (disabled: boolean) => page.evaluate((value) => {
      for (const sheet of document.styleSheets) {
        if (sheet.href?.includes('/_nuxt/assets/project-pages.css')) {
          sheet.disabled = value;
        }
      }
    }, disabled);
    await toggle(true);
    return () => toggle(false);
  }, 'Project stylesheet.');
});

mutationTest('project page rejects an unnamed text section', async ({
  page,
}) => {
  await openWide('project')(page);
  await proveMutation(() => expectProjectStructure(page, 'project', 'vi'),
    () => mutateAttribute(page.locator('.project-outcome'),
      'aria-labelledby', 'missing-title'), 'Project page structure.');
});
