import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import TextField from '../../app/components/app/TextField.vue';

function field(modelValue?: string) {
  return mount(TextField, { props: { label: 'Name', modelValue } });
}
const input = (wrapper: ReturnType<typeof field>) =>
  wrapper.get('[data-field-input]');

describe('TextField commit rule (decisions U4)', () => {
  it('sets a non-empty changed value on blur', async () => {
    const wrapper = field(undefined);
    await input(wrapper).setValue('Ada');
    await input(wrapper).trigger('blur');
    expect(wrapper.emitted('intent')).toEqual([
      [{ kind: 'set', value: 'Ada' }],
    ]);
  });
  it('sets on Enter in a single-line field', async () => {
    const wrapper = field('Ada');
    await input(wrapper).setValue('Ada Lovelace');
    await input(wrapper).trigger('keydown', { key: 'Enter' });
    expect(wrapper.emitted('intent')).toEqual([
      [{ kind: 'set', value: 'Ada Lovelace' }],
    ]);
  });
  it('unsets when a defined value is emptied', async () => {
    const wrapper = field('Ada');
    await input(wrapper).setValue('');
    await input(wrapper).trigger('blur');
    expect(wrapper.emitted('intent')).toEqual([[{ kind: 'unset' }]]);
  });
  it('emits nothing for an empty undefined field', async () => {
    const wrapper = field(undefined);
    await input(wrapper).trigger('blur');
    expect(wrapper.emitted('intent')).toBeUndefined();
  });
  it('emits nothing when the value is unchanged', async () => {
    const wrapper = field('Ada');
    await input(wrapper).setValue('Ad');
    await input(wrapper).setValue('Ada');
    await input(wrapper).trigger('blur');
    expect(wrapper.emitted('intent')).toBeUndefined();
  });
  it('reverts on Escape and then emits nothing on blur', async () => {
    const wrapper = field('Ada');
    await input(wrapper).setValue('Grace');
    await input(wrapper).trigger('keydown', { key: 'Escape' });
    expect((input(wrapper).element as HTMLInputElement).value).toBe('Ada');
    await input(wrapper).trigger('blur');
    expect(wrapper.emitted('intent')).toBeUndefined();
  });
  it('follows an external model change while clean', async () => {
    const wrapper = field('Ada');
    await wrapper.setProps({ modelValue: 'Grace' });
    expect((input(wrapper).element as HTMLInputElement).value).toBe('Grace');
  });
  it('keeps the draft on an external model change while dirty', async () => {
    const wrapper = field('Ada');
    await input(wrapper).setValue('Typing');
    await wrapper.setProps({ modelValue: 'Grace' });
    expect((input(wrapper).element as HTMLInputElement).value).toBe('Typing');
  });
  it('does not commit on Enter in a multiline field', async () => {
    const wrapper = mount(TextField, {
      props: { label: 'Summary', modelValue: 'a', multiline: true },
    });
    await wrapper.get('[data-field-input]').setValue('a\nb');
    await wrapper
      .get('[data-field-input]')
      .trigger('keydown', { key: 'Enter' });
    expect(wrapper.emitted('intent')).toBeUndefined();
  });
  it('passes control attributes to the rendered control', () => {
    const wrapper = mount(TextField, {
      props: {
        label: 'Name',
        controlAttrs: { 'data-detail-id': 'x', 'data-part': 'name' },
      },
    });
    expect(wrapper.get('[data-field-input]').attributes('data-detail-id')).toBe(
      'x',
    );
    expect(wrapper.get('[data-field-input]').attributes('data-part')).toBe(
      'name',
    );
  });
  it('does not emit from a queued blur after unmount', async () => {
    const wrapper = field('Ada');
    await input(wrapper).setValue('Grace');
    const element = input(wrapper).element;
    wrapper.unmount();
    element.dispatchEvent(new Event('blur'));
    await Promise.resolve();
    expect(wrapper.emitted('intent')).toBeUndefined();
  });
  it('wires label, hint, and error through FormField', () => {
    const wrapper = mount(TextField, {
      props: {
        label: 'Name',
        id: 'n',
        hint: 'h',
        error: 'e',
        name: 'fullName',
      },
    });
    expect(wrapper.get('[data-slot="label"]').attributes('for')).toBe('n');
    expect(
      wrapper.get('[data-field-input]').attributes('aria-describedby'),
    ).toBe('n-hint n-error');
    expect(wrapper.attributes('data-field')).toBe('fullName');
  });
});
