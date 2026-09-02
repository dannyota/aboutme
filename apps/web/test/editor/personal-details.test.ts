import { mount } from '@vue/test-utils';
import type { PersonalDetails } from '@aboutme/schema';
import { computed } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import ContactList from '../../app/components/editor/forms/ContactList.vue';
import PersonalDetailsPanel from
  '../../app/components/editor/forms/PersonalDetailsPanel.vue';
import type { ResumeEditorActions } from
  '../../app/composables/useResumeEditor';

describe('PersonalDetailsPanel', () => {
  it('sets a typed name on blur and unsets an emptied one', async () => {
    const edit = vi.fn(() => ({ kind: 'enqueued' as const }));
    const wrapper = mount(PersonalDetailsPanel, {
      props: { actions: actionsFor(edit), personal: { fullName: 'Ada' } },
    });
    const name = wrapper.get('[data-field="fullName"] [data-field-input]');
    await name.setValue('Ada Lovelace');
    await name.trigger('blur');
    expect(edit).toHaveBeenLastCalledWith({
      kind: 'personalField', path: 'fullName',
      value: { present: true, value: 'Ada Lovelace' },
    });
    await wrapper.setProps({ personal: { fullName: 'Ada Lovelace' } });
    await name.setValue('');
    await name.trigger('blur');
    expect(edit).toHaveBeenLastCalledWith({
      kind: 'personalField',
      path: 'fullName',
      value: { present: false },
    });
  });

  it('never emits a clear intent', async () => {
    const edit = vi.fn(() => ({ kind: 'enqueued' as const }));
    const wrapper = mount(PersonalDetailsPanel, {
      props: { actions: actionsFor(edit), personal: { headline: 'Engineer' } },
    });
    const headline = wrapper.get(
      '[data-field="headline"] [data-field-input]',
    );
    await headline.setValue('');
    await headline.trigger('keydown', { key: 'Enter' });
    expect(edit.mock.calls.every(([command]) =>
      !('value' in command)
      || command.value.present === false
      || command.value.value !== '',
    )).toBe(true);
  });

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
    expect(wrapper.get('[data-detail-id]').text()).toBe('Contact detail 1');
    expect(wrapper.get('[data-detail-id]').attributes('data-detail-id'))
      .toBe('detail-1');
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

  it('unsets a present label and toggles hidden by checkbox role', async () => {
    const wrapper = mount(ContactList, {
      props: {
        createEntityId: () => 'detail-2',
        details: [{
          id: 'detail-1', type: 'email', value: 'a@b.c', label: 'Work',
          isHidden: false,
        }],
      },
    });

    const label = wrapper.get('[data-detail-label]');
    await label.setValue('');
    await label.trigger('blur');
    expect(wrapper.emitted('change')?.at(-1)?.[0]).toEqual([{
      id: 'detail-1', type: 'email', value: 'a@b.c', isHidden: false,
    }]);

    const hidden = wrapper.get('[data-detail-is-hidden]');
    expect(hidden.attributes('role')).toBe('checkbox');
    expect(hidden.attributes('aria-checked')).toBe('false');
    await hidden.trigger('click');
    expect(hidden.attributes('aria-checked')).toBe('true');
    expect(wrapper.emitted('change')?.at(-1)?.[0]).toEqual([{
      id: 'detail-1', type: 'email', value: 'a@b.c', isHidden: true,
    }]);
  });

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

      expect(wrapper.get('[data-error-for="fullName"]').text()).toBe(
        'This value is too long.',
      );
      expect(wrapper.find('[data-issue="personalDetails.fullName"]').exists())
        .toBe(false);
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
