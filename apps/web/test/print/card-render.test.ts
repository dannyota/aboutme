// @vitest-environment node

import { JSDOM } from 'jsdom';
import { describe, expect, it } from 'vitest';

import { decodePrintEnvelope } from '../../server/utils/print/envelope';
import { renderPrintCard } from '../../server/workers/print/render';
import { cardEnvelope } from './fixture';

const parse = (html: string) => new JSDOM(html).window.document;
// The text of each card paragraph, in order.
const text = (html: string): string =>
  [...parse(html).querySelectorAll('main p')]
    .map((paragraph) => (paragraph.textContent ?? '').trim())
    .join(' | ');

// docs/design/link-previews.md, "Preview card": the card page is one print
// document with no script, and shows only the envelope's text.
describe('print card page', () => {
  it('is one print document with no script', async () => {
    const html = await renderPrintCard(cardEnvelope());
    const document = parse(html);
    expect(document.querySelectorAll('[data-print-document="true"]'))
      .toHaveLength(1);
    expect(document.scripts).toHaveLength(0);
    expect(html).not.toMatch(/<script\b|\son[a-z]+=/iu);
    expect(document.documentElement.lang).toBe('vi');
    expect(document.title).toBe('aboutme.vn/nguyen-an');
    expect(
      [...document.querySelectorAll('link[rel="stylesheet"]')]
        .map((link) => link.getAttribute('href')),
    ).toEqual(['/_nuxt/assets/print-fonts.css', '/_nuxt/assets/print.css']);
  });

  it('shows only the name, headline, photo, and address', async () => {
    const html = await renderPrintCard(cardEnvelope());
    expect(text(html)).toBe(
      'Nguyễn Văn An | Kỹ sư phần mềm | aboutme.vn/nguyen-an',
    );
    const images = [...parse(html).querySelectorAll('img')];
    expect(images.map((image) => image.getAttribute('src')))
      .toEqual(['data:image/png;base64,AA==']);
  });

  it('shows the address in place of a missing name', async () => {
    const envelope = cardEnvelope();
    envelope.card.name = null;
    envelope.card.headline = null;
    envelope.card.photo = null;
    const html = await renderPrintCard(envelope);
    expect(text(html)).toBe('aboutme.vn/nguyen-an | aboutme.vn');
    expect(parse(html).querySelectorAll('img')).toHaveLength(0);
  });

  it('writes card text as escaped text', async () => {
    const envelope = cardEnvelope();
    envelope.card.name = '<i>"A" & B';
    envelope.card.headline = '</main><script>';
    const html = await renderPrintCard(envelope);
    expect(html).not.toMatch(/<script\b|<i>/iu);
    expect(text(html)).toBe(
      '<i>"A" & B | </main><script> | aboutme.vn/nguyen-an',
    );
    expect(parse(html).querySelectorAll('main')).toHaveLength(1);
  });

  it('never carries a contact sentinel sent beside the card', () => {
    const sentinels = [
      'sentinel-card-email@example.com',
      '+84 900 000 001',
      'https://sentinel-card.example/profile',
    ];
    const withContacts = {
      ...cardEnvelope(),
      card: { ...cardEnvelope().card, contacts: sentinels },
    };
    expect(() => decodePrintEnvelope(JSON.stringify(withContacts))).toThrow();
    const withDocument = { ...cardEnvelope(), details: sentinels };
    expect(() => decodePrintEnvelope(JSON.stringify(withDocument))).toThrow();
  });

  it('keeps the accent off every text color', async () => {
    const envelope = cardEnvelope();
    envelope.card.accent = '#abcdef';
    const html = await renderPrintCard(envelope);
    const document = parse(html);
    for (const element of document.querySelectorAll('[style]')) {
      const style = element.getAttribute('style') ?? '';
      if (!style.includes('#abcdef')) continue;
      expect(style).not.toMatch(/(?:^|;)\s*color\s*:/u);
    }
  });
});
