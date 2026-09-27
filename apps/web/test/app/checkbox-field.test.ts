import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import CheckboxField from '../../app/components/app/CheckboxField.vue';

const describedBy = (wrapper: ReturnType<typeof mount>) =>
  wrapper.get('[role="checkbox"]').attributes('aria-describedby');

// docs/design/linkedin-import-ui.md Accessibility.
describe('CheckboxField description id', () => {
  it('points aria-describedby at the description id when there is one', () => {
    const wrapper = mount(CheckboxField, {
      props: {
        id: 'email',
        label: 'Email',
        modelValue: false,
        description: 'you@example.com',
      },
    });
    const description = wrapper.get('p');
    expect(description.attributes('id')).toBe('email-description');
    expect(describedBy(wrapper)).toBe('email-description');
  });

  it('has no aria-describedby without a description', () => {
    const wrapper = mount(CheckboxField, {
      props: { id: 'email', label: 'Email', modelValue: false },
    });
    expect(wrapper.find('p').exists()).toBe(false);
    expect(describedBy(wrapper)).toBeUndefined();
  });

  it('joins a caller-supplied aria-describedby with the description id', () => {
    const wrapper = mount(CheckboxField, {
      props: {
        id: 'email',
        label: 'Email',
        modelValue: false,
        description: 'you@example.com',
      },
      attrs: { 'aria-describedby': 'external-hint' },
    });
    expect(describedBy(wrapper)).toBe('external-hint email-description');
  });

  it('keeps a caller-supplied aria-describedby without a description', () => {
    const wrapper = mount(CheckboxField, {
      props: { id: 'email', label: 'Email', modelValue: false },
      attrs: { 'aria-describedby': 'external-hint' },
    });
    expect(describedBy(wrapper)).toBe('external-hint');
  });
});
