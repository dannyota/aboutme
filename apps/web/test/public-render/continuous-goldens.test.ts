// @vitest-environment node

import type { Resume } from '@aboutme/schema';
import { JSDOM } from 'jsdom';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import { applyTemplate } from '../../app/components/resume/applyTemplate';
import { FIXED_PHOTO_DATA_URL } from '../../app/pages/_harness/photo-fixture';
import type {
  PublicRenderResumeRequest,
} from '../../server/utils/public-render/envelope';
import { renderPublicResume } from '../../server/workers/public-render/render';
import {
  buildGoldenMatrix,
  buildStartingDocument,
} from '../renderer/golden.generate.mts';

const workspace = resolve(import.meta.dirname, '../../../..');
const readJson = (path: string): unknown =>
  JSON.parse(readFileSync(resolve(workspace, path), 'utf8'));
const VERSIONS = { style: '0123456789abcdef', script: 'fedcba9876543210' };

const publicDocument = (presetFilename: string, start: 1 | 2): Resume => {
  const starting = buildStartingDocument(
    readJson('packages/schema/fixtures/full.json'),
    start,
  ) as Resume;
  const preset = readJson(`packages/schema/templates/${presetFilename}`) as {
    customization: Resume['customization'];
  };
  const document = {
    ...starting,
    customization: applyTemplate(
      {
        ...starting.customization,
        pageFormat: preset.customization.pageFormat,
      },
      preset as Parameters<typeof applyTemplate>[1],
      starting.content,
      'en',
    ),
  };
  const photo = document.personalDetails.photo!;
  return {
    ...document,
    personalDetails: {
      ...document.personalDetails,
      photo: { url: FIXED_PHOTO_DATA_URL, crop: photo.crop },
    },
  } as unknown as Resume;
};

const cells = buildGoldenMatrix().filter((cell: { mode: string }) =>
  cell.mode === 'continuous') as {
  presetFilename: string;
  start: 1 | 2;
  filename: string;
}[];

describe('public page server HTML', () => {
  it('covers every preset at both starting placements', () => {
    expect(cells).toHaveLength(40);
  });

  it.each(cells.map((cell) => [cell.filename, cell] as const))(
    'puts exactly the continuous golden inside main for %s',
    async (_name, cell) => {
      const request = {
        publicResume: {
          slug: 'ada',
          revision: '1',
          lng: 'en',
          downloadEnabled: true,
          document: publicDocument(cell.presetFilename, cell.start),
        },
        mode: 'continuous',
        canonicalOrigin: 'https://aboutme.example',
        discoveryEnabled: false,
        pageTitle: 'Ada Lovelace Resume',
        faviconHref: '',
      } as unknown as PublicRenderResumeRequest;
      const html = await renderPublicResume(request, VERSIONS);
      const page = new JSDOM(html).window.document;
      const articles = page.querySelectorAll(
        'main#public-resume > .public-resume-page > article',
      );
      expect(articles).toHaveLength(1);
      expect(page.querySelector('.public-pages, .paged-resume')).toBeNull();
      const golden = readFileSync(
        resolve(import.meta.dirname, '../renderer/golden', cell.filename),
        'utf8',
      );
      expect(html).toContain(`${golden}</div></main>`);
    },
  );
});
