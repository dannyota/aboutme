import type { PersonalDetail } from '@aboutme/schema';
import { mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import {
  contactHref,
  emailHref,
  phoneHref,
} from '../../app/components/resume/contactHref';
import ContactChip from
  '../../app/components/resume/primitives/ContactChip.vue';

function detail(
  type: PersonalDetail['type'],
  value: string,
  extra: Partial<PersonalDetail> = {},
): PersonalDetail {
  return { id: 'detail-1', type, value, isHidden: false, ...extra };
}

// The corpus is shared with the server's internal/contactlink tests, so the
// renderer and the public page validator link exactly the same values
// (docs/adr/0043-email-and-phone-links.md).
interface CorpusCase {
  type: PersonalDetail['type'];
  value: string;
  href: string | null;
}

const corpus = JSON.parse(readFileSync(resolve(
  process.cwd(),
  '../server/internal/contactlink/testdata/contact-link-corpus.json',
), 'utf8')) as { cases: CorpusCase[] };

describe('email and phone links match the server corpus', () => {
  it('has cases for both link types and for text', () => {
    const types = new Set(corpus.cases.map((test) => test.type));
    expect(types).toEqual(new Set(['email', 'phone', 'location', 'custom']));
    expect(corpus.cases.some((test) => test.href === null)).toBe(true);
  });

  it.each(corpus.cases.map((test) => [
    test.type,
    JSON.stringify(test.value),
    test,
  ] as const))('%s %s', (_type, _value, test) => {
    expect(contactHref(detail(test.type, test.value))).toBe(test.href);
    if (test.type === 'email') expect(emailHref(test.value)).toBe(test.href);
    if (test.type === 'phone') expect(phoneHref(test.value)).toBe(test.href);
  });
});

describe('contact detail links', () => {
  it('links only the types that can link', () => {
    expect(contactHref(detail('email', 'ada@example.com')))
      .toBe('mailto:ada@example.com');
    expect(contactHref(detail('phone', '(+84) 374837720')))
      .toBe('tel:+84374837720');
    expect(contactHref(detail('location', 'Hà Nội'))).toBeNull();
    expect(contactHref(detail('github', 'https://github.com/ada')))
      .toBe('https://github.com/ada');
    expect(contactHref(detail('custom', 'ada@example.com'))).toBeNull();
    expect(contactHref(detail('custom', 'http://ada.dev'))).toBeNull();
  });

  it.each([
    ['email', 'ada@example.com', 'mailto:ada@example.com', 'mail'],
    ['phone', '(+84) 374837720', 'tel:+84374837720', 'phone'],
  ] as const)('renders a %s link with its value as text', (
    type,
    value,
    href,
    icon,
  ) => {
    // A stored display value does not change an email or phone link.
    const wrapper = mount(ContactChip, {
      props: {
        detail: detail(type, value, { display: 'label' }),
        iconStyle: 'outline',
      },
    });
    const anchor = wrapper.get('a');
    expect(anchor.attributes('href')).toBe(href);
    expect(anchor.attributes('rel')).toBe('noopener noreferrer');
    expect(anchor.attributes('style')).toContain('text-decoration: underline');
    expect(anchor.text()).toBe(value);
    expect(wrapper.find(`[data-icon="${icon}"]`).exists()
      || wrapper.html().includes(icon)).toBe(true);
  });

  it.each([
    ['email', 'not an address'],
    ['phone', 'ask me'],
    ['location', 'Hà Nội'],
  ] as const)('renders an unlinkable %s as text', (type, value) => {
    const wrapper = mount(ContactChip, {
      props: { detail: detail(type, value), iconStyle: 'outline' },
    });
    expect(wrapper.find('a').exists()).toBe(false);
    expect(wrapper.text()).toContain(value);
  });
});
