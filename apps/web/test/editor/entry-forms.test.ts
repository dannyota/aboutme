import { mount } from '@vue/test-utils';
import type { Section } from '@aboutme/schema';
import { computed } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import SectionPanel from '../../app/components/editor/forms/SectionPanel.vue';
import CertificateEntryFields from
  '../../app/components/editor/forms/entries/CertificateEntryFields.vue';
import CustomEntryFields from
  '../../app/components/editor/forms/entries/CustomEntryFields.vue';
import EducationEntryFields from
  '../../app/components/editor/forms/entries/EducationEntryFields.vue';
import LanguageEntryFields from
  '../../app/components/editor/forms/entries/LanguageEntryFields.vue';
import ProfileEntryFields from
  '../../app/components/editor/forms/entries/ProfileEntryFields.vue';
import ProjectEntryFields from
  '../../app/components/editor/forms/entries/ProjectEntryFields.vue';
import SkillEntryFields from
  '../../app/components/editor/forms/entries/SkillEntryFields.vue';
import WorkEntryFields from
  '../../app/components/editor/forms/entries/WorkEntryFields.vue';
import RichTextEditor from
  '../../app/components/editor/richtext/RichTextEditor.vue';
import EntryCard from '../../app/components/editor/EntryCard.vue';
import type { ResumeEditorActions } from
  '../../app/composables/useResumeEditor';

const actionsSpy = () =>
  ({
    createEntityId: vi.fn(() => 'entry-2'),
    edit: vi.fn(),
    record: computed(() => undefined),
  }) as unknown as ResumeEditorActions & {
    createEntityId: ReturnType<typeof vi.fn>;
    edit: ReturnType<typeof vi.fn>;
  };

const sectionFixture = (sectionType: Section['sectionType']): Section =>
  ({ sectionType, entries: [{ id: 'entry-1' }] }) as Section;

const cases = [
  ['profile', ['text']],
  [
    'work',
    [
      'jobTitle',
      'employer',
      'employerLink',
      'city',
      'country',
      'dates',
      'description',
    ],
  ],
  [
    'education',
    [
      'degree',
      'school',
      'schoolLink',
      'city',
      'country',
      'dates',
      'description',
    ],
  ],
  ['skill', ['name', 'level', 'infoHtml']],
  ['language', ['name', 'level']],
  ['certificate', ['title', 'titleLink', 'issuer', 'date', 'description']],
  ['project', ['title', 'link', 'dates', 'description']],
  [
    'custom',
    ['title', 'titleLink', 'subtitle', 'city', 'dates', 'description'],
  ],
] as const;

const entryComponents = {
  profile: ProfileEntryFields,
  work: WorkEntryFields,
  education: EducationEntryFields,
  skill: SkillEntryFields,
  language: LanguageEntryFields,
  certificate: CertificateEntryFields,
  project: ProjectEntryFields,
  custom: CustomEntryFields,
} as const;

describe('SectionPanel', () => {
  it('supports controlled entry-card open state', async () => {
    const wrapper = mount(EntryCard, {
      props: {
        title: 'Entry', entryId: 'e1', hidden: false, index: 0, count: 2,
        open: false,
      },
      slots: { default: '<input data-testid="card-field">' },
    });
    expect(wrapper.find('[data-testid="card-field"]').exists()).toBe(false);
    await wrapper.get('[data-action="toggle-entry-fields"]').trigger('click');
    expect(wrapper.emitted('update:open')).toEqual([[true]]);
    await wrapper.get('[data-action="toggle-hidden"]').trigger('click');
    await wrapper.get('[data-action="entry-down"]').trigger('click');
    await wrapper.get('[data-action="delete-entry"]').trigger('click');
    expect(wrapper.emitted('update:open')).toEqual([[true]]);
    expect(wrapper.emitted('toggleHidden')).toHaveLength(1);
    expect(wrapper.emitted('moveDown')).toHaveLength(1);
    expect(wrapper.emitted('delete')).toHaveLength(1);
    await wrapper.setProps({ open: true });
    await wrapper.vm.$nextTick();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(wrapper.find('[data-testid="card-field"]').exists()).toBe(true);
  });

  it(
    'lets a section entry card collapse without a controlled open prop',
    async () => {
      const wrapper = mount(SectionPanel, {
        props: {
          sectionKey: 'work',
          section: sectionFixture('work'),
          actions: actionsSpy(),
        },
      });
      const trigger = wrapper.get('[data-action="toggle-entry-fields"]');
      expect(trigger.attributes('aria-label')).toBe('Collapse entry fields');

      await trigger.trigger('click');

      expect(
        wrapper
          .get('[data-action="toggle-entry-fields"]')
          .attributes('aria-label'),
      ).toBe('Expand entry fields');
    },
  );

  it('maps an emptied description to unset without a clear intent', () => {
    const wrapper = mount(WorkEntryFields, {
      props: { entry: { id: 'e1', description: '<p>Old</p>' } },
    });
    wrapper.getComponent(RichTextEditor).vm.$emit('update:modelValue', '');
    expect(wrapper.emitted('field')?.at(-1)?.[0]).toEqual({
      path: 'description',
      intent: { kind: 'unset' },
    });
  });

  it('confirms entry deletion through the alert dialog', async () => {
    const actions = actionsSpy();
    const wrapper = mount(SectionPanel, {
      attachTo: document.body,
      props: {
        sectionKey: 'work',
        section: {
          sectionType: 'work',
          entries: [{ id: 'e1', jobTitle: 'Dev' }],
        },
        actions,
      },
    });
    await wrapper
      .get('[data-entry-id="e1"] [data-action="delete-entry"]')
      .trigger('click');
    document.body
      .querySelector<HTMLButtonElement>(
        '[data-action="confirm-delete-entry"]',
      )
      ?.click();
    expect(actions.edit).toHaveBeenLastCalledWith({
      kind: 'entryDelete', sectionKey: 'work', entryId: 'e1',
    });
    wrapper.unmount();
  });

  it('reorders entries from the card controls', async () => {
    const actions = actionsSpy();
    const wrapper = mount(SectionPanel, {
      props: {
        sectionKey: 'work',
        section: {
          sectionType: 'work',
          entries: [{ id: 'e1' }, { id: 'e2' }],
        },
        actions,
      },
    });
    await wrapper
      .get('[data-entry-id="e2"] [data-action="entry-up"]')
      .trigger('click');
    expect(actions.edit).toHaveBeenLastCalledWith({
      kind: 'entryReorder', sectionKey: 'work', entryIds: ['e2', 'e1'],
    });
  });

  it('exposes section and entry IDs as read-only text', () => {
    const sectionId = '0d85ca7e-c265-49cb-a31c-cf8ac8e7d557';
    const entryId = 'dd89bd8a-ba7d-4bec-9c43-f1b296c56fac';
    const wrapper = mount(SectionPanel, {
      props: {
        sectionKey: sectionId,
        section: {
          sectionType: 'work',
          displayName: 'Experience',
          entries: [{
            id: entryId,
            jobTitle: 'Principal Engineer',
          }],
        },
        actions: actionsSpy(),
      },
    });

    expect(wrapper.get('h2').text()).toBe('Experience');
    expect(wrapper.get('[data-section-id-text]').text()).toBe(sectionId);
    expect(wrapper.get('[data-entry-id-text]').text()).toBe(entryId);
    expect(wrapper.get('h3').text()).toBe('Principal Engineer');
    expect(
      wrapper.get('[data-entry-field="jobTitle"] [data-field-input]').element
        .value,
    )
      .not.toBe(sectionId);
    expect(
      wrapper.get('[data-entry-field="jobTitle"] [data-field-input]').element
        .value,
    )
      .not.toBe(entryId);
    expect(
      wrapper.findAll('[contenteditable="true"]').map((field) => field.text()),
    ).not.toContain(sectionId);
    expect(
      wrapper.findAll('[contenteditable="true"]').map((field) => field.text()),
    ).not.toContain(entryId);
  });

  it.each(cases)('renders only %s fields', (sectionType, fields) => {
    const wrapper = mount(SectionPanel, {
      props: {
        sectionKey: `${sectionType}-section`,
        section: sectionFixture(sectionType),
        actions: actionsSpy(),
      },
    });

    expect(
      wrapper
        .findAll('[data-entry-field]')
        .map((node) => node.attributes('data-entry-field')),
    ).toEqual(fields);
  });

  it.each(cases)(
    'captures %s add, field, visibility, and confirmed deletion intents',
    async (sectionType, fields) => {
      const actions = actionsSpy();
      const wrapper = mount(SectionPanel, {
        props: {
          sectionKey: `${sectionType}-section`,
          section: sectionFixture(sectionType),
          actions,
        },
      });

      await wrapper.get('[data-action="add-entry"]').trigger('click');
      wrapper
        .findComponent(entryComponents[sectionType])
        .vm.$emit('field', {
          path: fields[0],
          intent: { kind: 'set', value: 'Ada' },
        });
      wrapper
        .findComponent(entryComponents[sectionType])
        .vm.$emit('field', {
          path: fields[0],
          intent: { kind: 'unset' },
        });
      await wrapper.vm.$nextTick();
      await wrapper.get('[data-action="toggle-hidden"]').trigger('click');
      await wrapper.get('[data-action="delete-entry"]').trigger('click');
      document.body
        .querySelectorAll<HTMLButtonElement>(
          '[data-action="confirm-delete-entry"]',
        )
        .item(0)
        .click();
      await wrapper.vm.$nextTick();

      expect(actions.createEntityId).toHaveBeenCalledOnce();
      expect(actions.edit.mock.calls.map(([intent]) => intent.kind)).toEqual([
        'entryUpsert',
        'entryField',
        'entryField',
        'entryField',
        'entryDelete',
      ]);
      expect(actions.edit).toHaveBeenNthCalledWith(2, {
        kind: 'entryField',
        sectionKey: `${sectionType}-section`,
        entryId: 'entry-1',
        path: fields[0],
        value: {
          present: true,
          value: 'Ada',
        },
      });
      expect(actions.edit).toHaveBeenNthCalledWith(3, {
        kind: 'entryField',
        sectionKey: `${sectionType}-section`,
        entryId: 'entry-1',
        path: fields[0],
        value: { present: false },
      });
      expect('publish' in actions).toBe(false);
    },
  );

  it.each(['skill', 'language'] as const)(
    'preserves level zero for %s',
    async (sectionType) => {
      const actions = actionsSpy();
      const wrapper = mount(SectionPanel, {
        props: {
          sectionKey: sectionType,
          section: sectionFixture(sectionType),
          actions,
        },
      });

      await wrapper.get('[data-entry-field="level"] select').setValue('0');

      expect(actions.edit).toHaveBeenCalledWith({
        kind: 'entryField',
        sectionKey: sectionType,
        entryId: 'entry-1',
        path: 'level',
        value: { present: true, value: 0 },
      });
    },
  );

  it('admits only exact link schemes', async () => {
    const actions = actionsSpy();
    const wrapper = mount(SectionPanel, {
      props: {
        sectionKey: 'work',
        section: sectionFixture('work'),
        actions,
      },
    });
    const link = wrapper.get('[data-entry-field="employerLink"] input');

    for (const invalid of ['HTTPS://example.test', 'https:foo', 'https://', 'mailto:', 'tel:']) {
      await link.setValue(invalid);
      await link.trigger('blur');
    }
    expect(actions.edit).not.toHaveBeenCalled();

    await link.setValue('https://example.test');
    await link.trigger('blur');
    expect(actions.edit).toHaveBeenCalledWith({
      kind: 'entryField',
      sectionKey: 'work',
      entryId: 'entry-1',
      path: 'employerLink',
      value: { present: true, value: 'https://example.test' },
    });
  });

  it('keeps server issue text safe and focuses a mapped field', async () => {
    const actions = {
      ...actionsSpy(),
      record: computed(() => ({
        issues: {
          command: [
            {
              path: 'content.work-section.entries[0].jobTitle',
              code: 'format',
            },
            { path: 'raw.untrusted.path', code: 'unknown' },
          ],
        },
      })),
    } as unknown as ResumeEditorActions;
    const wrapper = mount(SectionPanel, {
      props: {
        sectionKey: 'work-section',
        section: sectionFixture('work'),
        actions,
      },
      attachTo: document.body,
    });

    expect(wrapper.text()).toContain('Enter a value in the required format.');
    expect(wrapper.text()).toContain('This value needs attention.');
    expect(wrapper.text()).not.toContain('raw.untrusted.path');
    await wrapper
      .get('[data-issue="content.work-section.entries[0].jobTitle"]')
      .trigger('click');
    expect(document.activeElement).toBe(
      wrapper.get('[data-entry-field="jobTitle"] input').element,
    );
    wrapper.unmount();
  });

  it(
    'confirms only the captured current entry and restores focus',
    async () => {
      const actions = actionsSpy();
      const wrapper = mount(SectionPanel, {
        props: {
          sectionKey: 'work',
          section: {
            sectionType: 'work',
            entries: [{ id: 'entry-1', jobTitle: 'Engineer' }],
          },
          actions,
        },
        attachTo: document.body,
      });
      const opener = wrapper.get('[data-action="delete-entry"]');
      await opener.trigger('click');
      const dialog = document.body.querySelector('[role="alertdialog"]')!;
      expect(dialog.getAttribute('aria-labelledby')).toBeTruthy();
      expect(dialog.getAttribute('aria-describedby')).toBeTruthy();
      expect(dialog.textContent).toContain('Engineer');
      expect(document.activeElement).toBe(
        document.body.querySelector<HTMLButtonElement>(
          '[data-action="cancel-delete-entry"]',
        ),
      );
      await wrapper.setProps({
        section: { sectionType: 'work', entries: [] },
      });
      document.body
        .querySelectorAll<HTMLButtonElement>(
          '[data-action="confirm-delete-entry"]',
        )
        .item(0)
        .click();
      await wrapper.vm.$nextTick();
      expect(actions.edit).not.toHaveBeenCalled();
      expect(wrapper.text()).toContain(
        'Entry changed. Reopen delete confirmation.',
      );
      wrapper.unmount();
    },
  );
});
