// @vitest-environment node

import { readFileSync } from 'node:fs';

import { JSDOM } from 'jsdom';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import PublicResumeApp from '../../app/components/public/PublicResumeApp.vue';
import { PRINT_FAILURE } from '../../server/utils/print/envelope';
import {
  renderPrintResume,
} from '../../server/workers/print/render';
import {
  PRINT_CONTENT_SECURITY_POLICY,
} from '../../server/utils/print/protocol';
import { printEnvelope } from './fixture';

const deepFreeze = <T>(value: T): T => {
  if (value !== null && typeof value === 'object') {
    Object.freeze(value);
    Object.values(value).forEach(deepFreeze);
  }
  return value;
};

describe('private print Vue document', () => {
  it('keeps exact shared-renderer parity with public SSR', async () => {
    const envelope = printEnvelope();
    const shared = await renderToString(createSSRApp({
      render: () => h(PublicResumeApp, {
        publicResume: {
          slug: 'parity-only',
          revision: envelope.revision,
          lng: envelope.lng,
          downloadEnabled: false,
          document: envelope.document,
        },
      }),
    }));
    await expect(renderPrintResume(envelope)).resolves.toContain(shared);
  });

  it(
    'renders the shared continuous renderer with fixed print framing',
    async () => {
      const envelope = printEnvelope();
      envelope.document.personalDetails.fullName = '';
      envelope.document.customization.pageFormat = 'letter';
      const before = structuredClone(envelope);
      const html = await renderPrintResume(deepFreeze(envelope));

      expect(envelope).toEqual(before);
      expect(html).toContain('<html lang="en">');
      expect(html).toContain('<title>Resume</title>');
      expect(html).toContain(
        '<link rel="stylesheet" href="/_nuxt/assets/print-fonts.css">',
      );
      expect(html).toContain(
        '<link rel="stylesheet" href="/_nuxt/assets/print.css">',
      );
      expect(html).toContain('@page {\n  size: 8.5in 11in;');
      expect(html).toContain(
        '<main data-print-document="true" data-revision="7">',
      );
      expect(html).toContain('class="resume-document"');
      expect(html).not.toMatch(/<script\b/iu);
      expect(PRINT_CONTENT_SECURITY_POLICY).toBe(
        'sandbox allow-same-origin; default-src \'none\'; script-src \'none\'; '
        + 'style-src \'self\' \'unsafe-inline\'; font-src \'self\'; '
        + 'img-src data:; connect-src \'none\'; frame-src \'none\'; '
        + 'worker-src \'none\'; child-src \'none\'; media-src \'none\'; '
        + 'manifest-src \'none\'; object-src \'none\'; base-uri \'none\'; '
        + 'form-action \'none\'; frame-ancestors \'none\'',
      );
    },
  );

  it('uses the decoded photo only as explicit renderer context', async () => {
    const envelope = printEnvelope();
    const photoUrl = 'data:image/png;base64,AA==';
    envelope.document.personalDetails.photo = {
      url: photoUrl,
      crop: { height: 0.5, width: 0.5, x: 0.25, y: 0.25 },
    };
    const html = await renderPrintResume(envelope);
    expect(html).toContain(`src="${photoUrl}"`);
    expect(html).not.toContain('print-inline-photo');
  });

  it(
    'renders Go-sanitized corpus bytes without a client sanitizer',
    async () => {
      const corpus = JSON.parse(readFileSync(
        new URL(
          '../../../../apps/server/internal/sanitize/testdata/'
          + 'corpus-output.golden.json',
          import.meta.url,
        ),
        'utf8',
      )) as Record<string, string>;
      for (const payload of Object.values(corpus)) {
        const envelope = printEnvelope();
        envelope.document.content = {
          profile: {
            sectionType: 'profile',
            entries: [{
              id: '30000000-0000-4000-8000-000000000003',
              text: payload,
            }],
          },
        };
        envelope.document.customization.layout.sections.main = ['profile'];
        const html = await renderPrintResume(envelope);
        const document = new JSDOM(html).window.document;
        const richText = document.querySelector('.rich-text');
        if (payload === '') {
          expect(richText).toBeNull();
        } else {
          const expected = new JSDOM('').window.document
            .createElement('template');
          expected.innerHTML = payload;
          expect(richText?.innerHTML).toBe(expected.innerHTML);
        }
        expect(document.querySelector(
          'script,iframe,object,embed,form,input,meta[http-equiv]',
        )).toBeNull();
        for (const element of document.querySelectorAll('*')) {
          expect([...element.attributes].some((attribute) =>
            attribute.name.toLowerCase().startsWith('on'))).toBe(false);
        }
      }
    },
  );

  it('fails generically for a non-current document', async () => {
    const envelope = printEnvelope();
    envelope.document.schemaVersion = 1;
    await expect(renderPrintResume(envelope)).rejects.toThrow(PRINT_FAILURE);
  });
});
