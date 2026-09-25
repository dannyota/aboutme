import { mount } from '@vue/test-utils';
import { h, nextTick } from 'vue';
import { afterEach, describe, expect, it } from 'vitest';

import ConfirmDialog from '../../app/components/app/ConfirmDialog.vue';
import FormDialog from '../../app/components/app/FormDialog.vue';
import SelectField from '../../app/components/app/SelectField.vue';

afterEach(() => {
  document.body.innerHTML = '';
});

function classesOf(selector: string): string[] {
  const element = document.body.querySelector(selector);
  expect(element, selector).not.toBeNull();
  return [...(element?.classList ?? [])];
}

describe('dialog rhythm (DESIGN.md)', () => {
  it('spaces a form dialog 24 / 16 / 24 with full-width selects', async () => {
    const wrapper = mount(FormDialog, {
      attachTo: document.body,
      props: {
        open: true,
        title: 'Create resume',
        description: 'Create a new private resume.',
        submitLabel: 'Create',
      },
      slots: {
        default: () => h(SelectField, {
          label: 'Resume language',
          modelValue: 'vi',
          options: [{ value: 'vi', label: 'Tiếng Việt' }],
        }),
      },
    });
    await nextTick();

    expect(classesOf('[role="dialog"]')).toContain('gap-6');
    expect(classesOf('[data-slot="dialog-header"]')).toContain('gap-1.5');
    expect(classesOf('[role="dialog"] form')).toEqual(
      expect.arrayContaining([
        'grid',
        'gap-4',
        '[&_[data-slot=native-select-wrapper]]:w-full',
      ]),
    );
    expect(classesOf('[data-slot="dialog-footer"]')).toEqual(
      expect.arrayContaining(['mt-2', 'flex-col-reverse', 'sm:justify-end']),
    );
    wrapper.unmount();
  });

  it('bounds a form dialog to the viewport and scrolls its content',
    async () => {
      const wrapper = mount(FormDialog, {
        attachTo: document.body,
        props: {
          open: true,
          title: 'Create resume',
          submitLabel: 'Create',
          class: 'sm:max-w-[760px]',
        },
      });
      await nextTick();

      expect(classesOf('[role="dialog"]')).toEqual(
        expect.arrayContaining([
          'max-h-[calc(100dvh-2rem)]',
          'overflow-y-auto',
          'sm:max-w-[760px]',
        ]),
      );
      wrapper.unmount();
    });

  it('gives confirm dialogs the same header and body spacing', async () => {
    const wrapper = mount(ConfirmDialog, {
      attachTo: document.body,
      props: {
        open: true,
        title: 'Delete resume',
        description: 'This cannot be undone.',
        confirmLabel: 'Delete',
      },
    });
    await nextTick();

    expect(classesOf('[role="alertdialog"]')).toContain('gap-6');
    expect(classesOf('[data-slot="alert-dialog-header"]')).toContain(
      'gap-1.5',
    );
    wrapper.unmount();
  });
});
