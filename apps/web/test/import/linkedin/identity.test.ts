import { mountSuspended } from '@nuxt/test-utils/runtime';
import type { VueWrapper } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import ImportActionPanel
  from '../../../app/components/import/ImportActionPanel.vue';
import ImportEntryRow from '../../../app/components/import/ImportEntryRow.vue';
import ImportPick from '../../../app/components/import/ImportPick.vue';
import ImportReview from '../../../app/components/import/ImportReview.vue';
import {
  buildReview,
  type ReviewEntry,
} from '../../../app/import/linkedin/build';
import { initialChoiceSets } from '../../../app/import/linkedin/pageState';
import { importCopy } from '../../../app/i18n/import';
import { resumeCreateCopy } from '../../../app/i18n/resume-create';

function pickProps(overrides: Record<string, unknown> = {}) {
  return {
    copy: importCopy.en,
    error: null,
    stopped: false,
    reading: false,
    fileName: null,
    pagesRead: 0,
    pageCount: null,
    ...overrides,
  };
}

function panelProps(overrides: Record<string, unknown> = {}) {
  return {
    copy: importCopy.en,
    createCopy: resumeCreateCopy.en,
    n: 1,
    s: 1,
    bytes: 100,
    sizeOver: false,
    schemaCheck: { ok: true } as const,
    creating: false,
    createErrorMessage: null,
    uncertain: false,
    ...overrides,
  };
}

function expectCard(classes: string[]): void {
  for (const name of [
    'rounded-[var(--radius-card)]', 'border', 'border-border', 'bg-card',
  ]) {
    expect(classes, name).toContain(name);
  }
  expect(classes).not.toContain('rounded-lg');
  expect(classes.filter((name) => /shadow/u.test(name))).toEqual([]);
}

function zone(wrapper: VueWrapper) {
  return wrapper.get('section[aria-labelledby="import-pick-heading"]');
}

describe('import steps card', () => {
  it('is a flat card on the card radius', async () => {
    const wrapper = await mountSuspended(ImportPick, { props: pickProps() });
    expectCard(wrapper
      .get('section[aria-labelledby="import-steps-heading"]').classes());
  });

  it('numbers each step in an ink figure on a mist circle', async () => {
    const wrapper = await mountSuspended(ImportPick, { props: pickProps() });
    const figures = wrapper.findAll('ol li > span[aria-hidden="true"]');
    expect(figures.map((figure) => figure.text())).toEqual([
      '1', '2', '3', '4',
    ]);
    for (const figure of figures) {
      for (const name of [
        'size-6', 'rounded-full', 'bg-surface-mist', 'text-foreground',
        'text-sm', 'font-semibold',
      ]) {
        expect(figure.classes(), name).toContain(name);
      }
      expect(figure.classes()).not.toContain('text-link');
    }
  });

  it('draws the note, file and privacy icons in muted ink', async () => {
    const wrapper = await mountSuspended(ImportPick, { props: pickProps() });
    const steps = wrapper
      .get('section[aria-labelledby="import-steps-heading"]');
    const privacy = wrapper.element.lastElementChild!;
    const icons = [
      steps.get('p svg').element,
      zone(wrapper).get('svg').element,
      privacy.querySelector('svg')!,
    ];
    for (const icon of icons) {
      expect(icon.classList.contains('text-muted-foreground')).toBe(true);
    }
  });
});

describe('import drop zone', () => {
  it('rests as a dashed control edge on mist with the card radius',
    async () => {
      const wrapper = await mountSuspended(ImportPick, { props: pickProps() });
      const classes = zone(wrapper).classes();
      for (const name of [
        'rounded-[var(--radius-card)]', 'border-2', 'border-dashed',
        'border-control', 'bg-surface-mist', 'transition-colors',
        'duration-150', 'motion-reduce:transition-none',
      ]) {
        expect(classes, name).toContain(name);
      }
      expect(classes).not.toContain('rounded-lg');
    });

  it('turns solid ink on the accent fill while a file is over it',
    async () => {
      const wrapper = await mountSuspended(ImportPick, { props: pickProps() });
      await zone(wrapper).trigger('dragover');
      const classes = zone(wrapper).classes();
      for (const name of [
        'border-2', 'border-solid', 'border-foreground', 'bg-accent',
      ]) {
        expect(classes, name).toContain(name);
      }
      expect(classes).not.toContain('border-dashed');
      expect(classes).not.toContain('bg-surface-mist');
      expect(zone(wrapper).text()).toContain(importCopy.en.dropActive);

      await zone(wrapper).trigger('dragleave');
      expect(zone(wrapper).classes()).toContain('border-dashed');
      expect(zone(wrapper).classes()).toContain('bg-surface-mist');
    });

  it('fills reading progress in the chart bar', async () => {
    const wrapper = await mountSuspended(ImportPick, {
      props: pickProps({
        reading: true, fileName: 'profile.pdf', pagesRead: 1, pageCount: 2,
      }),
    });
    const fill = wrapper.get('[role="progressbar"] > div');
    expect(fill.classes()).toContain('bg-chart-bar');
    expect(fill.classes()).not.toContain('bg-primary');
  });
});

describe('import review cards', () => {
  it('draws every review card flat on the card radius', async () => {
    let n = 0;
    const review = buildReview({
      profile: { fullName: 'Sample Person', headline: 'Engineer' },
      contacts: [{ type: 'email', value: 'sample@example.com' }],
      work: [],
      education: [],
      skills: ['Go'],
      languages: [],
      certificates: [],
      dropped: [{ heading: 'Honors-Awards', count: 1 }],
    }, () => {
      n += 1;
      return `00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;
    });
    const choices = initialChoiceSets(review);
    const wrapper = await mountSuspended(ImportReview, {
      props: {
        copy: importCopy.en,
        createCopy: resumeCreateCopy.en,
        locale: 'en',
        review,
        dateFormat: 'Mon YYYY',
        title: 'LinkedIn resume',
        fullName: review.fullName,
        headline: review.headline,
        detailIds: choices.detailIds,
        entryIds: choices.entryIds,
        schemaCheck: { ok: true },
        disabled: false,
        titleInvalid: false,
      },
    });
    const cards = wrapper.findAll('section');
    expect(cards.map((card) => card.attributes('aria-labelledby'))).toEqual([
      'import-resume-heading',
      'import-personal-heading',
      'import-section-skill-heading',
      'import-not-imported-heading',
    ]);
    for (const card of cards) expectCard(card.classes());
  });

  it('marks an entry notice with a muted info icon', async () => {
    const entry: ReviewEntry = {
      id: 'entry-1',
      section: 'work',
      entry: {
        id: 'entry-1',
        jobTitle: 'Engineer',
        employer: 'Example Co.',
        dates: { start: { y: 2020, m: 1 }, end: null, present: true },
      },
      marks: [{ kind: 'noDates' }],
      selectedByDefault: true,
    };
    const wrapper = await mountSuspended(ImportEntryRow, {
      props: {
        copy: importCopy.en,
        entry,
        locale: 'en',
        dateFormat: 'Mon YYYY',
        invalidEntryIds: new Set<string>(),
        modelValue: true,
        disabled: false,
      },
    });
    expect(wrapper.text()).toContain(importCopy.en.entryNoDates);
    expect(wrapper.get('label svg').classes())
      .toContain('text-muted-foreground');
  });
});

describe('import action panel', () => {
  it('is a flat card from 900 px and a flat bar below it', async () => {
    const wrapper = await mountSuspended(ImportActionPanel, {
      props: panelProps(),
    });
    const classes = wrapper.get('aside').classes();
    for (const name of [
      'bg-card', 'border-t', 'min-[900px]:border',
      'min-[900px]:rounded-[var(--radius-card)]',
    ]) {
      expect(classes, name).toContain(name);
    }
    expect(classes).not.toContain('min-[900px]:rounded-lg');
    expect(classes.filter((name) => /shadow/u.test(name))).toEqual([]);
  });

  it('fills the size meter in the chart bar on the muted track', async () => {
    const wrapper = await mountSuspended(ImportActionPanel, {
      props: panelProps(),
    });
    const meter = wrapper.get('[role="meter"]');
    expect(meter.classes()).toContain('bg-input');
    expect(meter.get('div').classes()).toContain('bg-chart-bar');
    expect(meter.get('div').classes()).not.toContain('bg-primary');
  });

  it('fills the meter in destructive over the limit', async () => {
    const wrapper = await mountSuspended(ImportActionPanel, {
      props: panelProps({ bytes: 300000, sizeOver: true }),
    });
    const fill = wrapper.get('[role="meter"] > div');
    expect(fill.classes()).toContain('bg-destructive');
    expect(fill.classes()).not.toContain('bg-chart-bar');
  });
});
