import { mount } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { beforeEach, describe, expect, it } from 'vitest';
import { ref } from 'vue';

import DateRangeField from
  '../../app/components/editor/forms/DateRangeField.vue';
import YearMonthField from
  '../../app/components/editor/forms/YearMonthField.vue';

const locale = ref<'vi' | 'en'>('en');
mockNuxtImport('useLocale', () => () => ({ locale }));

beforeEach(() => {
  locale.value = 'en';
});

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
      kind: 'set',
      value: { y: 2026, m: 1 },
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
  it('keeps a focused invalid draft while locale copy changes', async () => {
    locale.value = 'en';
    const wrapper = mount(DateRangeField, {
      props: { fieldId: 'dates', modelValue: undefined },
      attachTo: document.body,
    });
    const year = wrapper.get('[data-part="start-year"]');
    await year.setValue('20');
    await year.trigger('blur');
    (year.element as HTMLInputElement).focus();
    (year.element as HTMLInputElement).setSelectionRange(1, 2);
    const emitted = wrapper.emitted('intent');

    locale.value = 'vi';
    await wrapper.vm.$nextTick();

    expect((year.element as HTMLInputElement).value).toBe('20');
    expect((year.element as HTMLInputElement).selectionStart).toBe(1);
    expect((year.element as HTMLInputElement).selectionEnd).toBe(2);
    expect(document.activeElement).toBe(year.element);
    expect(wrapper.emitted('intent')).toEqual(emitted);
    expect(wrapper.text()).toContain('Nhập ngày bắt đầu hợp lệ.');
    expect(wrapper.text()).toContain('Năm bắt đầu');
    wrapper.unmount();
  });

  it('does not emit when a range is changed then restored', async () => {
    const wrapper = mount(DateRangeField, {
      props: {
        fieldId: 'dates',
        modelValue: {
          start: { y: 2024, m: 1 },
          end: { y: 2025, m: 1 },
          present: false,
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

  it('shows a local order error for a start after end', async () => {
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
    expect(wrapper.get('[role="group"]').attributes('aria-describedby')).toBe(
      'work-dates-error',
    );
    expect(wrapper.emitted('intent')).toBeUndefined();
  });

  it('uses January for a missing month and clears absent ranges', async () => {
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
  });
});
