// @vitest-environment node

import { describe, expect, it } from 'vitest';

import { CARD_LAYOUT_VERSION } from '../../app/components/preview/cardLayout';
import {
  decodePrintEnvelope,
  isCardEnvelope,
  PRINT_FAILURE,
  PRINT_PHOTO_MAX_BYTES,
} from '../../server/utils/print/envelope';
import { cardEnvelope, printEnvelope, RESUME_ID } from './fixture';

// The closed card envelope of docs/design/link-previews.md, "Preview card".
const decode = (value: unknown) => decodePrintEnvelope(JSON.stringify(value));
const rejected = (value: unknown): void => {
  expect(() => decode(value)).toThrow(PRINT_FAILURE);
};
const withCard = (fields: Record<string, unknown>) => {
  const envelope = cardEnvelope();
  return { ...envelope, card: { ...envelope.card, ...fields } };
};

describe('print card envelope', () => {
  it('accepts the exact card envelope and tells it from a resume', () => {
    const decoded = decode(cardEnvelope());
    expect(decoded).toEqual(cardEnvelope());
    expect(isCardEnvelope(decoded)).toBe(true);
    expect(isCardEnvelope(decode(printEnvelope()))).toBe(false);
  });

  it('matches the server layout version', () => {
    expect(CARD_LAYOUT_VERSION).toBe(1);
    for (const layoutVersion of [0, 2, '1', null, 1.5]) {
      rejected(withCard({ layoutVersion }));
    }
  });

  it('rejects any extra or missing key at either level', () => {
    for (const extra of [
      { document: printEnvelope().document },
      { revision: '7' },
      { lng: 'vi' },
    ]) {
      rejected({ ...cardEnvelope(), ...extra });
    }
    for (const extra of [
      { email: 'sentinel-card-email@example.com' },
      { contacts: [] },
      { details: [{ value: '+84 912 345 678' }] },
      { summary: 'text' },
    ]) {
      rejected(withCard(extra));
    }
    const without = (value: object, key: string) => Object.fromEntries(
      Object.entries(value).filter(([name]) => name !== key),
    );
    for (const key of Object.keys(cardEnvelope())) {
      rejected(without(cardEnvelope(), key));
    }
    for (const key of Object.keys(cardEnvelope().card)) {
      rejected({ ...cardEnvelope(), card: without(cardEnvelope().card, key) });
    }
  });

  it('rejects duplicate keys inside the card', () => {
    const source = JSON.stringify(cardEnvelope())
      .replace('"slug":', '"slug":"other-slug","slug":');
    expect(() => decodePrintEnvelope(source)).toThrow(PRINT_FAILURE);
  });

  it('requires version 1, kind card, and a canonical resume id', () => {
    rejected({ ...cardEnvelope(), version: 2 });
    for (const kind of ['Card', 'resume', '', null]) {
      rejected({ ...cardEnvelope(), kind });
    }
    for (const resumeId of [
      '00000000-0000-0000-0000-000000000000',
      RESUME_ID.toUpperCase(),
      'not-a-uuid',
    ]) {
      rejected({ ...cardEnvelope(), resumeId });
    }
  });

  it('accepts canonical languages and und', () => {
    for (const lng of ['vi', 'en', 'en-GB', 'und']) {
      expect(decode(withCard({ lng }))).toBeTruthy();
    }
    for (const lng of ['en-us', 'EN', 'iw', '', 'x'.repeat(36), 7]) {
      rejected(withCard({ lng }));
    }
  });

  it('accepts only public slug shapes', () => {
    for (const slug of ['abcd', 'a'.repeat(30), 'nguyen-van-an', 'a1-b2']) {
      expect(decode(withCard({ slug }))).toBeTruthy();
    }
    for (const slug of [
      'abc',
      'a'.repeat(31),
      '-abcd',
      'abcd-',
      'ab--cd',
      'Abcd',
      'ab_cd',
      'ab cd',
      'ab"cd',
      'ab/cd',
      null,
    ]) {
      rejected(withCard({ slug }));
    }
  });

  it('accepts normalized name and headline of 1 to 160 code points', () => {
    expect(decode(withCard({ name: 'A', headline: null }))).toBeTruthy();
    expect(decode(withCard({ name: '𝒜'.repeat(160) }))).toBeTruthy();
    expect(decode(withCard({ headline: 'ệ'.repeat(160) }))).toBeTruthy();
    expect(decode(withCard({ name: null, headline: null }))).toBeTruthy();
    for (const field of ['name', 'headline']) {
      for (const value of [
        '',
        'a'.repeat(161),
        '𝒜'.repeat(161),
        ' Ada',
        'Ada ',
        ' Ada',
        'Ada　',
        'Ada\u0007Lovelace',
        'Ada\nLovelace',
        '\ud800Ada',
        7,
        ['Ada'],
      ]) {
        rejected(withCard({ [field]: value }));
      }
    }
  });

  it('rejects a headline without a name', () => {
    rejected(withCard({ name: null, headline: 'Kỹ sư' }));
  });

  it('accepts only an inline JPEG or PNG photo with an optional crop', () => {
    const photo = (value: unknown) => withCard({ photo: value });
    expect(decode(photo(null))).toBeTruthy();
    expect(decode(photo({ url: 'data:image/jpeg;base64,/9j/', crop: null })))
      .toBeTruthy();
    expect(decode(photo({
      url: 'data:image/png;base64,AA==',
      crop: { height: 1, width: 1, x: 0, y: 0 },
    }))).toBeTruthy();
    for (const url of [
      'https://resume.example/photo.png',
      'data:image/gif;base64,AA==',
      'data:image/png;base64,AA',
      'data:image/png;base64,',
      'data:image/svg+xml;base64,AA==',
    ]) {
      rejected(photo({ url, crop: null }));
    }
    rejected(photo({ url: 'data:image/png;base64,AA==' }));
    rejected(photo({ url: 'data:image/png;base64,AA==', crop: null, x: 1 }));
    rejected(photo('data:image/png;base64,AA=='));
    const crop = { height: 0.5, width: 0.5, x: 0.25, y: 0 };
    for (const change of [
      { x: -0.01 },
      { y: 1.01 },
      { width: 0 },
      { height: 0 },
      { width: 1.5 },
      { x: '0' },
      { extra: 1 },
    ]) {
      rejected(photo({
        url: 'data:image/png;base64,AA==',
        crop: { ...crop, ...change },
      }));
    }
    const { x: _x, ...missing } = crop;
    rejected(photo({ url: 'data:image/png;base64,AA==', crop: missing }));
  });

  it('bounds the decoded photo bytes', () => {
    const photo = (bytes: number) => withCard({
      photo: {
        url: `data:image/png;base64,${Buffer.alloc(bytes).toString('base64')}`,
        crop: null,
      },
    });
    expect(decode(photo(PRINT_PHOTO_MAX_BYTES))).toBeTruthy();
    rejected(photo(PRINT_PHOTO_MAX_BYTES + 1));
  });

  it('accepts only a lowercase six-digit hex accent', () => {
    for (const accent of ['#000000', '#1d4ed8', '#ffffff']) {
      expect(decode(withCard({ accent }))).toBeTruthy();
    }
    for (const accent of [
      '#1D4ED8',
      '#1d4ed',
      '#1d4ed80',
      '1d4ed8',
      'red',
      'rgb(0,0,0)',
      '#1d4ed8;',
      null,
    ]) {
      rejected(withCard({ accent }));
    }
  });
});
