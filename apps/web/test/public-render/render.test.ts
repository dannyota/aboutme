// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import { renderPublicResume } from '../../server/workers/public-render/render';

const request = () => {
  const document = JSON.parse(
    readFileSync(
      resolve(process.cwd(), '../../packages/schema/fixtures/minimal.json'),
      'utf8',
    ),
  );
  document.personalDetails = {
    fullName: 'Ada <&> Lovelace',
    headline: '  Computes\nthings  ',
    photo: { url: 'https://resume.example/photo' },
    details: [
      {
        id: '00000000-0000-4000-8000-000000000001',
        type: 'website',
        value: 'https://one.example/path',
      },
      {
        id: '00000000-0000-4000-8000-000000000002',
        type: 'github',
        value: 'https://one.example/path',
      },
      {
        id: '00000000-0000-4000-8000-000000000003',
        type: 'email',
        value: 'ada@example.test',
      },
      {
        id: '00000000-0000-4000-8000-000000000005',
        type: 'twitter',
        value: 'https://?query',
      },
      {
        id: '00000000-0000-4000-8000-000000000006',
        type: 'twitter',
        value: 'https://#fragment',
      },
      {
        id: '00000000-0000-4000-8000-000000000007',
        type: 'twitter',
        value: 'http://not-https.example',
      },
      {
        id: '00000000-0000-4000-8000-000000000008',
        type: 'twitter',
        value: 'https://user@port.example:8443/path',
      },
    ],
  };
  document.content = {
    profile: {
      sectionType: 'profile',
      entries: [{ id: '00000000-0000-4000-8000-000000000004' }],
    },
  };
  document.customization.layout.sections.main = ['profile'];
  return {
    publicResume: {
      slug: 'ada1',
      revision: '1',
      lng: 'en',
      downloadEnabled: false,
      document,
    },
    mode: 'continuous' as const,
    canonicalOrigin: 'https://resume.example',
    discoveryEnabled: true,
  };
};

describe('public Vue worker document', () => {
  it('uses Task 08 JSON-LD bytes and a complete titled document', async () => {
    const html = await renderPublicResume(request());
    await expect(renderPublicResume(request())).resolves.toBe(html);
    expect(html).toContain(
      '<title>Ada &lt;&amp;&gt; Lovelace — Resume</title>',
    );
    expect(html).toContain(
      '<link rel="canonical" href="https://resume.example/ada1">',
    );
    expect(html).toContain('<main id="public-resume" data-revision="1">');
    const skipLinks = html.match(
      /<a href="#public-resume">Skip to content<\/a>/gu,
    );
    expect(skipLinks).toHaveLength(1);
    expect(html).toContain(
      '</head><body><a href="#public-resume">Skip to content</a><main ',
    );
    expect(html).toContain('/_nuxt/assets/public-resume.mjs');
    expect(html).toContain(
      '<script type="application/ld+json">'
      + '{"@context":"https://schema.org","@type":"ProfilePage",'
      + '"url":"https://resume.example/ada1",'
      + '"name":"Ada \\u003c\\u0026\\u003e Lovelace — Resume",'
      + '"inLanguage":"en","mainEntity":{"@type":"Person",'
      + '"name":"Ada \\u003c\\u0026\\u003e Lovelace",'
      + '"description":"  Computes\\nthings  ",'
      + '"image":"https://resume.example/photo",'
      + '"sameAs":["https://one.example/path",'
      + '"https://user@port.example:8443/path"]}}</script>',
    );
  });

  it('omits the JSON-LD script when discovery is disabled', async () => {
    const value = request();
    value.discoveryEnabled = false;
    await expect(renderPublicResume(value)).resolves.not.toContain(
      'application/ld+json',
    );
  });
});
