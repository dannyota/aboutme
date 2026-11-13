// @vitest-environment node

import { JSDOM } from 'jsdom';
import { describe, expect, it } from 'vitest';

import {
  decodePublicRenderEnvelope,
  type PublicRenderGateRequest,
} from '../../server/utils/public-render/envelope';
import { renderPublicGate } from '../../server/workers/public-render/render';

const VERSIONS = { style: '0123456789abcdef', script: 'fedcba9876543210' };

const gate = {
  mode: 'gate',
  canonicalOrigin: 'https://resume.example',
  slug: 'ada1',
  lng: 'en',
  pageTitle: 'Ada Lovelace',
  faviconHref: '',
  preview: {
    title: 'Ada Lovelace',
    description: 'Writes the first program.',
    locale: 'en_US',
    imageAlt: 'Ada Lovelace',
  },
  providers: ['google', 'linkedin'],
  message: 'none',
};

const render = async (fields: Record<string, unknown> = {}) => {
  const request = decodePublicRenderEnvelope(
    JSON.stringify({ ...gate, ...fields }),
  ) as PublicRenderGateRequest;
  return renderPublicGate(request, VERSIONS);
};

const providerHrefs = (html: string) =>
  [...new JSDOM(html).window.document.querySelectorAll('a.gate-provider')]
    .map((anchor) => anchor.getAttribute('href'));

describe('gate provider links', () => {
  it('carry no page for the resume page', async () => {
    const html = await render();
    expect(providerHrefs(html)).toEqual([
      '/api/v1/auth/google/start?purpose=view&slug=ada1',
      '/api/v1/auth/linkedin/start?purpose=view&slug=ada1',
    ]);
    expect(html).not.toContain('page=');
  });

  it('end in the project list page', async () => {
    const html = await render({ page: 'projects' });
    expect(providerHrefs(html)).toEqual([
      '/api/v1/auth/google/start?purpose=view&slug=ada1&page=projects',
      '/api/v1/auth/linkedin/start?purpose=view&slug=ada1&page=projects',
    ]);
    expect(html).toContain(
      'href="/api/v1/auth/google/start?purpose=view&amp;slug=ada1'
      + '&amp;page=projects"',
    );
  });

  it('end in one project page', async () => {
    const html = await render({ page: 'projects/note-g', lng: 'vi' });
    expect(providerHrefs(html)).toEqual([
      '/api/v1/auth/google/start?purpose=view&slug=ada1&page=projects/note-g',
      '/api/v1/auth/linkedin/start?purpose=view&slug=ada1'
      + '&page=projects/note-g',
    ]);
  });

  it('leave the rest of the gate unchanged', async () => {
    const plain = await render();
    const withPage = await render({ page: 'projects/note-g' });
    expect(withPage.replaceAll('&amp;page=projects/note-g', ''))
      .toBe(plain);
  });
});
