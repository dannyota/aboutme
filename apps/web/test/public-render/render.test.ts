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

const STYLE_VERSION = '0123456789abcdef';
const SCRIPT_VERSION = 'fedcba9876543210';
const VERSIONS = { style: STYLE_VERSION, script: SCRIPT_VERSION };

describe('public Vue worker document', () => {
  it('uses Task 08 JSON-LD bytes and a complete titled document', async () => {
    const html = await renderPublicResume(request(), VERSIONS);
    await expect(renderPublicResume(request(), VERSIONS))
      .resolves.toBe(html);
    expect(html).toContain(
      '<title>Ada &lt;&amp;&gt; Lovelace — Resume</title>',
    );
    expect(html).toContain(
      '<link rel="canonical" href="https://resume.example/ada1">',
    );
    expect(html).toContain(
      '<meta property="og:image" content="https://resume.example/api/v1/public/resumes/ada1/og.png">',
    );
    expect(html).toContain('<meta property="og:image:width" content="1200">');
    expect(html).toContain('<meta property="og:image:height" content="630">');
    expect(html).toContain(
      '<meta name="twitter:card" content="summary_large_image">',
    );
    expect(html).toContain(
      '<meta name="twitter:image" content="https://resume.example/api/v1/public/resumes/ada1/og.png">',
    );
    expect(html).toContain('<main id="public-resume" data-revision="1">');
    const skipLinks = html.match(
      /<a href="#public-resume">Skip to content<\/a>/gu,
    );
    expect(skipLinks).toHaveLength(1);
    expect(html).toContain(
      '</head><body><a href="#public-resume">Skip to content</a><main ',
    );
    // The hydration script is versioned the same way, so a returning browser
    // never runs a previous release's cached script against new HTML.
    expect(html).toContain(
      '<script type="module" '
      + `src="/_nuxt/assets/public-resume.mjs?v=${SCRIPT_VERSION}"></script>`,
    );
    // The template's CSS and fonts come from the same self-hosted stylesheets
    // the print document uses; without them the page renders unstyled.
    // The stylesheets keep fixed names but are cached for a year, so each
    // link carries the build's style version to fetch fresh CSS per release.
    expect(html).toContain(
      '<link rel="stylesheet" '
      + `href="/_nuxt/assets/print-fonts.css?v=${STYLE_VERSION}">`
      + '<link rel="stylesheet" '
      + `href="/_nuxt/assets/print.css?v=${STYLE_VERSION}">`,
    );
    expect(html).not.toMatch(/<style\b/iu);
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
    const html = await renderPublicResume(value, VERSIONS);
    expect(html).not.toContain(
      'application/ld+json',
    );
    expect(html).toContain(
      '<meta property="og:image" content="https://resume.example/api/v1/public/resumes/ada1/og.png">',
    );
    expect(html).toContain(
      '<meta name="twitter:image" content="https://resume.example/api/v1/public/resumes/ada1/og.png">',
    );
  });
});

describe('public style version', () => {
  it('refuses a missing or malformed version', async () => {
    for (const version of ['', 'ABCDEF0123456789', '0123', 'x'.repeat(16)]) {
      await expect(renderPublicResume(request(), {
        style: version,
        script: SCRIPT_VERSION,
      })).rejects.toThrow();
      await expect(renderPublicResume(request(), {
        style: STYLE_VERSION,
        script: version,
      })).rejects.toThrow();
    }
  });
});
