import { mount } from '@vue/test-utils';
import type { PersonalDetails } from '@aboutme/schema';
import { computed } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import ContactList from '../../app/components/editor/forms/ContactList.vue';
import DateRangeField from
  '../../app/components/editor/forms/DateRangeField.vue';
import OptionalField from '../../app/components/editor/forms/OptionalField.vue';
import PersonalDetailsPanel from
  '../../app/components/editor/forms/PersonalDetailsPanel.vue';
import YearMonthField from
  '../../app/components/editor/forms/YearMonthField.vue';
import type { ResumeEditorActions } from
  '../../app/composables/useResumeEditor';

describe('OptionalField', () => {
  it('does not turn an untouched absent field into a command', async () => {
    const wrapper = mount(OptionalField, {
      props: { label: 'Name', modelValue: undefined },
    });

    await wrapper.get('input').trigger('blur');

    expect(wrapper.emitted('intent')).toBeUndefined();
  });

  it('emits a set transition for typed text', async () => {
    const wrapper = mount(OptionalField, {
      props: { label: 'Name', modelValue: undefined },
    });

    await wrapper.get('input').setValue('Ada');
    await wrapper.get('input').trigger('blur');

    expect(wrapper.emitted('intent')?.at(-1)?.[0]).toEqual({
      kind: 'set',
      value: 'Ada',
    });
  });

  it.each([
    ['clear', { kind: 'clear', value: '' }],
    ['unset', { kind: 'unset' }],
  ] as const)('emits the %s transition exactly', async (action, expected) => {
    const wrapper = mount(OptionalField, {
      props: { label: 'Name', modelValue: 'Ada' },
    });

    await wrapper.get(`[data-action="${action}"]`).trigger('click');
    await wrapper.get('input').trigger('blur');

    expect(wrapper.emitted('intent')?.at(-1)?.[0]).toEqual(expected);
  });

  it('commits clear directly from its button action', async () => {
    const wrapper = mount(OptionalField, {
      props: { label: 'Name', modelValue: 'Ada' },
    });

    await wrapper.get('[data-action="clear"]').trigger('click');

    expect(wrapper.emitted('intent')?.at(-1)?.[0]).toEqual({
      kind: 'clear',
      value: '',
    });
  });

  it(
    'commits Set once from its button action and ignores a later blur',
    async () => {
      const wrapper = mount(OptionalField, {
        props: { label: 'Name', modelValue: undefined },
      });

      await wrapper.get('input').setValue('Ada');
      await wrapper.get('[data-action="set"]').trigger('click');
      expect(wrapper.emitted('intent')).toEqual([[
        { kind: 'set', value: 'Ada' },
      ]]);
      await wrapper.get('input').trigger('blur');

      expect(wrapper.emitted('intent')).toEqual([[
        { kind: 'set', value: 'Ada' },
      ]]);
    },
  );

  it(
    'commits remove once from its button action and ignores a later blur',
    async () => {
      const wrapper = mount(OptionalField, {
        props: { label: 'Name', modelValue: 'Ada' },
      });

      await wrapper.get('[data-action="unset"]').trigger('click');
      await wrapper.get('input').trigger('blur');

      expect(wrapper.emitted('intent')).toEqual([[{ kind: 'unset' }]]);
    },
  );
});

describe('YearMonthField', () => {
  it('preserves a month value while capturing an exact date', async () => {
    const wrapper = mount(YearMonthField, {
      props: { fieldId: 'date', label: 'Date', modelValue: undefined },
    });

    await wrapper.get('[data-part="year"]').setValue('2026');
    await wrapper.get('[data-part="month"]').setValue('1');
    await wrapper.get('[data-part="month"]').trigger('blur');

    expect(wrapper.emitted('intent')?.at(-1)?.[0]).toEqual({
      kind: 'set',
      value: { y: 2026, m: 1 },
    });
  });

  it('does not capture fractional or non-finite numeric values', async () => {
    const wrapper = mount(YearMonthField, {
      props: { fieldId: 'date', label: 'Date', modelValue: undefined },
    });

    await wrapper.get('[data-part="year"]').setValue('2026.5');
    await wrapper.get('[data-part="year"]').trigger('blur');

    expect(wrapper.emitted('intent')).toBeUndefined();
  });

  it('unsets a present date without emitting an invalid clear', async () => {
    const wrapper = mount(YearMonthField, {
      props: { fieldId: 'date', label: 'Date', modelValue: { y: 2026 } },
    });

    await wrapper.get('[data-action="unset"]').trigger('click');

    expect(wrapper.emitted('intent')?.at(-1)?.[0]).toEqual({ kind: 'unset' });
    expect(wrapper.find('[data-action="clear"]').exists()).toBe(false);
  });
});

describe('DateRangeField', () => {
  it('captures present ranges with a null end', async () => {
    const wrapper = mount(DateRangeField, {
      props: { fieldId: 'work-dates', modelValue: undefined },
    });

    await wrapper.get('[data-part="start-year"]').setValue('2024');
    await wrapper.get('[data-part="start-month"]').setValue('1');
    await wrapper.get('[data-part="present"]').setValue(true);
    await wrapper.get('[data-part="present"]').trigger('change');

    expect(wrapper.emitted('intent')?.at(-1)?.[0]).toEqual({
      kind: 'set',
      value: { start: { y: 2024, m: 1 }, end: null, present: true },
    });
  });

  it('requires a start date before accepting present', async () => {
    const wrapper = mount(DateRangeField, {
      props: { fieldId: 'work-dates', modelValue: undefined },
    });

    await wrapper.get('[data-part="present"]').setValue(true);
    await wrapper.get('[data-part="present"]').trigger('change');

    expect(wrapper.get('[data-error="date-order"]').text()).toBe(
      'Enter a valid start date.',
    );
    expect(wrapper.emitted('intent')).toBeUndefined();
  });

  it('does not unset an existing range when present has no start', async () => {
    const wrapper = mount(DateRangeField, {
      props: {
        fieldId: 'work-dates',
        modelValue: {
          start: { y: 2024 }, end: { y: 2025 }, present: false,
        },
      },
    });

    await wrapper.get('[data-part="start-year"]').setValue('');
    await wrapper.get('[data-part="end-year"]').setValue('');
    await wrapper.get('[data-part="present"]').setValue(true);
    await wrapper.get('[data-part="present"]').trigger('change');

    expect(wrapper.get('[data-error="date-order"]').text()).toBe(
      'Enter a valid start date.',
    );
    expect(wrapper.emitted('intent')).toBeUndefined();
  });

  it(
    'shows a local order error instead of emitting start-after-end',
    async () => {
      const wrapper = mount(DateRangeField, {
        props: {
          fieldId: 'work-dates',
          modelValue: {
            start: { y: 2026, m: 2 },
            end: { y: 2025, m: 12 },
            present: false,
          },
        },
      });

      await wrapper.get('[data-part="end-month"]').setValue('12');
      await wrapper.get('[data-part="end-month"]').trigger('blur');

      expect(wrapper.get('[data-error="date-order"]').text()).toBe(
        'Start date must not be after end date.',
      );
      expect(wrapper.get('fieldset').attributes('aria-describedby')).toBe(
        'work-dates-error',
      );
      expect(wrapper.emitted('intent')).toBeUndefined();
    },
  );
});

describe('PersonalDetailsPanel', () => {
  it(
    'captures full name and headline intent through the action boundary',
    async () => {
      const edit = vi.fn();
      const wrapper = mount(PersonalDetailsPanel, {
        props: { actions: actionsFor(edit), personal: {} },
      });

      await wrapper.get('[data-field="fullName"] input').setValue('Ada');
      await wrapper.get('[data-field="fullName"] input').trigger('blur');
      await wrapper.get('[data-field="headline"] input').setValue('Engineer');
      await wrapper.get('[data-field="headline"] input').trigger('blur');

      expect(edit).toHaveBeenNthCalledWith(1, {
        kind: 'personalField',
        path: 'fullName',
        value: { present: true, value: 'Ada' },
      });
      expect(edit).toHaveBeenNthCalledWith(2, {
        kind: 'personalField',
        path: 'headline',
        value: { present: true, value: 'Engineer' },
      });
    },
  );

  it(
    'preserves clear versus unset at the personal action boundary',
    async () => {
      const edit = vi.fn();
      const wrapper = mount(PersonalDetailsPanel, {
        props: { actions: actionsFor(edit), personal: { fullName: 'Ada' } },
      });

      await wrapper
        .get('[data-field="fullName"] [data-action="clear"]')
        .trigger('click');
      await wrapper
        .get('[data-field="fullName"] [data-action="unset"]')
        .trigger('click');

      expect(edit).toHaveBeenNthCalledWith(1, {
        kind: 'personalField',
        path: 'fullName',
        value: { present: true, value: '' },
      });
      expect(edit).toHaveBeenNthCalledWith(2, {
        kind: 'personalField',
        path: 'fullName',
        value: { present: false },
      });
    },
  );

  it('captures ordered contact edits through the action boundary', async () => {
    const edit = vi.fn();
    const wrapper = mount(PersonalDetailsPanel, {
      props: { actions: actionsFor(edit), personal: {} },
    });

    await wrapper.get('[data-action="add-detail"]').trigger('click');
    await wrapper.get('[data-detail-value]').setValue('https://example.test');
    await wrapper.get('[data-detail-value]').trigger('blur');
    await wrapper.get('[data-action="add-detail"]').trigger('click');
    await wrapper
      .findAll('[data-action="move-detail-up"]')[1]
      ?.trigger('click');

    expect(edit).toHaveBeenLastCalledWith({
      kind: 'personalField',
      path: 'details',
      value: {
        present: true,
        value: [
          {
            id: 'detail-2',
            type: 'email',
            value: '',
            isHidden: false,
          },
          {
            id: 'detail-1',
            type: 'email',
            value: 'https://example.test',
            isHidden: false,
          },
        ],
      },
    });
  });

  it('allocates one immutable contact ID at Add', async () => {
    const edit = vi.fn();
    const actions = actionsFor(edit);
    const wrapper = mount(PersonalDetailsPanel, {
      props: { actions, personal: {} },
    });

    await wrapper.get('[data-action="add-detail"]').trigger('click');

    expect(actions.createEntityId).toHaveBeenCalledOnce();
    expect(wrapper.get('[data-detail-id]').text()).toBe('detail-1');
    expect(wrapper.find('input[value="detail-1"]').exists()).toBe(false);
  });

  it('does not allocate a seventeenth contact detail', async () => {
    const createEntityId = vi.fn(() => 'detail-17');
    const wrapper = mount(ContactList, {
      props: {
        createEntityId,
        details: Array.from({ length: 16 }, (_, index) => ({
          id: `detail-${index + 1}`,
          type: 'email' as const,
          value: '',
          isHidden: false,
        })),
      },
    });

    await wrapper.get('[data-action="add-detail"]').trigger('click');

    expect(createEntityId).not.toHaveBeenCalled();
    expect(wrapper.get('[data-error="detail-limit"]').text()).toBe(
      'You can add up to 16 contact details.',
    );
    expect(wrapper.emitted('change')).toBeUndefined();
  });

  it(
    'retains accepted siblings and server-owned photo while editing details',
    async () => {
      const edit = vi.fn();
      const personal: PersonalDetails = {
        fullName: 'Ada',
        photo: { key: 'private-object-key' },
        details: [
          {
            id: 'detail-1',
            type: 'website',
            value: 'https://example.test',
            isHidden: false,
          },
        ],
      };
      const wrapper = mount(PersonalDetailsPanel, {
        props: { actions: actionsFor(edit), personal },
      });

      await wrapper.get('[data-detail-label]').setValue('Site');
      await wrapper.get('[data-detail-label]').trigger('blur');

      expect(edit).toHaveBeenCalledWith({
        kind: 'personalField',
        path: 'details',
        value: {
          present: true,
          value: [
            {
              id: 'detail-1',
              type: 'website',
              label: 'Site',
              value: 'https://example.test',
              isHidden: false,
            },
          ],
        },
      });
      expect(wrapper.text()).not.toContain('private-object-key');
    },
  );

  it.each([
    'email',
    'phone',
    'location',
    'website',
    'linkedin',
    'github',
    'twitter',
    'custom',
  ] as const)('accepts the supported %s contact type', async (type) => {
    const detail = {
      id: 'detail-1',
      type: type === 'email' ? 'phone' : 'email',
      value: '',
      isHidden: false,
    };
    const wrapper = mount(ContactList, {
      props: { createEntityId: () => 'detail-2', details: [detail] },
    });

    await wrapper.get('[data-detail-type]').setValue(type);
    await wrapper.get('[data-detail-type]').trigger('change');

    expect(wrapper.emitted('change')?.at(-1)?.[0]).toEqual([
      {
        ...detail,
        type,
      },
    ]);
  });

  it('rejects non-lowercase web profile URLs before capture', async () => {
    const wrapper = mount(ContactList, {
      props: {
        details: [
          {
            id: 'detail-1',
            type: 'github',
            value: '',
            isHidden: false,
          },
        ],
        createEntityId: () => 'detail-2',
      },
    });

    await wrapper.get('[data-detail-value]').setValue('HTTPS://github.com/ada');
    await wrapper.get('[data-detail-value]').trigger('blur');

    expect(wrapper.get('[data-error="contact-url"]').text()).toBe(
      'Use a lowercase https:// URL.',
    );
    expect(wrapper.emitted('change')).toBeUndefined();
  });

  it(
    'does not emit when an absent contact label only receives focus and blur',
    async () => {
      const wrapper = mount(ContactList, {
        props: {
          createEntityId: () => 'detail-2',
          details: [{
            id: 'detail-1', type: 'email', value: '', isHidden: false,
          }],
        },
      });

      await wrapper.get('[data-detail-label]').trigger('focus');
      await wrapper.get('[data-detail-label]').trigger('blur');

      expect(wrapper.emitted('change')).toBeUndefined();
    },
  );

  it(
    'rejects a web-profile type change that carries an invalid value',
    async () => {
      const wrapper = mount(ContactList, {
        props: {
          details: [
            {
              id: 'detail-1',
              type: 'email',
              value: 'ada@example.test',
              isHidden: false,
            },
          ],
          createEntityId: () => 'detail-2',
        },
      });

      await wrapper.get('[data-detail-type]').setValue('github');
      await wrapper.get('[data-detail-type]').trigger('change');

      expect(wrapper.get('[data-error="contact-url"]').text()).toBe(
        'Use a lowercase https:// URL.',
      );
      expect(wrapper.emitted('change')).toBeUndefined();
    },
  );

  it(
    'keeps raw server messages out of mapped issue text and focuses the field',
    async () => {
      const edit = vi.fn();
      const wrapper = mount(PersonalDetailsPanel, {
        props: {
          actions: actionsFor(edit, {
            'personalDetails.fullName': [
              {
                path: 'personalDetails.fullName',
                code: 'max_length',
              },
            ],
          }),
          personal: {},
        },
        attachTo: document.body,
      });

      const input = wrapper.get('[data-field="fullName"] input');
      const issue = wrapper.get('[data-issue="personalDetails.fullName"]');
      expect(issue.text()).toBe('This value is too long.');
      await wrapper
        .get('[data-issue="personalDetails.fullName"]')
        .trigger('click');
      expect(document.activeElement).toBe(input.element);
    },
  );

  it(
    'focuses the exact contact control for a mapped server issue',
    async () => {
      const wrapper = mount(PersonalDetailsPanel, {
        props: {
          actions: actionsFor(vi.fn(), {
            contact: [{
              path: 'personalDetails.details[0].value', code: 'format',
            }],
          }),
          personal: {
            details: [{
              id: 'detail-1', type: 'email', value: '', isHidden: false,
            }],
          },
        },
        attachTo: document.body,
      });

      const input = wrapper.get('[data-detail-index="0"] [data-detail-value]');
      await wrapper
        .get('[data-issue="personalDetails.details[0].value"]')
        .trigger('click');

      expect(document.activeElement).toBe(input.element);
    },
  );

  it.each([
    ['label', '[data-detail-label]'],
    ['type', '[data-detail-type]'],
    ['isHidden', '[data-detail-is-hidden]'],
  ] as const)(
    'focuses the mapped contact %s control',
    async (field, selector) => {
      const path = `personalDetails.details[0].${field}`;
      const wrapper = mount(PersonalDetailsPanel, {
        props: {
          actions: actionsFor(vi.fn(), {
            contact: [{ path, code: 'format' }],
          }),
          personal: {
            details: [{
              id: 'detail-1', type: 'email', value: '', isHidden: false,
            }],
          },
        },
        attachTo: document.body,
      });

      const input = wrapper.get(`[data-detail-index="0"] ${selector}`);
      await wrapper.get(`[data-issue="${path}"]`).trigger('click');

      expect(document.activeElement).toBe(input.element);
    },
  );

  it('hides a hostile unmapped path while retaining a safe message', () => {
    const hostilePath = 'personalDetails.details[0].value <img src=x>';
    const wrapper = mount(PersonalDetailsPanel, {
      props: {
        actions: actionsFor(vi.fn(), {
          contact: [{
            path: hostilePath, code: 'format',
          }],
        }),
        personal: {},
      },
    });

    expect(wrapper.text()).not.toContain(hostilePath);
    expect(wrapper.text()).toContain('Enter a value in the required format.');
  });
});

function actionsFor(
  edit: ReturnType<typeof vi.fn>,
  issues: Record<
    string,
    readonly { readonly code: string; readonly path: string }[]
  > = {},
): ResumeEditorActions {
  return {
    record: computed(() => ({ issues })),
    createEntityId: vi
      .fn()
      .mockReturnValueOnce('detail-1')
      .mockReturnValueOnce('detail-2'),
    edit,
  } as unknown as ResumeEditorActions;
}
