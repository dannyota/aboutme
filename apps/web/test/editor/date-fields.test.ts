import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import DateRangeField from
  '../../app/components/editor/forms/DateRangeField.vue';
import YearMonthField from
  '../../app/components/editor/forms/YearMonthField.vue';

describe('YearMonthField', () => {
  it('does not emit when a date is changed then restored', async () => {
    const wrapper = mount(YearMonthField, {
      props: { fieldId: 'date', label: 'Date', modelValue: { y: 2026, m: 1 } },
    });
    await wrapper.get('[data-part="month"]').setValue('2');
    await wrapper.get('[data-part="month"]').setValue('1');
    await wrapper.get('[data-part="month"]').trigger('blur');
    expect(wrapper.emitted('intent')).toBeUndefined();
  });
  it('preserves a month value while capturing an exact date', async () => {
    const wrapper = mount(YearMonthField, {
      props: { fieldId: 'date', label: 'Date', modelValue: undefined },
    });
    await wrapper.get('[data-part="year"]').setValue('2026');
    await wrapper.get('[data-part="month"]').setValue('1');
    await wrapper.get('[data-part="month"]').trigger('blur');
    expect(wrapper.emitted('intent')?.at(-1)?.[0]).toEqual({
      kind: 'set', value: { y: 2026, m: 1 },
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
  it('does not emit when a range is changed then restored', async () => {
    const wrapper = mount(DateRangeField, {
      props: {
        fieldId: 'dates',
        modelValue: {
          start: { y: 2024, m: 1 }, end: { y: 2025, m: 1 }, present: false,
        },
      },
    });
    await wrapper.get('[data-part="end-month"]').setValue('2');
    await wrapper.get('[data-part="end-month"]').setValue('1');
    await wrapper.get('[data-part="end-month"]').trigger('blur');
    expect(wrapper.emitted('intent')).toBeUndefined();
  });
  it('captures present ranges with a null end', async () => {
    const wrapper = mount(DateRangeField, {
      props: { fieldId: 'work-dates', modelValue: undefined },
    });
    await wrapper.get('[data-part="start-year"]').setValue('2024');
    await wrapper.get('[data-part="start-month"]').setValue('1');
    await wrapper.get('[data-part="present"]').trigger('click');
    expect(wrapper.emitted('intent')?.at(-1)?.[0]).toEqual({
      kind: 'set',
      value: { start: { y: 2024, m: 1 }, end: null, present: true },
    });
  });

  it('requires a start date before accepting present', async () => {
    const wrapper = mount(DateRangeField, {
      props: { fieldId: 'work-dates', modelValue: undefined },
    });
    await wrapper.get('[data-part="present"]').trigger('click');
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
          start: { y: 2024 },
          end: { y: 2025 },
          present: false,
        },
      },
    });
    await wrapper.get('[data-part="start-year"]').setValue('');
    await wrapper.get('[data-part="end-year"]').setValue('');
    await wrapper.get('[data-part="present"]').trigger('click');
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
      expect(
        wrapper.get('[role="group"]').attributes('aria-describedby'),
      ).toBe('work-dates-error');
      expect(wrapper.emitted('intent')).toBeUndefined();
    },
  );

  it(
    'uses January for a missing month and keeps an absent cleared range quiet',
    async () => {
      const wrapper = mount(DateRangeField, {
        props: { fieldId: 'dates', modelValue: undefined },
      });
      await wrapper.get('[data-part="start-year"]').setValue('2024');
      await wrapper.get('[data-part="end-year"]').setValue('2024');
      await wrapper.get('[data-part="end-month"]').setValue('1');
      await wrapper.get('[data-part="end-month"]').trigger('blur');
      expect(wrapper.emitted('intent')?.at(-1)?.[0]).toEqual({
        kind: 'set',
        value: {
          start: { y: 2024 },
          end: { y: 2024, m: 1 },
          present: false,
        },
      });
      await wrapper.get('[data-part="start-year"]').setValue('');
      await wrapper.get('[data-part="end-year"]').setValue('');
      await wrapper.get('[data-part="end-month"]').setValue('');
      await wrapper.get('[data-part="end-month"]').trigger('blur');
      expect(wrapper.find('[data-error="date-order"]').exists()).toBe(false);
    },
  );
});
