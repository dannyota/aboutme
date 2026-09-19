import type { PersonalDetail } from '@aboutme/schema';
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import ContactChip from '../../app/components/resume/primitives/ContactChip.vue'; // eslint-disable-line max-len

const detail = (
  type: PersonalDetail['type'],
  value: string,
  label?: string,
): PersonalDetail => ({
  id: '00000000-0000-4000-8000-000000000001',
  type,
  value,
  isHidden: false,
  ...(label === undefined ? {} : { label }),
});

describe('contact chips', () => {
  it.each(['website', 'linkedin', 'github', 'twitter'] as const)(
    'linkifies a validated https %s value',
    (type) => {
      const wrapper = mount(ContactChip, {
        props: { detail: detail(type, 'https://example.com'), iconStyle: 'none' },
      });
      const link = wrapper.get('a');
      expect(link.attributes('href')).toBe('https://example.com');
      expect(link.attributes('rel')).toBe('noopener noreferrer');
      expect(link.attributes('style')).toContain('underline');
    },
  );

  it.each(['javascript:alert(1)', '//example.com', 'mailto:a@example.com'])(
    'renders an invalid URL-typed value as text: %s',
    (value) => {
      const wrapper = mount(ContactChip, {
        props: { detail: detail('website', value), iconStyle: 'outline' },
      });
      expect(wrapper.find('a').exists()).toBe(false);
      expect(wrapper.text()).toContain(value);
    },
  );

  it.each(['email', 'phone', 'location'] as const)(
    'keeps %s plain text',
    (type) => {
      const wrapper = mount(ContactChip, {
        props: { detail: detail(type, 'https://example.com'), iconStyle: 'none' },
      });
      expect(wrapper.find('a').exists()).toBe(false);
    },
  );

  it('uses a non-empty custom label', () => {
    const wrapper = mount(ContactChip, {
      props: {
        detail: detail('website', 'https://example.com', 'Portfolio'),
        iconStyle: 'none',
      },
    });
    expect(wrapper.text()).toContain('Portfolio');
    expect(wrapper.text()).not.toContain('Website:');
  });

  it.each([
    ['email', 'a@example.com'],
    ['phone', '+84 90 000 0000'],
    ['location', 'Hanoi'],
    ['website', 'https://example.com'],
    ['github', 'https://github.com/ada'],
  ] as const)(
    'drops the default %s label next to its icon',
    (type, value) => {
      const wrapper = mount(ContactChip, {
        props: { detail: detail(type, value), iconStyle: 'outline' },
      });
      expect(wrapper.find('.contact-label').exists()).toBe(false);
    },
  );

  it('keeps the default label when icons are off', () => {
    const wrapper = mount(ContactChip, {
      props: { detail: detail('email', 'a@example.com'), iconStyle: 'none' },
    });
    expect(wrapper.get('.contact-label').text()).toBe('Email:');
  });

  it('keeps a user label and a custom label next to the icon', () => {
    for (const value of [
      detail('website', 'https://example.com', 'Portfolio'),
      detail('custom', 'ORCID 0000-0001', 'ORCID'),
    ]) {
      const wrapper = mount(ContactChip, {
        props: { detail: value, iconStyle: 'outline' },
      });
      expect(wrapper.get('.contact-label').text()).toBe(`${value.label}:`);
    }
  });

  it.each([
    ['https://danny.vn/', 'danny.vn'],
    ['https://linkedin.com/in/ada', 'linkedin.com/in/ada'],
    ['https://github.com/ada/', 'github.com/ada'],
    ['https://example.com/a?b=1', 'example.com/a?b=1'],
  ])('shows %s without its scheme or trailing slash', (value, shown) => {
    const wrapper = mount(ContactChip, {
      props: { detail: detail('website', value), iconStyle: 'outline' },
    });
    const link = wrapper.get('a');
    expect(link.text()).toBe(shown);
    expect(link.attributes('href')).toBe(value);
  });
});
