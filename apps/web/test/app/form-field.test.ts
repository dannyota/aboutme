import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import FormField from '../../app/components/app/FormField.vue';

const slotInput = [
  '<template #default="{ id, describedBy, invalid }">',
  '<input data-field-input :id="id" :aria-describedby="describedBy"',
  ':aria-invalid="invalid">',
  '</template>',
].join('');

describe('FormField', () => {
  it('links label, hint, and error to the control', () => {
    const wrapper = mount(FormField, {
      props: {
        label: 'Email',
        id: 'email',
        hint: 'Work address',
        error: 'Required',
        name: 'email',
      },
      slots: { default: slotInput },
    });
    const input = wrapper.get('[data-field-input]');
    expect(wrapper.get('[data-slot="label"]').attributes('for')).toBe('email');
    expect(input.attributes('aria-describedby')).toBe('email-hint email-error');
    expect(input.attributes('aria-invalid')).toBe('true');
    expect(wrapper.get('[role="alert"]').attributes('id')).toBe('email-error');
    expect(wrapper.get('[role="alert"]').attributes('data-error-for')).toBe(
      'email',
    );
    expect(wrapper.attributes('data-field')).toBe('email');
  });

  it('omits describedby and invalid without hint or error', () => {
    const wrapper = mount(FormField, {
      props: { label: 'Email' },
      slots: { default: slotInput },
    });
    const input = wrapper.get('[data-field-input]');
    expect(input.attributes('id')).toMatch(/^field-/);
    expect(input.attributes('aria-describedby')).toBeUndefined();
    expect(input.attributes('aria-invalid')).toBeUndefined();
    expect(wrapper.find('[role="alert"]').exists()).toBe(false);
  });
});
