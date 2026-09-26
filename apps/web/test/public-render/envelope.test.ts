// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import {
  decodePublicRenderEnvelope,
  PUBLIC_RENDER_FAILURE,
} from '../../server/utils/public-render/envelope';

const document = JSON.parse(
  readFileSync(
    resolve(process.cwd(), '../../packages/schema/fixtures/minimal.json'),
    'utf8',
  ),
);
document.content = {
  profile: {
    sectionType: 'profile',
    entries: [{ id: '00000000-0000-4000-8000-000000000001' }],
  },
};
document.customization.layout.sections.main = ['profile'];

const base = {
  publicResume: {
    slug: 'ada1',
    revision: '7',
    lng: 'en',
    downloadEnabled: false,
    document,
  },
  mode: 'continuous',
  canonicalOrigin: 'https://resume.example',
  discoveryEnabled: false,
  pageTitle: 'Ada — Resume',
  faviconHref: '',
};

const preview = {
  title: 'Ada Lovelace',
  description: 'Writes the first program.',
  locale: 'en_US',
  imageAlt: 'Ada Lovelace · Analyst',
};

const decode = (value: unknown) =>
  decodePublicRenderEnvelope(JSON.stringify(value));

const rejects = (value: unknown) =>
  expect(() => decode(value)).toThrow(PUBLIC_RENDER_FAILURE);

const withPreview = (fields: Record<string, unknown>) => ({
  ...base,
  preview: { ...preview, ...fields },
});

// A string of exactly `bytes` UTF-8 bytes built from 3-byte characters and
// ASCII padding, so byte limits are measured in bytes, not characters.
const bytesOf = (bytes: number) =>
  'ệ'.repeat(Math.floor(bytes / 3)) + 'a'.repeat(bytes % 3);

describe('public render envelope preview', () => {
  it('accepts an envelope without preview text', () => {
    expect(decode(base).preview).toBeUndefined();
  });

  it('accepts preview text and keeps its values', () => {
    expect(decode({ ...base, preview }).preview).toEqual(preview);
  });

  it('accepts an empty locale and a three-letter language code', () => {
    for (const locale of ['', 'vi_VN', 'fil_PH']) {
      expect(decode(withPreview({ locale })).preview?.locale).toBe(locale);
    }
  });

  it('rejects a preview that is not an object', () => {
    for (const value of [null, 'text', 7, true, [], [preview]]) {
      rejects({ ...base, preview: value });
    }
  });

  it('rejects a missing or extra preview key', () => {
    for (const key of Object.keys(preview)) {
      const value = Object.fromEntries(
        Object.entries(preview).filter(([name]) => name !== key),
      );
      rejects({ ...base, preview: value });
    }
    rejects(withPreview({ themeColor: '#000000' }));
    rejects(withPreview({ twitterTitle: 'Ada' }));
  });

  it('rejects a duplicate preview key', () => {
    const source = JSON.stringify({ ...base, preview })
      .replace('"title":', '"title":"Ada","title":');
    expect(() => decodePublicRenderEnvelope(source))
      .toThrow(PUBLIC_RENDER_FAILURE);
  });

  it('rejects a non-string or empty text value', () => {
    for (const key of ['title', 'description', 'imageAlt']) {
      for (const value of ['', null, 7, ['x'], { text: 'x' }]) {
        rejects(withPreview({ [key]: value }));
      }
    }
    for (const value of [null, 7, ['en_US']]) {
      rejects(withPreview({ locale: value }));
    }
  });

  it('bounds each text value in UTF-8 bytes', () => {
    for (const [key, limit] of [
      ['title', 2240],
      ['description', 1024],
      ['imageAlt', 1024],
    ] as const) {
      expect(decode(withPreview({ [key]: bytesOf(limit) })).preview?.[key])
        .toBe(bytesOf(limit));
      rejects(withPreview({ [key]: bytesOf(limit + 1) }));
    }
  });

  it('rejects a locale outside language_REGION', () => {
    for (const locale of [
      'en',
      'en-US',
      'EN_US',
      'en_us',
      'e_US',
      'engl_US',
      'en_USA',
      ' en_US',
      'en_US ',
      'en_US\n',
    ]) {
      rejects(withPreview({ locale }));
    }
  });
});

describe('public render envelope card image', () => {
  const imageUrl
    = 'https://resume.example/api/v1/public/resumes/ada1/og/0123456789abcdef.png';

  it('accepts preview text with and without the card URL', () => {
    expect(decode(withPreview({ imageUrl })).preview?.imageUrl)
      .toBe(imageUrl);
    expect(decode({ ...base, preview }).preview?.imageUrl).toBeUndefined();
  });

  it('accepts only this page\'s versioned card URL', () => {
    for (const value of [
      '',
      null,
      7,
      'https://resume.example/api/v1/public/resumes/ada1/og.png',
      'https://other.example/api/v1/public/resumes/ada1/og/0123456789abcdef.png',
      'https://resume.example/api/v1/public/resumes/bob1/og/0123456789abcdef.png',
      'https://resume.example/api/v1/public/resumes/ada1/og/0123456789ABCDEF.png',
      'https://resume.example/api/v1/public/resumes/ada1/og/0123456789abcde.png',
      'https://resume.example/api/v1/public/resumes/ada1/og/0123456789abcdef0.png',
      `${imageUrl}?v=1`,
      `${imageUrl}#x`,
      ` ${imageUrl}`,
      `${imageUrl}"><script>`,
    ]) {
      rejects(withPreview({ imageUrl: value }));
    }
  });
});
