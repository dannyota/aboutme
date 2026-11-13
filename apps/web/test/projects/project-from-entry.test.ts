import type { ProjectEntry } from '@aboutme/schema';
import { describe, expect, it } from 'vitest';

import {
  canSaveEntry,
  projectFromEntry,
  projectLanguage,
} from '../../app/components/projects/projectFromEntry';

const ENTRY = '0190f000-0000-7000-8000-0000000000e1';

const entry = (overrides: Partial<ProjectEntry> = {}): ProjectEntry => ({
  id: ENTRY, title: 'Shop site', ...overrides,
});

describe('a project made from an entry', () => {
  it('takes the name from the title, the role from the subtitle and the '
    + 'dates, in the language it is given', () => {
    expect(projectFromEntry(entry({
      title: '  Shop site ',
      subtitle: ' Lead engineer ',
      dates: { start: { y: 2024, m: 3 }, end: null, present: true },
      isHidden: true,
    }), 'vi')).toEqual({
      defaultLanguage: 'vi',
      dates: { start: { y: 2024, m: 3 }, end: null, present: true },
      text: { vi: { name: 'Shop site', role: 'Lead engineer' } },
    });
  });

  it('leaves out every field the entry does not have', () => {
    expect(projectFromEntry(entry({ subtitle: ' ', link: '', description: '' }),
      'en')).toEqual({
      defaultLanguage: 'en',
      text: { en: { name: 'Shop site' } },
    });
  });

  it.each([
    'https://github.com/ada/shop',
    'https://gitlab.com/ada/shop',
    'https://bitbucket.org/ada/shop',
    'https://codeberg.org/ada/shop',
    'https://GitHub.com/ada/shop',
  ])('puts a code host link %s in the repository link', (link) => {
    const made = projectFromEntry(entry({ link }), 'en');
    expect(made.repositoryUrl).toBe(link);
    expect(made.demoUrl).toBeUndefined();
  });

  it.each([
    'https://shop.example',
    'https://ada.github.io/shop',
    'https://gist.github.com/ada/1',
    'https://github.com.example/ada',
  ])('puts any other https link %s in the demo link', (link) => {
    const made = projectFromEntry(entry({ link }), 'en');
    expect(made.demoUrl).toBe(link);
    expect(made.repositoryUrl).toBeUndefined();
  });

  it.each(['mailto:ada@example.com', 'tel:+84123456789'])(
    'drops %s, which a project link cannot hold', (link) => {
      const made = projectFromEntry(entry({ link }), 'en');
      expect(made.repositoryUrl).toBeUndefined();
      expect(made.demoUrl).toBeUndefined();
    },
  );

  it('makes the summary plain text, with paragraph and list breaks as '
    + 'spaces and entities decoded', () => {
    const made = projectFromEntry(entry({
      description: '<p>Built the <strong>shop</strong> &amp; its '
        + '<a href="https://shop.example">site</a>.</p>'
        + '<ul><li>Go</li><li>Vue</li></ul><p>Line<br>break</p>',
    }), 'en');

    expect(made.text.en!.summary)
      .toBe('Built the shop & its site. Go Vue Line break');
  });

  it('cuts the summary to 300 characters, never inside a character', () => {
    const long = `${'a'.repeat(299)}e\u0301${'b'.repeat(20)}`;
    expect(projectFromEntry(entry({ description: `<p>${long}</p>` }), 'en')
      .text.en!.summary).toBe('a'.repeat(299));

    const exact = 'é'.repeat(300);
    expect(projectFromEntry(entry({ description: `<p>${exact}</p>` }), 'en')
      .text.en!.summary).toBe(exact);
  });

  it('leaves the summary out when the description has no text', () => {
    expect(projectFromEntry(entry({ description: '<p> </p><p><br></p>' }),
      'en').text.en).toEqual({ name: 'Shop site' });
  });
});

describe('the language of a project made from an entry', () => {
  it.each([
    ['vi', 'en', 'vi'],
    ['en-GB', 'vi', 'en'],
    ['zh-Hant', 'vi', 'zh'],
    ['und', 'vi', 'vi'],
    [null, 'en', 'en'],
    [undefined, 'vi', 'vi'],
  ] as const)('is the resume language %s, else the site language %s',
    (lng, site, expected) => {
      expect(projectLanguage(lng, site)).toBe(expected);
    });
});

describe('an entry that can become a project', () => {
  it('needs a title, and a title and subtitle within the project limits',
    () => {
      expect(canSaveEntry(entry())).toBe(true);
      expect(canSaveEntry(entry({ title: '   ' }))).toBe(false);
      expect(canSaveEntry(entry({ title: undefined }))).toBe(false);
      expect(canSaveEntry(entry({ title: 'a'.repeat(120) }))).toBe(true);
      expect(canSaveEntry(entry({ title: 'a'.repeat(121) }))).toBe(false);
      expect(canSaveEntry(entry({ subtitle: 'b'.repeat(121) }))).toBe(false);
    });
});
