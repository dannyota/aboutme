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

  it.each(['email', 'phone', 'location', 'custom'] as const)(
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
});
