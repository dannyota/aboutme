import type { PersonalDetail } from '@aboutme/schema';
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { computed } from 'vue';

import { ResumeLngKey } from '../../app/components/resume/formatDate';
import ContactChip from '../../app/components/resume/primitives/ContactChip.vue'; // eslint-disable-line max-len

// Link display modes, custom https links, and brand marks
// (docs/adr/0041-contact-link-display-and-body-justify.md).

type Display = NonNullable<PersonalDetail['display']>;

const detail = (
  type: PersonalDetail['type'],
  value: string,
  extra: { label?: string; display?: Display } = {},
): PersonalDetail => ({
  id: '00000000-0000-4000-8000-000000000001',
  type,
  value,
  isHidden: false,
  ...extra,
});

const chip = (
  value: PersonalDetail,
  iconStyle: 'none' | 'outline' = 'outline',
) => mount(ContactChip, { props: { detail: value, iconStyle } });

const DISPLAYS: readonly (Display | undefined)[] = [
  undefined,
  'short',
  'full',
  'label',
];

describe('custom https links', () => {
  it('links a custom https value with the link icon', () => {
    const wrapper = chip(detail('custom', 'https://scholar.example.com/ada/', {
      label: 'Google Scholar',
    }));
    const link = wrapper.get('a');
    expect(link.attributes('href')).toBe('https://scholar.example.com/ada/');
    expect(link.attributes('rel')).toBe('noopener noreferrer');
    expect(link.text()).toBe('scholar.example.com/ada');
    expect(wrapper.get('.contact-label').text()).toBe('Google Scholar:');
    expect(wrapper.get('svg').classes()).toContain('lucide-link');
  });

  it('keeps a custom text value on the person icon', () => {
    const wrapper = chip(
      detail('custom', 'ORCID 0000-0001', { label: 'ORCID' }),
    );
    expect(wrapper.find('a').exists()).toBe(false);
    expect(wrapper.get('svg').classes()).toContain('lucide-user');
  });

  it.each([
    'javascript:alert(1)',
    'data:text/html,<b>x</b>',
    '//evil.example',
    'HTTPS://example.com',
    ' https://example.com',
    'https:/example.com',
    'http://example.com',
  ])('renders hostile custom value %j as text in every mode', (value) => {
    for (const display of DISPLAYS) {
      for (const iconStyle of ['none', 'outline'] as const) {
        const wrapper = chip(detail('custom', value, {
          label: 'Profile',
          ...(display === undefined ? {} : { display }),
        }), iconStyle);
        const mode = `${display}/${iconStyle}`;
        expect(wrapper.find('a').exists(), mode).toBe(false);
        expect(wrapper.html()).not.toContain('href');
        expect(wrapper.text()).toContain(value.trim());
      }
    }
  });

  it.each(['javascript:alert(1)', '//evil.example', 'HTTPS://example.com'])(
    'keeps hostile typed URL value %j as text in every display mode',
    (value) => {
      for (const display of DISPLAYS) {
        const wrapper = chip(detail('github', value, {
          ...(display === undefined ? {} : { display }),
        }));
        expect(wrapper.find('a').exists(), String(display)).toBe(false);
      }
    },
  );
});

describe('link display modes', () => {
  it('shows the short address when display is absent or short', () => {
    for (const display of [undefined, 'short'] as const) {
      const link = chip(detail('website', 'https://ada.example.com/', {
        ...(display === undefined ? {} : { display }),
      })).get('a');
      expect(link.text()).toBe('ada.example.com');
      expect(link.attributes('href')).toBe('https://ada.example.com/');
    }
  });

  it('shows the whole URL with display full', () => {
    const wrapper = chip(detail('website', 'https://ada.example.com/', {
      display: 'full',
    }), 'none');
    expect(wrapper.get('a').text()).toBe('https://ada.example.com/');
    expect(wrapper.get('.contact-label').text()).toBe('Website:');
  });

  it('shows the user label as the anchor with no prefix', () => {
    const wrapper = chip(detail('custom', 'https://scholar.example.com/ada', {
      label: 'Google Scholar',
      display: 'label',
    }), 'none');
    expect(wrapper.get('a').text()).toBe('Google Scholar');
    expect(wrapper.find('.contact-label').exists()).toBe(false);
    expect(wrapper.text()).not.toContain('scholar.example.com');
  });

  it.each([
    ['github', 'GitHub'],
    ['twitter', 'X'],
    ['linkedin', 'LinkedIn'],
    ['website', 'Website'],
    ['custom', 'Detail'],
  ] as const)('falls back to the %s default label', (type, text) => {
    for (const iconStyle of ['none', 'outline'] as const) {
      const wrapper = chip(detail(type, 'https://example.com/me', {
        display: 'label',
      }), iconStyle);
      expect(wrapper.get('a').text()).toBe(text);
      expect(wrapper.find('.contact-label').exists()).toBe(false);
    }
  });

  it('ignores display on email, phone, and text values', () => {
    for (const display of ['full', 'label'] as const) {
      // An email links as mailto: with its value as the text (ADR 0043).
      const email = chip(
        detail('email', 'ada@example.com', { display }),
        'none',
      );
      expect(email.get('a').attributes('href'))
        .toBe('mailto:ada@example.com');
      expect(email.text()).toBe('Email:ada@example.com');
      const location = chip(
        detail('location', 'Hà Nội', { display }),
        'none',
      );
      expect(location.find('a').exists()).toBe(false);
      expect(location.text()).toBe('Location:Hà Nội');
    }
  });
});

describe('brand marks', () => {
  it.each([
    ['github', 'brand-github', '0 0 24 24'],
    ['twitter', 'brand-x', '0 0 24 24'],
    ['linkedin', 'brand-linkedin', '-14 18 476 476'],
  ] as const)('draws the %s mark in the icon colour', (type, name, box) => {
    const svg = chip(detail(type, 'https://example.com/me')).get('svg');
    expect(svg.classes())
      .toEqual(expect.arrayContaining(['resume-icon', name]));
    expect(svg.attributes('fill')).toBe('currentColor');
    // Every mark fills a square box at the Lucide icon size.
    expect(svg.attributes('viewBox')).toBe(box);
    expect(svg.attributes('width')).toBe('16');
    expect(svg.attributes('height')).toBe('16');
    expect(svg.attributes('aria-hidden')).toBe('true');
    expect(svg.findAll('path')).toHaveLength(1);
    expect(svg.html()).not.toMatch(/<(image|use|script|a)\b|href|url\(/u);
  });

  it('keeps a linked custom detail on the generic link glyph', () => {
    const svg = chip(detail('custom', 'https://orcid.example/ada')).get('svg');
    expect(svg.classes()).toContain('lucide-link');
  });

  it('labels twitter X when icons are off', () => {
    const wrapper = chip(detail('twitter', 'https://x.com/ada'), 'none');
    expect(wrapper.get('.contact-label').text()).toBe('X:');
  });
});

describe('default labels in a Vietnamese resume', () => {
  it.each([
    ['phone', '+84 90 000 0000', 'Điện thoại:'],
    ['location', 'Hà Nội', 'Địa chỉ:'],
    ['email', 'an@example.com', 'Email:'],
  ] as const)('labels %s in Vietnamese', (type, value, text) => {
    const wrapper = mount(ContactChip, {
      props: { detail: detail(type, value), iconStyle: 'none' },
      global: { provide: { [ResumeLngKey as symbol]: computed(() => 'vi') } },
    });
    expect(wrapper.get('.contact-label').text()).toBe(text);
  });
});
