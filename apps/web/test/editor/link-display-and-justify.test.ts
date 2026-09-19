import { mount } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import type {
  Content,
  Customization,
  PersonalDetail,
} from '@aboutme/schema';
import { TEMPLATES } from '@aboutme/schema/templates';
import { computed, nextTick, ref } from 'vue';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import CustomizationPanel from
  '../../app/components/editor/customization/CustomizationPanel.vue';
import ContactList from '../../app/components/editor/forms/ContactList.vue';
import { applyTemplate } from '../../app/components/resume/applyTemplate';
import type { ResumeEditorActions } from
  '../../app/composables/useResumeEditor';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

const locale = ref<'vi' | 'en'>('en');
mockNuxtImport('useLocale', () => () => ({ locale }));

beforeEach(() => {
  locale.value = 'en';
});

function detail(overrides: Partial<PersonalDetail>): PersonalDetail {
  return {
    id: 'detail-1',
    type: 'github',
    value: 'https://github.com/ada',
    isHidden: false,
    ...overrides,
  };
}

function mountContacts(details: readonly PersonalDetail[]) {
  return mount(ContactList, {
    props: { details, createEntityId: () => 'detail-new' },
  });
}

describe('contact link display (ADR 0041)', () => {
  it.each([
    ['website', 'https://ada.dev'],
    ['linkedin', 'https://linkedin.com/in/ada'],
    ['github', 'https://github.com/ada'],
    ['twitter', 'https://x.com/ada'],
    ['custom', 'https://orcid.org/0000-0002-1825-0097'],
  ] as const)('offers Show as for a %s link', (type, value) => {
    const wrapper = mountContacts([detail({ type, value })]);
    const select = wrapper.get('[data-detail-display]');
    expect((select.element as HTMLSelectElement).value).toBe('short');
    expect(select.findAll('option').map((option) => option.text()))
      .toEqual(['Short address', 'Full address', 'Label']);
  });

  it.each([
    ['email', 'ada@example.com'],
    ['phone', '+84 90 000 0000'],
    ['location', 'Hà Nội'],
    ['custom', 'Available from May'],
    ['custom', 'HTTPS://orcid.org/0000'],
    ['custom', ' https://orcid.org/0000'],
  ] as const)('hides Show as for %s text %j', (type, value) => {
    const wrapper = mountContacts([detail({ type, value })]);
    expect(wrapper.find('[data-detail-display]').exists()).toBe(false);
  });

  it('stores full or label and drops the key for short', async () => {
    const wrapper = mountContacts([detail({})]);
    const select = wrapper.get('[data-detail-display]');

    await select.setValue('label');
    expect(wrapper.emitted('change')?.at(-1)?.[0]).toEqual([
      detail({ display: 'label' }),
    ]);

    await select.setValue('short');
    const last = (wrapper.emitted('change')?.at(-1)?.[0] ?? []) as
      PersonalDetail[];
    expect(last).toEqual([detail({})]);
    expect('display' in (last[0] ?? {})).toBe(false);
  });

  it('shows a stored display and ignores an unchanged choice', async () => {
    const wrapper = mountContacts([detail({ display: 'full' })]);
    const select = wrapper.get('[data-detail-display]');
    expect((select.element as HTMLSelectElement).value).toBe('full');

    await select.setValue('full');
    expect(wrapper.emitted('change')).toBeUndefined();
  });
});

function recordFor(textAlign?: 'left' | 'justify'): ResumeRecord {
  const current = acceptedFixture();
  const { textAlign: _stored, ...font } = current.document.customization.font;
  current.document.customization = {
    ...current.document.customization,
    font: textAlign === undefined ? font : { ...font, textAlign },
  };
  return {
    current,
    accepted: current,
    issues: {},
    pending: [],
    conflicts: [],
    sessionLost: false,
  } as ResumeRecord;
}

function mountDesign(record: ResumeRecord) {
  const edit = vi.fn();
  const wrapper = mount(CustomizationPanel, {
    props: {
      actions: {
        edit,
        record: computed(() => record),
      } as unknown as ResumeEditorActions,
      record,
    },
  });
  return {
    edit,
    select: wrapper.get('[data-field="font.textAlign"] select'),
    wrapper,
  };
}

describe('body text alignment (ADR 0041)', () => {
  it('shows Left for an absent value in the Type group', async () => {
    const { select, wrapper } = mountDesign(recordFor());
    expect((select.element as HTMLSelectElement).value).toBe('left');
    expect(select.findAll('option').map((option) => option.text()))
      .toEqual(['Left', 'Justify']);
    expect(wrapper.get('[data-field="font.textAlign"] label').text())
      .toBe('Text alignment');
    expect(
      wrapper.find('[data-customization-group="Type"] '
        + '[data-field="font.textAlign"]').exists(),
    ).toBe(true);

    locale.value = 'vi';
    await nextTick();

    expect(
      wrapper.find('[data-customization-group="Type"] '
        + '[data-field="font.textAlign"]').exists(),
    ).toBe(true);
  });

  it('sets justify and clears the key for left', async () => {
    const justify = mountDesign(recordFor());
    await justify.select.setValue('justify');
    expect(justify.edit).toHaveBeenCalledWith({
      kind: 'customization',
      deltas: [{ op: 'set', path: 'font.textAlign', value: 'justify' }],
    });

    const left = mountDesign(recordFor('justify'));
    await left.select.setValue('left');
    expect(left.edit).toHaveBeenCalledWith({
      kind: 'customization',
      deltas: [{ op: 'unset', path: 'font.textAlign' }],
    });
  });

  it('does not write left when the value is already absent', async () => {
    const { edit, select } = mountDesign(recordFor());
    await select.setValue('left');
    expect(edit).not.toHaveBeenCalled();
  });
});

describe('applyTemplate keeps text alignment (ADR 0041)', () => {
  const fixture = acceptedFixture().document;
  const content: Content = fixture.content;

  function withAlign(
    customization: Customization,
    textAlign?: 'left' | 'justify',
  ): Customization {
    const { textAlign: _stored, ...font } = customization.font;
    return {
      ...customization,
      font: textAlign === undefined ? font : { ...font, textAlign },
    };
  }

  it.each(TEMPLATES.map((preset) => [preset.id, preset] as const))(
    'carries justify over and keeps absence absent for %s',
    (_id, preset) => {
      const justified = withAlign(fixture.customization, 'justify');
      expect(applyTemplate(justified, preset, content).font.textAlign)
        .toBe('justify');

      const unset = withAlign(fixture.customization);
      expect('textAlign' in applyTemplate(unset, preset, content).font)
        .toBe(false);
    },
  );

  it('ignores a text alignment carried by a preset', () => {
    const preset = TEMPLATES[0]!;
    const withPresetAlign = {
      ...preset,
      customization: {
        ...preset.customization,
        font: { ...preset.customization.font, textAlign: 'justify' },
      },
    } as typeof preset;
    const next = applyTemplate(
      withAlign(fixture.customization),
      withPresetAlign,
      content,
    );
    expect('textAlign' in next.font).toBe(false);
  });
});
