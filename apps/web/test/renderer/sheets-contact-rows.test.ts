import type { PersonalDetail, Resume } from '@aboutme/schema';
import { mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import ContactChip from '../../app/components/resume/primitives/ContactChip.vue'; // eslint-disable-line max-len
import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';

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

const row = (
  value: PersonalDetail,
  iconStyle: 'none' | 'outline' = 'outline',
) => mount(ContactChip, { props: { detail: value, iconStyle, row: true } });

describe('sheets contact rows', () => {
  it('renders a div row with the chip classes and a sheets modifier', () => {
    const wrapper = row(detail('email', 'ada@example.com'));
    expect(wrapper.element.tagName).toBe('DIV');
    expect(wrapper.classes()).toEqual(['contact-chip', 'contact-chip--sheets']);
    expect(wrapper.find('.contact-label').exists()).toBe(true);
  });

  it('shows the icon, the label without a colon, then the value', () => {
    const wrapper = row(detail('phone', '+84 912 345 678'));
    const label = wrapper.get('.contact-label');
    expect(label.text()).toBe('Phone');
    expect(label.text()).not.toContain(':');
    expect(label.find('svg.resume-icon').exists()).toBe(true);
    const value = wrapper.get('.contact-value');
    expect(value.get('a').attributes('href')).toBe('tel:+84912345678');
    expect(value.get('a').text()).toBe('+84 912 345 678');
    expect([...wrapper.element.children].map((child) => child.className))
      .toEqual(['contact-label', 'contact-value']);
  });

  it('shows the label beside an icon even without an authored label', () => {
    for (const type of ['email', 'website', 'github', 'linkedin'] as const) {
      const value = type === 'email' ? 'ada@example.com' : 'https://example.com/ada';
      expect(row(detail(type, value)).get('.contact-label').text())
        .not.toBe('');
    }
  });

  it('omits the icon when the icon style is none', () => {
    const wrapper = row(detail('email', 'ada@example.com'), 'none');
    expect(wrapper.find('svg').exists()).toBe(false);
    expect(wrapper.get('.contact-label').text()).toBe('Email');
  });

  it('prefers the authored label over the type default', () => {
    const wrapper = row(detail('custom', 'https://scholar.example.com/ada/', {
      label: 'Google Scholar',
    }));
    expect(wrapper.get('.contact-label').text()).toBe('Google Scholar');
    expect(wrapper.get('a').text()).toBe('scholar.example.com/ada');
  });

  it('uses the display text as anchor text and keeps the label column', () => {
    const labelled = row(detail('github', 'https://github.com/ada', {
      display: 'label',
    }));
    expect(labelled.get('.contact-label').text()).toBe('GitHub');
    expect(labelled.get('.contact-value a').text()).toBe('GitHub');
    expect(labelled.get('a').attributes('href')).toBe('https://github.com/ada');
    const full = row(detail('twitter', 'https://x.com/ada', {
      display: 'full',
    }));
    expect(full.get('.contact-label').text()).toBe('X');
    expect(full.get('.contact-value a').text()).toBe('https://x.com/ada');
  });

  it('renders location as a row without a link', () => {
    const wrapper = row(detail('location', 'Hà Nội, Việt Nam'));
    expect(wrapper.find('a').exists()).toBe(false);
    expect(wrapper.get('.contact-value').text()).toBe('Hà Nội, Việt Nam');
    expect(wrapper.get('.contact-label').text()).toBe('Location');
  });

  it.each(['javascript:alert(1)', '//evil.example', 'HTTPS://example.com'])(
    'keeps hostile value %j as text',
    (value) => {
      const wrapper = row(detail('website', value));
      expect(wrapper.find('a').exists()).toBe(false);
      expect(wrapper.html()).not.toContain('href');
      expect(wrapper.get('.contact-value').text()).toBe(value);
    },
  );

  it('keeps the inline chip unchanged outside sheets', () => {
    const chip = mount(ContactChip, {
      props: { detail: detail('email', 'ada@example.com', { label: 'Mail' }),
        iconStyle: 'outline' },
    });
    expect(chip.element.tagName).toBe('SPAN');
    expect(chip.classes()).toEqual(['contact-chip']);
    expect(chip.get('.contact-label').text()).toBe('Mail:');
    expect(chip.find('.contact-value').exists()).toBe(false);
  });
});

describe('sheets header contact rows', () => {
  const full = (): Resume => {
    const document = JSON.parse(
      readFileSync(
        resolve(
          import.meta.dirname,
          '../../../../packages/schema/fixtures/full.json',
        ),
        'utf8',
      ),
    ) as Resume;
    delete document.personalDetails.photo;
    return document;
  };

  it.each(['inline', 'stacked'] as const)(
    'turns %s details into rows inside header.resume-header',
    (detailsLayout) => {
      const document = full();
      document.customization.header = {
        ...document.customization.header!,
        detailsLayout,
      };
      const wrapper = mount(ResumeDocument, {
        props: { document, context: { lng: 'en', mode: 'sheets' } },
      });
      const rows = wrapper.findAll('.contact-chip');
      const shown = document.personalDetails.details!
        .filter((item) => !item.isHidden);
      expect(rows).toHaveLength(shown.length);
      for (const item of rows) {
        expect(item.element.tagName).toBe('DIV');
        expect(item.classes()).toContain('contact-chip--sheets');
        expect(item.element.closest('header.resume-header')).not.toBeNull();
      }
      expect(wrapper.get('.resume-details').classes())
        .toContain('details-rows');
      expect(wrapper.findAll('.contact-label')).toHaveLength(shown.length);
    },
  );
});
