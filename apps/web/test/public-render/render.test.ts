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
    pageTitle: 'Ada <&> Lovelace — Resume',
    faviconHref: '',
  };
};

const STYLE_VERSION = '0123456789abcdef';
const SCRIPT_VERSION = 'fedcba9876543210';
const VERSIONS = { style: STYLE_VERSION, script: SCRIPT_VERSION };

describe('public Vue worker document', () => {
  it('uses the Go JSON-LD bytes and a complete titled document', async () => {
    const html = await renderPublicResume(request(), VERSIONS);
    await expect(renderPublicResume(request(), VERSIONS))
      .resolves.toBe(html);
    expect(html).toContain(
      '<title>Ada &lt;&amp;&gt; Lovelace — Resume</title>',
    );
    expect(html).toContain(
      '<link rel="canonical" href="https://resume.example/ada1">',
    );
    // Stops Safari and other browsers from auto-linking digit runs such as
    // date ranges into tel: links, without touching the explicit tel:/
    // mailto: anchors the contact details render.
    expect(html).toContain(
      '<meta name="format-detection" '
      + 'content="telephone=no, date=no, address=no, email=no">',
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

describe('owner page title and emoji favicon', () => {
  const ROCKET_HREF = 'data:image/svg+xml,'
    + '%3Csvg%20xmlns%3D%27http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%27%20'
    + 'viewBox%3D%270%200%20100%20100%27%3E%3Ctext%20y%3D%27.9em%27%20'
    + 'font-size%3D%2790%27%3E%F0%9F%9A%80%3C%2Ftext%3E%3C%2Fsvg%3E';

  it('writes the given title as escaped text', async () => {
    const value = request();
    value.pageTitle = 'Danny <b>&</b> "friends" \'x\'';
    const html = await renderPublicResume(value, VERSIONS);
    expect(html).toContain(
      '<title>Danny &lt;b&gt;&amp;&lt;/b&gt; &quot;friends&quot; \'x\'</title>',
    );
    expect(html.match(/<title>/gu)).toHaveLength(1);
  });

  it('links the favicon only when one is given', async () => {
    const none = await renderPublicResume(request(), VERSIONS);
    expect(none).not.toContain('rel="icon"');
    const value = request();
    value.faviconHref = ROCKET_HREF;
    const html = await renderPublicResume(value, VERSIONS);
    expect(html).toContain(`<link rel="icon" href="${ROCKET_HREF}">`);
    expect(html.match(/rel="icon"/gu)).toHaveLength(1);
  });
});

describe('public page measure and PDF download', () => {
  const downloadLink = (html: string) =>
    /<a class="public-download" href="([^"]*)">([^<]*)<\/a>/u.exec(html);

  it('links the public PDF only while download is enabled', async () => {
    const hidden = await renderPublicResume(request(), VERSIONS);
    expect(hidden).not.toContain('public-download');
    expect(hidden).not.toContain('/pdf');

    const value = request();
    value.publicResume.downloadEnabled = true;
    const html = await renderPublicResume(value, VERSIONS);
    const link = downloadLink(html);
    expect(link?.[1]).toBe('/api/v1/public/resumes/ada1/pdf');
    expect(link?.[2]).toBe('Download PDF');
    expect(html.match(/public-download/gu)).toHaveLength(1);
  });

  it('labels the link in the resume language', async () => {
    for (const [lng, label] of [
      ['vi', 'Tải PDF'],
      ['vi-VN', 'Tải PDF'],
      ['en-US', 'Download PDF'],
      ['und', 'Download PDF'],
    ] as const) {
      const value = request();
      value.publicResume.downloadEnabled = true;
      value.publicResume.lng = lng;
      const html = await renderPublicResume(value, VERSIONS);
      expect(downloadLink(html)?.[2], lng).toBe(label);
    }
  });

  it('marks the column count for the public measure', async () => {
    const one = await renderPublicResume(request(), VERSIONS);
    expect(one).toContain('<div class="public-resume-page" data-columns="1"');
    const value = request();
    value.publicResume.document.customization.layout.columns = 2;
    const two = await renderPublicResume(value, VERSIONS);
    expect(two).toContain('<div class="public-resume-page" data-columns="2"');
  });
});

describe('JSON-LD sameAs parity with the Go validator', () => {
  // The public HTML validator requires this script to byte-equal Go's
  // publicformat.JSONLD output. Both suites read the same corpus.
  it('picks exactly the sameAs values Go picks', async () => {
    const corpusPath = resolve(
      process.cwd(),
      '../server/internal/publicformat/testdata/public-format',
      'sameas-parity.json',
    );
    const corpus = JSON.parse(readFileSync(corpusPath, 'utf8')) as {
      details: { type: string; value: string }[];
      sameAs: string[];
    };
    const value = request();
    value.publicResume.document.personalDetails.details = corpus.details
      .map((detail, index) => ({
        id: `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`,
        ...detail,
      })) as typeof value.publicResume.document.personalDetails.details;
    const html = await renderPublicResume(value, VERSIONS);
    const script = /<script type="application\/ld\+json">(.*?)<\/script>/u
      .exec(html)?.[1];
    expect(JSON.parse(script!).mainEntity.sameAs).toEqual(corpus.sameAs);
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

describe('link-preview head', () => {
  // The head between the canonical link and the stylesheets, where the
  // preview tags sit (docs/design/link-previews.md, "Page head").
  const previewHead = (html: string) => html.slice(
    html.indexOf('<link rel="canonical"'),
    html.indexOf('<link rel="stylesheet"'),
  );

  it('writes every tag in order for a Vietnamese resume', async () => {
    const value = {
      ...request(),
      preview: {
        title: 'Nguyễn "An" <Dev> & Co',
        description: 'Kỹ sư phần mềm, viết \'backend\' & <API>.',
        locale: 'vi_VN',
        imageAlt: 'Nguyễn "An" · Kỹ sư',
      },
    };
    value.publicResume.lng = 'vi';
    const html = await renderPublicResume(value, VERSIONS);
    expect(previewHead(html)).toBe([
      '<link rel="canonical" href="https://resume.example/ada1">',
      '<meta name="description" content="Kỹ sư phần mềm, viết &#39;backend&#39;'
      + ' &amp; &lt;API&gt;.">',
      '<meta property="og:type" content="profile">',
      '<meta property="og:site_name" content="aboutme.vn">',
      '<meta property="og:title" content="Nguyễn &quot;An&quot; &lt;Dev&gt;'
      + ' &amp; Co">',
      '<meta property="og:description" content="Kỹ sư phần mềm, viết '
      + '&#39;backend&#39; &amp; &lt;API&gt;.">',
      '<meta property="og:url" content="https://resume.example/ada1">',
      '<meta property="og:locale" content="vi_VN">',
      '<meta property="og:image" '
      + 'content="https://resume.example/api/v1/public/resumes/ada1/og.png">',
      '<meta property="og:image:type" content="image/png">',
      '<meta property="og:image:width" content="1200">',
      '<meta property="og:image:height" content="630">',
      '<meta property="og:image:alt" content="Nguyễn &quot;An&quot; · Kỹ sư">',
      '<meta name="twitter:card" content="summary_large_image">',
      '<meta name="twitter:image" '
      + 'content="https://resume.example/api/v1/public/resumes/ada1/og.png">',
      '<meta name="twitter:image:alt" content="Nguyễn &quot;An&quot; · Kỹ sư">',
    ].join(''));
    expect(html).toContain('<html lang="vi">');
  });

  it('writes every tag in order for an English resume', async () => {
    const value = {
      ...request(),
      preview: {
        title: 'Ada Lovelace',
        description: 'Writes the first published program.',
        locale: 'en_US',
        imageAlt: 'Ada Lovelace · Analyst',
      },
    };
    const html = await renderPublicResume(value, VERSIONS);
    expect(previewHead(html)).toBe([
      '<link rel="canonical" href="https://resume.example/ada1">',
      '<meta name="description" '
      + 'content="Writes the first published program.">',
      '<meta property="og:type" content="profile">',
      '<meta property="og:site_name" content="aboutme.vn">',
      '<meta property="og:title" content="Ada Lovelace">',
      '<meta property="og:description" '
      + 'content="Writes the first published program.">',
      '<meta property="og:url" content="https://resume.example/ada1">',
      '<meta property="og:locale" content="en_US">',
      '<meta property="og:image" '
      + 'content="https://resume.example/api/v1/public/resumes/ada1/og.png">',
      '<meta property="og:image:type" content="image/png">',
      '<meta property="og:image:width" content="1200">',
      '<meta property="og:image:height" content="630">',
      '<meta property="og:image:alt" content="Ada Lovelace · Analyst">',
      '<meta name="twitter:card" content="summary_large_image">',
      '<meta name="twitter:image" '
      + 'content="https://resume.example/api/v1/public/resumes/ada1/og.png">',
      '<meta name="twitter:image:alt" content="Ada Lovelace · Analyst">',
    ].join(''));
  });

  it('omits og:locale when no locale maps', async () => {
    const value = {
      ...request(),
      preview: {
        title: 'Ada',
        description: 'Resume on aboutme.vn',
        locale: '',
        imageAlt: 'Ada',
      },
    };
    value.publicResume.lng = 'und';
    const html = await renderPublicResume(value, VERSIONS);
    expect(html).not.toContain('og:locale');
    expect(html).toContain(
      '<meta property="og:url" content="https://resume.example/ada1">'
      + '<meta property="og:image" ',
    );
  });

  it('never writes tags the server validator rejects', async () => {
    const html = await renderPublicResume({
      ...request(),
      preview: {
        title: 'Ada',
        description: 'Analyst',
        locale: 'en_US',
        imageAlt: 'Ada',
      },
    }, VERSIONS);
    const rejected = ['twitter:title', 'twitter:description', 'theme-color'];
    for (const name of rejected) {
      expect(html).not.toContain(name);
    }
  });

  it('keeps the image-only head without preview text', async () => {
    const html = await renderPublicResume(request(), VERSIONS);
    expect(previewHead(html)).toBe([
      '<link rel="canonical" href="https://resume.example/ada1">',
      '<meta property="og:image" '
      + 'content="https://resume.example/api/v1/public/resumes/ada1/og.png">',
      '<meta property="og:image:width" content="1200">',
      '<meta property="og:image:height" content="630">',
      '<meta name="twitter:card" content="summary_large_image">',
      '<meta name="twitter:image" '
      + 'content="https://resume.example/api/v1/public/resumes/ada1/og.png">',
    ].join(''));
    expect(html).not.toMatch(/name="description"|og:title|og:type/u);
  });
});
