// @vitest-environment jsdom

import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';

import CustomEntryFields from
  '../../app/components/editor/forms/entries/CustomEntryFields.vue';
import EducationEntryFields from
  '../../app/components/editor/forms/entries/EducationEntryFields.vue';
import WorkEntryFields from
  '../../app/components/editor/forms/entries/WorkEntryFields.vue';
import RichTextEditor from
  '../../app/components/editor/richtext/RichTextEditor.vue';
import SaveStatus from '../../app/components/editor/SaveStatus.vue';
import {
  createFieldDrafts,
  FieldDraftsKey,
  type FieldDrafts,
} from '../../app/composables/useFieldDrafts';
import {
  flushAndCheckUnsaved,
} from '../../app/composables/useUnsavedNavigationGuard';

afterEach(() => {
  vi.useRealTimers();
  document.body.innerHTML = '';
});

const withDrafts = (drafts: FieldDrafts) => ({
  global: { provide: { [FieldDraftsKey as symbol]: drafts } },
});

const entryForms = [
  ['work', WorkEntryFields],
  ['education', EducationEntryFields],
  ['custom', CustomEntryFields],
] as const;

describe('an unfinished date range', () => {
  it.each(entryForms)(
    'survives a remount and saves its start once Present is ticked (%s)',
    async (_type, component) => {
      const drafts = createFieldDrafts();
      const entry = { id: 'entry-1' };
      const first = mount(component, {
        props: { entry },
        ...withDrafts(drafts),
      });
      const dates = () => first.get('[data-entry-field="dates"]');
      await dates().get('[data-part="start-year"]').setValue('2025');
      await dates().get('[data-part="start-month"]').setValue('7');
      await dates().get('[data-part="start-month"]').trigger('blur');

      expect(dates().get('[data-error="date-order"]').text()).toBe(
        'Add an end date or tick Present to save this date.',
      );
      expect(first.emitted('field')).toBeUndefined();
      expect(drafts.count.value).toBe(1);

      // Switching sections or collapsing the card remounts the form.
      first.unmount();
      const second = mount(component, {
        props: { entry },
        ...withDrafts(drafts),
      });
      await second.vm.$nextTick();
      const restored = second.get('[data-entry-field="dates"]');
      expect(
        (restored.get('[data-part="start-year"]').element as HTMLInputElement)
          .value,
      ).toBe('2025');
      expect(
        (restored.get('[data-part="start-month"]').element as HTMLInputElement)
          .value,
      ).toBe('7');
      expect(restored.get('[data-error="date-order"]').text()).toBe(
        'Add an end date or tick Present to save this date.',
      );

      await restored.get('[data-part="present"]').trigger('click');

      expect(second.emitted('field')?.at(-1)?.[0]).toEqual({
        path: 'dates',
        intent: {
          kind: 'set',
          value: { start: { y: 2025, m: 7 }, end: null, present: true },
        },
      });
      expect(drafts.count.value).toBe(0);
    },
  );

  it('drops the draft when the range is removed', async () => {
    const drafts = createFieldDrafts();
    const wrapper = mount(WorkEntryFields, {
      props: { entry: { id: 'entry-1' } },
      ...withDrafts(drafts),
    });
    await wrapper.get('[data-part="start-year"]').setValue('2025');
    expect(drafts.count.value).toBe(1);

    await wrapper.get('[data-action="unset"]').trigger('click');

    expect(drafts.count.value).toBe(0);
  });

  it('keeps the page while an unfinished date cannot be saved', async () => {
    const drafts = createFieldDrafts();
    const wrapper = mount(WorkEntryFields, {
      props: { entry: { id: 'entry-1' } },
      ...withDrafts(drafts),
    });
    await wrapper.get('[data-part="start-year"]').setValue('2025');
    await wrapper.get('[data-part="start-year"]').trigger('blur');

    expect(flushAndCheckUnsaved(undefined, drafts)).toBe(true);
    expect(wrapper.emitted('field')).toBeUndefined();
  });
});

async function pasteText(editor: HTMLElement, text: string): Promise<void> {
  const paste = new Event('paste', { cancelable: true });
  Object.defineProperty(paste, 'clipboardData', {
    value: {
      files: [],
      getData: (kind: string) => (kind === 'text/plain' ? text : ''),
    },
  });
  editor.dispatchEvent(paste);
  await Promise.resolve();
}

describe('rich text while typing', () => {
  it('emits after a pause and counts as unsaved until then', async () => {
    vi.useFakeTimers();
    const drafts = createFieldDrafts();
    const wrapper = mount(RichTextEditor, {
      props: { modelValue: '' },
      ...withDrafts(drafts),
    });
    const editor = wrapper.get('[contenteditable="true"]').element as
      HTMLElement;

    await pasteText(editor, 'typed');

    expect(wrapper.emitted('update:modelValue')).toBeUndefined();
    expect(drafts.count.value).toBe(1);

    vi.advanceTimersByTime(400);

    expect(wrapper.emitted('update:modelValue')).toEqual([['<p>typed</p>']]);
    expect(drafts.count.value).toBe(0);
  });

  it('hands held text over when the navigation guard flushes', async () => {
    vi.useFakeTimers();
    const drafts = createFieldDrafts();
    const wrapper = mount(RichTextEditor, {
      props: { modelValue: '' },
      ...withDrafts(drafts),
    });
    await pasteText(
      wrapper.get('[contenteditable="true"]').element as HTMLElement,
      'leaving',
    );

    expect(flushAndCheckUnsaved(undefined, drafts)).toBe(false);
    expect(wrapper.emitted('update:modelValue')).toEqual([['<p>leaving</p>']]);
  });

  it('hands held text over when the editor unmounts', async () => {
    vi.useFakeTimers();
    const drafts = createFieldDrafts();
    const update = vi.fn();
    const wrapper = mount(RichTextEditor, {
      props: { 'modelValue': '', 'onUpdate:modelValue': update },
      ...withDrafts(drafts),
    });
    await pasteText(
      wrapper.get('[contenteditable="true"]').element as HTMLElement,
      'kept',
    );

    wrapper.unmount();

    expect(update).toHaveBeenCalledWith('<p>kept</p>');
    expect(drafts.count.value).toBe(0);
  });

  it('drops held text on Escape', async () => {
    vi.useFakeTimers();
    const drafts = createFieldDrafts();
    const wrapper = mount(RichTextEditor, {
      props: { modelValue: '<p>saved</p>' },
      ...withDrafts(drafts),
    });
    const editor = wrapper.get('[contenteditable="true"]');
    await pasteText(editor.element as HTMLElement, ' more');
    await editor.trigger('keydown', { key: 'Escape' });
    vi.advanceTimersByTime(1_000);

    expect(wrapper.emitted('update:modelValue')).toBeUndefined();
    expect(drafts.count.value).toBe(0);
  });
});

describe('save status wording', () => {
  it.each([
    ['dirty', 'Unsaved'],
    ['saving', 'Saving…'],
    ['saved', 'Saved'],
  ] as const)('shows %s as "%s"', (state, text) => {
    const wrapper = mount(SaveStatus, { props: { state } });
    expect(wrapper.text()).toBe(text);
  });
});
