import type { Resume } from '@aboutme/schema';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

import {
  ResumeRenderError,
  resolveRenderModel,
} from '../../app/components/resume/resolveRenderModel';

const fixture = (name: string): Resume =>
  JSON.parse(
    readFileSync(`../../packages/schema/fixtures/${name}.json`, 'utf8'),
  ) as Resume;

describe('resolved render boundary', () => {
  it('orders actual sections and does not retain raw customization', () => {
    const document = fixture('full');
    const before = structuredClone(document);
    const model = resolveRenderModel(document, {
      lng: 'vi',
      mode: 'continuous',
      photoUrl: 'data:image/png;base64,AA==',
    });
    expect(model.main.map((section) => section.key)).toEqual([
      'profile',
      'work',
      'education',
    ]);
    expect(model.sidebar.map((section) => section.key)).toEqual(
      document.customization.layout.sections.sidebar,
    );
    expect(model.header).toEqual({
      align: 'center',
      detailsLayout: 'inline',
      iconStyle: 'outline',
    });
    expect(model.photo).toEqual({
      url: 'data:image/png;base64,AA==',
      crop: document.personalDetails.photo?.crop,
    });
    expect(model).not.toHaveProperty('customization');
    expect(JSON.stringify(model)).not.toContain('photo-original.jpg');
    expect(document).toEqual(before);
  });

  it.each([
    [
      'unsupported_schema_version',
      (document: Resume) => ({ ...document, schemaVersion: 99 }) as Resume,
      { lng: 'en', mode: 'continuous' } as const,
    ],
    [
      'photo_url_required',
      (document: Resume) => document,
      { lng: 'en', mode: 'continuous' } as const,
    ],
    [
      'unexpected_photo_url',
      (_document: Resume) => fixture('minimal'),
      {
        lng: 'en',
        mode: 'continuous',
        photoUrl: 'data:image/png;base64,AA==',
      } as const,
    ],
    [
      'render_mode_unavailable',
      (_document: Resume) => fixture('minimal'),
      { lng: 'en', mode: 'paged' } as const,
    ],
  ])('throws %s', (code, makeDocument, context) => {
    expect(() => resolveRenderModel(makeDocument(fixture('full')), context))
      .toThrowError(expect.objectContaining({ code }));
  });

  it('uses the typed error class', () => {
    expect(
      () =>
        resolveRenderModel(fixture('minimal'), {
          lng: 'en',
          mode: 'paged',
        }),
    ).toThrow(ResumeRenderError);
  });
});
