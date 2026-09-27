// One entry row: the label covers the whole text column, at least 44 px
// high, so the whole row is the hit area (docs/design/
// linkedin-import-ui.md, "Review state").
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { describe, expect, it } from 'vitest';

import ImportEntryRow from '../../../app/components/import/ImportEntryRow.vue';
import type { ReviewEntry } from '../../../app/import/linkedin/build';
import { importCopy } from '../../../app/i18n/import';

function workEntry(): ReviewEntry {
  return {
    id: 'entry-1',
    section: 'work',
    entry: {
      id: 'entry-1',
      jobTitle: 'Engineer',
      employer: 'Example Co.',
      dates: { start: { y: 2020, m: 1 }, end: null, present: true },
    },
    marks: [],
    selectedByDefault: true,
  };
}

describe('ImportEntryRow', () => {
  it('keeps the row at least 44 px high', async () => {
    const wrapper = await mountSuspended(ImportEntryRow, {
      props: {
        copy: importCopy.en,
        entry: workEntry(),
        locale: 'en',
        dateFormat: 'Mon YYYY',
        invalidEntryIds: new Set<string>(),
        modelValue: false,
        disabled: false,
      },
    });
    expect(wrapper.get('li').classes()).toContain('min-h-11');
  });

  it('nests the description inside the checkbox label, not just the '
    + 'heading, so the whole row toggles', async () => {
    const wrapper = await mountSuspended(ImportEntryRow, {
      props: {
        copy: importCopy.en,
        entry: workEntry(),
        locale: 'en',
        dateFormat: 'Mon YYYY',
        invalidEntryIds: new Set<string>(),
        modelValue: false,
        disabled: false,
      },
    });
    const label = wrapper.get('label');
    const checkbox = wrapper.get('[role="checkbox"]');
    expect(label.attributes('for')).toBe(checkbox.attributes('id'));

    const heading = wrapper.findAll('span')
      .find((span) => span.text() === 'Engineer · Example Co.');
    const description = wrapper.findAll('span')
      .find((span) => span.text().startsWith('Jan 2020'));
    expect(heading?.element.closest('label')).toBe(label.element);
    expect(description?.element.closest('label')).toBe(label.element);
  });

  it('marks an invalid entry with entryInvalid inside the label',
    async () => {
      const entry = workEntry();
      const wrapper = await mountSuspended(ImportEntryRow, {
        props: {
          copy: importCopy.en,
          entry,
          locale: 'en',
          dateFormat: 'Mon YYYY',
          invalidEntryIds: new Set([entry.id]),
          modelValue: true,
          disabled: false,
        },
      });
      const label = wrapper.get('label');
      expect(label.text()).toContain(importCopy.en.entryInvalid);
    });
});
