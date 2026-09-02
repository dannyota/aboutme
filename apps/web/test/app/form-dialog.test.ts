import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { describe, expect, it } from 'vitest';

import FormDialog from '../../app/components/app/FormDialog.vue';
import { Dialog } from '../../app/components/ui/dialog';

function open(props: Record<string, unknown> = {}) {
  return mount(FormDialog, {
    attachTo: document.body,
    props: { open: true, title: 'Edit resume', submitLabel: 'Save', ...props },
    slots: { default: '<input id="title">' },
  });
}
describe('FormDialog', () => {
  it('emits submit once and ignores submit while busy', async () => {
    const wrapper = open();
    await nextTick();
    const form = document.body.querySelector('[novalidate]')!;
    form.dispatchEvent(
      new Event('submit', { bubbles: true, cancelable: true }),
    );
    await nextTick();
    expect(wrapper.emitted('submit')).toHaveLength(1);
    await wrapper.setProps({ busy: true });
    form.dispatchEvent(
      new Event('submit', { bubbles: true, cancelable: true }),
    );
    await nextTick();
    expect(wrapper.emitted('submit')).toHaveLength(1);
    wrapper.unmount();
  });
  it('emits cancel from close unless busy and overlay click', async () => {
    const wrapper = open();
    await nextTick();
    const overlay = document.body.querySelector<HTMLElement>(
      '[data-slot="dialog-overlay"]',
    )!;
    await new Promise((resolve) => setTimeout(resolve, 0));
    overlay.dispatchEvent(
      new PointerEvent('pointerdown', {
        bubbles: true,
        button: 0,
        cancelable: true,
      }),
    );
    await nextTick();
    expect(wrapper.emitted('cancel')).toHaveLength(1);

    await wrapper.setProps({ open: true });
    await nextTick();
    wrapper.findComponent(Dialog).vm.$emit('update:open', false);
    await nextTick();
    expect(wrapper.emitted('cancel')).toHaveLength(2);
    await wrapper.setProps({ busy: true, open: true });
    wrapper.findComponent(Dialog).vm.$emit('update:open', false);
    await nextTick();
    expect(wrapper.emitted('cancel')).toHaveLength(2);
    wrapper.unmount();
  });
  it('focuses the first focusable control on open', async () => {
    const wrapper = open();
    await nextTick();
    await nextTick();
    expect(document.activeElement?.id).toBe('title');
    wrapper.unmount();
  });
});
