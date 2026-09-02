import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { describe, expect, it } from 'vitest';

import ConfirmDialog from '../../app/components/app/ConfirmDialog.vue';

function open(props: Record<string, unknown> = {}) {
  const trigger = document.createElement('button');
  document.body.append(trigger);
  trigger.focus();
  const wrapper = mount(ConfirmDialog, {
    attachTo: document.body,
    props: {
      open: true,
      title: 'Delete resume',
      description: 'This cannot be undone.',
      confirmLabel: 'Delete',
      confirmAction: 'confirm-delete',
      cancelAction: 'cancel-delete',
      ...props,
    },
  });
  return { wrapper, trigger };
}
const body = () => document.body;

describe('ConfirmDialog', () => {
  it('renders an alert dialog with title and description', async () => {
    const { wrapper } = open();
    await nextTick();
    const dialog = body().querySelector('[role="alertdialog"]')!;
    expect(dialog.getAttribute('aria-labelledby')).toBeTruthy();
    expect(dialog.textContent).toContain('Delete resume');
    expect(dialog.textContent).toContain('This cannot be undone.');
    wrapper.unmount();
  });
  it(
    'gates confirm on exact typed text including case and whitespace',
    async () => {
      const { wrapper } = open({
        confirmText: 'My resume',
        confirmInputLabel: 'Current title',
      });
      await nextTick();
      const confirm = body().querySelector<HTMLButtonElement>(
        '[data-action="confirm-delete"]',
      )!;
      const input = body().querySelector<HTMLInputElement>(
        '[role="alertdialog"] [data-slot="input"]',
      )!;
      expect(confirm.disabled).toBe(true);
      for (const value of ['my resume', 'My resume ']) {
        input.value = value;
        input.dispatchEvent(new Event('input', { bubbles: true }));
        await nextTick();
        expect(confirm.disabled).toBe(true);
      }
      input.value = 'My resume';
      input.dispatchEvent(new Event('input', { bubbles: true }));
      await nextTick();
      expect(confirm.disabled).toBe(false);
      confirm.click();
      expect(wrapper.emitted('confirm')).toHaveLength(1);
      wrapper.unmount();
    },
  );
  it('disables confirmation when the target changes while open', async () => {
    const { wrapper } = open({ confirmText: 'My resume' });
    await nextTick();
    const input = body().querySelector<HTMLInputElement>(
      '[role="alertdialog"] [data-slot="input"]',
    )!;
    input.value = 'My resume';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    await nextTick();
    await wrapper.setProps({ confirmText: 'A different resume' });
    expect(
      body().querySelector<HTMLButtonElement>('[data-action="confirm-delete"]')!
        .disabled,
    ).toBe(true);
    wrapper.unmount();
  });
  it(
    'focuses cancel when destructive and returns focus after cancel',
    async () => {
      const { wrapper, trigger } = open({ destructive: true });
      await nextTick();
      await nextTick();
      const cancel = body().querySelector<HTMLButtonElement>(
        '[data-action="cancel-delete"]',
      )!;
      expect(document.activeElement).toBe(cancel);
      cancel.click();
      await nextTick();
      expect(wrapper.emitted('cancel')).toHaveLength(1);
      await wrapper.setProps({ open: false });
      await nextTick();
      await nextTick();
      expect(document.activeElement).toBe(trigger);
      wrapper.unmount();
      trigger.remove();
    },
  );
  it('returns focus to opener after confirm', async () => {
    const { wrapper, trigger } = open();
    await nextTick();
    await nextTick();
    body()
      .querySelector<HTMLButtonElement>('[data-action="confirm-delete"]')!
      .click();
    await nextTick();
    expect(wrapper.emitted('confirm')).toHaveLength(1);
    await wrapper.setProps({ open: false });
    await nextTick();
    await nextTick();
    expect(document.activeElement).toBe(trigger);
    wrapper.unmount();
    trigger.remove();
  });
  it('ignores Escape while busy', async () => {
    const { wrapper } = open({ busy: true });
    await nextTick();
    body()
      .querySelector('[role="alertdialog"]')!
      .dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      );
    await nextTick();
    expect(wrapper.emitted('cancel')).toBeUndefined();
    wrapper.unmount();
  });
  it('renders hostile title as text', async () => {
    const { wrapper } = open({ title: '<img src=x onerror=alert(1)>' });
    await nextTick();
    expect(body().querySelector('[role="alertdialog"] [onerror]')).toBeNull();
    expect(body().querySelector('[role="alertdialog"]')!.textContent).toContain(
      '<img',
    );
    wrapper.unmount();
  });
});
