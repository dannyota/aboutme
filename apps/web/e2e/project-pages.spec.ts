import { expect, test, type Page } from '@playwright/test';

import { expectScreen } from './display-assertions';
import {
  EVIDENCE_ARIA, EVIDENCE_COLORS, EVIDENCE_CONTAINMENT, EVIDENCE_CONTENT,
  EVIDENCE_COUNTS, EVIDENCE_ICONS, EVIDENCE_LINKS, EVIDENCE_PLACEMENT,
  EVIDENCE_SHAPE, EVIDENCE_TYPOGRAPHY, expectEvidenceAria,
  expectEvidenceColors, expectEvidenceContainment, expectEvidenceContent,
  expectEvidenceCounts, expectEvidenceIcons, expectEvidenceLinks,
  expectEvidencePlacement, expectEvidenceShape, expectEvidenceTypography,
  expectProjectEvidence, mutateEvidence,
} from './project-evidence-details';
import {
  expectProjectBarRow,
  expectProjectColors,
  expectProjectColumns,
  expectProjectMeasure,
  expectProjectShapes,
  expectProjectStructure,
  expectProjectStylesheet,
  expectRowMeta,
  type Lng,
  openProjectPage,
  PROJECT_BAR_ROW,
  PROJECT_SHAPES,
  projectContract,
  type ProjectPageKind,
  type Scheme,
} from './project-page-details';
import {
  applyStyleMutation, mutateAttribute, mutateText, mutationTest,
  proveMutation, screenMutationTests,
} from './style-mutation';

const KINDS: readonly ProjectPageKind[] = ['project-list', 'project'];
const SCHEMES: readonly Scheme[] = ['light', 'dark'];
const WIDTHS = [320, 768, 1280] as const;

async function assertProjectPage(
  page: Page, kind: ProjectPageKind, lng: Lng, scheme: Scheme,
  external: readonly string[], width: number,
): Promise<void> {
  expect(external).toEqual([]);
  await expect(page.locator('html')).toHaveAttribute('lang', lng);
  await expectProjectStylesheet(page);
  await expectScreen(page, projectContract(kind, lng, scheme, width));
  await expectProjectMeasure(page, kind);
  if (kind === 'project') {
    await expectProjectColumns(page);
    await expectProjectEvidence(page, lng, scheme);
  } else {
    await expectRowMeta(page);
  }
  await expectProjectColors(page, kind, scheme);
  await expectProjectShapes(page, kind);
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
        await assertProjectPage(page, kind, lng, scheme, external, width);
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
      await assertProjectPage(page, kind, lng, viewer, external, 768);
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

mutationTest('project page rejects a squared card', async ({ page }) => {
  await openWide('project')(page);
  await proveMutation(() => expectProjectShapes(page, 'project'),
    () => applyStyleMutation(page.locator('.project-facts'),
      { 'border-radius': '10px' }), PROJECT_SHAPES);
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

test.describe('project bar at 360 px', () => {
  test.use({ hasTouch: true });

  for (const [kind, lng] of [
    ['project-list', 'vi'], ['project', 'en'],
  ] as const) {
    test(`${kind} keeps the credit and back link on one row ${lng}`,
      async ({ page }) => {
        await page.setViewportSize({ width: 360, height: 900 });
        await openProjectPage(page, kind, lng, 'light');
        await expectProjectBarRow(page);
      });
  }

  mutationTest('project bar rejects a wrapped row', async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 900 });
    await openProjectPage(page, 'project', 'vi', 'light');
    await proveMutation(() => expectProjectBarRow(page),
      () => applyStyleMutation(page.locator('.public-toolbar-inner'),
        { 'column-gap': '200px' }), PROJECT_BAR_ROW);
  });
});

const openEvidence = (width: number) => async (page: Page) => {
  await page.setViewportSize({ width, height: 900 });
  await openProjectPage(page, 'project', 'vi', 'light');
};

for (const [name, width, assertion, mutate, message] of [
  ['a mutated label', 1280, (page: Page) => expectEvidenceContent(page, 'vi'),
    (page: Page) => mutateText(page.locator('.project-evidence-label')
      .first(), 'MUTATED'), EVIDENCE_CONTENT],
  ['reordered rows', 1280, (page: Page) => expectEvidenceContent(page, 'vi'),
    (page: Page) => mutateEvidence(page, 'reorder'), EVIDENCE_CONTENT],
  ['a label on a plain row', 1280,
    (page: Page) => expectEvidenceContent(page, 'vi'),
    (page: Page) => mutateEvidence(page, 'plainLabel'), EVIDENCE_CONTENT],
  ['a weaker rel', 1280, (page: Page) => expectEvidenceLinks(page, 'vi'),
    (page: Page) => mutateAttribute(page.locator('a.project-evidence')
      .first(), 'rel', 'noopener'), EVIDENCE_LINKS],
  ['an added target', 1280, (page: Page) => expectEvidenceLinks(page, 'vi'),
    (page: Page) => mutateAttribute(page.locator('a.project-evidence')
      .first(), 'target', '_blank'), EVIDENCE_LINKS],
  ['a section moved first', 768, expectEvidencePlacement,
    (page: Page) => applyStyleMutation(
      page.locator('.project-evidence-section'), { order: '-1' }),
    EVIDENCE_PLACEMENT],
  ['a section inside the facts panel', 1280, expectEvidencePlacement,
    (page: Page) => mutateEvidence(page, 'intoFacts'), EVIDENCE_PLACEMENT],
  ['a duplicated row', 1280, expectEvidenceCounts,
    (page: Page) => mutateEvidence(page, 'duplicate'), EVIDENCE_COUNTS],
  ['a red link', 1280, (page: Page) => expectEvidenceColors(page, 'light'),
    (page: Page) => applyStyleMutation(page.locator('a.project-evidence')
      .first(), { color: 'rgb(255, 0, 0)' }), EVIDENCE_COLORS],
  ['larger meta text', 1280, expectEvidenceTypography,
    (page: Page) => applyStyleMutation(page.locator('.project-evidence-meta')
      .first(), { 'font-size': '16px' }), EVIDENCE_TYPOGRAPHY],
  ['a squared card', 1280, expectEvidenceShape,
    (page: Page) => applyStyleMutation(page.locator('.project-evidence-list'),
      { 'border-radius': '10px' }), EVIDENCE_SHAPE],
  ['a removed icon', 1280, expectEvidenceIcons,
    (page: Page) => mutateEvidence(page, 'removeIcon'), EVIDENCE_ICONS],
  ['a wide icon', 1280, expectEvidenceIcons,
    (page: Page) => applyStyleMutation(
      page.locator('.project-evidence-heading > svg').first(),
      { width: '24px' }), EVIDENCE_ICONS],
  ['an unnamed region', 1280, (page: Page) => expectEvidenceAria(page, 'vi'),
    (page: Page) => mutateAttribute(page.locator('.project-evidence-section'),
      'aria-labelledby', 'missing-title'), EVIDENCE_ARIA],
  ['an unwrapped long title', 320, expectEvidenceContainment,
    (page: Page) => applyStyleMutation(page.locator('a.project-evidence')
      .nth(1), { 'white-space': 'nowrap' }), EVIDENCE_CONTAINMENT],
] as const) {
  mutationTest(`project evidence rejects ${name}`, async ({ page }) => {
    await openEvidence(width)(page);
    await proveMutation(() => assertion(page), () => mutate(page), message);
  });
}
