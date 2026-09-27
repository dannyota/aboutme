import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';

// These are the explicit worker-relative component boundaries.
// eslint-disable-next-line max-len
import PublicResumeApp from '../../../app/components/public/PublicResumeApp.vue';
import PublicGate from '../../../app/components/public/PublicGate.vue';
import {
  PUBLIC_RENDER_FAILURE,
  PUBLIC_RENDER_HTML_MAX_BYTES,
  type PublicRenderGateRequest,
  type PublicRenderPreview,
  type PublicRenderResumeRequest,
} from '../../utils/public-render/envelope';

const jsonString = (value: string): string => {
  let result = '"';
  for (const character of value) {
    const code = character.codePointAt(0)!;
    const escaped = {
      '"': '\\"', '\\': '\\\\', '\b': '\\b', '\f': '\\f',
      '\n': '\\n', '\r': '\\r', '\t': '\\t', '<': '\\u003c',
      '>': '\\u003e', '&': '\\u0026', '\u2028': '\\u2028', '\u2029': '\\u2029',
    }[character];
    const control = `\\u00${code.toString(16).padStart(2, '0')}`;
    result += escaped ?? (code <= 0x1f ? control : character);
  }
  return `${result}"`;
};

// HTML-escapes plain text for a text context (<title>). It is complete for
// RCDATA text but does not escape single quotes, so never reuse it for an HTML
// attribute value.
const escapeText = (value: string): string => value
  .replaceAll('&', '&amp;')
  .replaceAll('<', '&lt;')
  .replaceAll('>', '&gt;')
  .replaceAll('"', '&quot;');

const escapeAttribute = (value: string): string => escapeText(value)
  .replaceAll('\'', '&#39;');

const isHTTPSURL = (value: string): boolean => {
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'https:' && parsed.host !== '';
  } catch {
    return false;
  }
};

const SAME_AS_TYPES = new Set([
  'website',
  'linkedin',
  'github',
  'twitter',
  'custom',
]);

const jsonLd = (request: PublicRenderResumeRequest): string => {
  if (!request.discoveryEnabled) return '';
  const person = request.publicResume.document.personalDetails;
  const details = person.details ?? [];
  // Must pick exactly what Go's publicformat.JSONLD picks: the public HTML
  // validator requires this script to byte-equal Go's (ADR 0013).
  const sameAs = [...new Set(details.flatMap((detail) => (
    SAME_AS_TYPES.has(detail.type)
    && detail.value.startsWith('https://')
    && isHTTPSURL(detail.value)
      ? [detail.value]
      : []
  )))];
  const main = [
    '"@type":"Person"', `"name":${jsonString(person.fullName)}`,
    ...(person.headline?.trim() === '' || person.headline === undefined
      ? []
      : [`"description":${jsonString(person.headline)}`]),
    ...(person.photo === undefined
      ? []
      : [`"image":${jsonString(person.photo.url)}`]),
    ...(sameAs.length === 0
      ? []
      : [`"sameAs":[${sameAs.map(jsonString).join(',')}]`]),
  ].join(',');
  const origin = request.canonicalOrigin;
  const json = `{"@context":"https://schema.org","@type":"ProfilePage","url":${jsonString(`${origin}/${request.publicResume.slug}`)},"name":${jsonString(`${person.fullName} — Resume`)},"inLanguage":${jsonString(request.publicResume.lng)},"mainEntity":{${main}}}`;
  return `<script type="application/ld+json">${json}</script>`;
};

const meta = (
  attribute: 'name' | 'property',
  key: string,
  value: string,
): string =>
  `<meta ${attribute}="${key}" content="${escapeAttribute(value)}">`;

// The link-preview tags in the order of docs/design/link-previews.md, "Page
// head". The server's validator accepts each exactly once with the value it
// computed, and rejects twitter:title, twitter:description and theme-color.
// A request without preview text gets only the image tags.
const previewHead = (
  pageURL: string,
  preview: PublicRenderPreview | undefined,
  imageURL: string,
): string => {
  const image = [
    meta('property', 'og:image', imageURL),
    ...(preview === undefined
      ? []
      : [meta('property', 'og:image:type', 'image/png')]),
    meta('property', 'og:image:width', '1200'),
    meta('property', 'og:image:height', '630'),
  ];
  const card = [
    meta('name', 'twitter:card', 'summary_large_image'),
    meta('name', 'twitter:image', imageURL),
  ];
  if (preview === undefined) return [...image, ...card].join('');
  return [
    meta('name', 'description', preview.description),
    meta('property', 'og:type', 'profile'),
    meta('property', 'og:site_name', 'aboutme.vn'),
    meta('property', 'og:title', preview.title),
    meta('property', 'og:description', preview.description),
    meta('property', 'og:url', pageURL),
    ...(preview.locale === ''
      ? []
      : [meta('property', 'og:locale', preview.locale)]),
    ...image,
    meta('property', 'og:image:alt', preview.imageAlt),
    ...card,
    meta('name', 'twitter:image:alt', preview.imageAlt),
  ].join('');
};

/**
 * Content versions of the fixed-name, immutable assets the public page loads;
 * each is 16 lowercase hex characters.
 */
export interface PublicAssetVersions {
  readonly style: string;
  readonly script: string;
}

export async function renderPublicResume(
  request: PublicRenderResumeRequest,
  versions: PublicAssetVersions,
): Promise<string> {
  try {
    const { style: styleVersion, script: scriptVersion } = versions;
    if (![styleVersion, scriptVersion].every((version) =>
      /^[0-9a-f]{16}$/u.test(version))) {
      throw new Error();
    }
    const body = await renderToString(
      createSSRApp({
        render: () =>
          h(PublicResumeApp, {
            publicResume: request.publicResume,
            homeHref: `${request.canonicalOrigin}/`,
          }),
      }),
    );
    const discoveryScript = jsonLd(request);
    // The stored card's versioned URL when the server names one, else the
    // og.png alias (docs/design/link-previews.md, "Page head").
    const imageURL = request.preview?.imageUrl ?? [
      request.canonicalOrigin,
      '/api/v1/public/resumes/',
      request.publicResume.slug,
      '/og.png',
    ].join('');
    const head = [
      '<meta charset="utf-8">',
      '<meta name="viewport" content="width=device-width, initial-scale=1">',
      // Safari auto-links digit runs such as date ranges into tel: links;
      // this opts the page out without touching the explicit tel:/mailto:
      // anchors the contact details render.
      '<meta name="format-detection" '
      + 'content="telephone=no, date=no, address=no, email=no">',
      // The server computes the title and favicon href from the owner's
      // settings, and its validator accepts exactly these (ADR 0014).
      `<title>${escapeText(request.pageTitle)}</title>`,
      request.faviconHref === ''
        ? ''
        : `<link rel="icon" href="${escapeAttribute(request.faviconHref)}">`,
      `<link rel="canonical" href="${request.canonicalOrigin}/`,
      `${request.publicResume.slug}">`,
      previewHead(
        `${request.canonicalOrigin}/${request.publicResume.slug}`,
        request.preview,
        imageURL,
      ),
      // Template CSS and fonts, self-hosted and shared with the print document.
      // The version query fetches fresh copies of these fixed-name, immutable
      // files after each release that changes them.
      '<link rel="stylesheet" ',
      `href="/_nuxt/assets/print-fonts.css?v=${styleVersion}">`,
      '<link rel="stylesheet" ',
      `href="/_nuxt/assets/print.css?v=${styleVersion}">`,
      discoveryScript,
    ].join('');
    const html = [
      '<!doctype html>',
      `<html lang="${request.publicResume.lng}"><head>${head}</head>`,
      '<body><a href="#public-resume">Skip to content</a>',
      '<main id="public-resume" ',
      request.joinInvite === undefined
        ? ''
        : `data-join-invite="${escapeAttribute(request.joinInvite)}" `,
      `data-revision="${request.publicResume.revision}">${body}</main>`,
      '<script type="module" ',
      `src="/_nuxt/assets/public-resume.mjs?v=${scriptVersion}"></script>`,
      '</body></html>',
    ].join('');
    if (Buffer.byteLength(html, 'utf8') > PUBLIC_RENDER_HTML_MAX_BYTES) {
      throw new Error();
    }
    return html;
  } catch {
    throw new Error(PUBLIC_RENDER_FAILURE);
  }
}

// The gate's one inline <style>, in literal token values
// (docs/design/viewer-analytics/sign-in-to-view.md, "Gate page"). It holds
// no url(, @import, or expression(, so it passes the same unsafe-inline-style
// rule Go applies to style attributes.
const GATE_STYLE = [
  'body{margin:0;background:#F5F8FF;',
  'font-family:"Be Vietnam Pro",Inter,system-ui,sans-serif;}',
  '#public-gate{display:flex;justify-content:center;',
  'padding:32px 16px 16px;box-sizing:border-box;}',
  '.gate-card{width:100%;max-width:480px;background:#FFFFFF;',
  'border:1px solid #DCE5F5;border-radius:14px;padding:24px;',
  'box-sizing:border-box;}',
  '.gate-title{margin:0 0 12px;font-size:20px;line-height:1.3;',
  'color:#101B3F;}',
  '.gate-text{margin:0;font-size:15px;line-height:1.5;color:#101B3F;}',
  '.gate-providers{display:flex;flex-direction:column;gap:12px;',
  'margin-top:16px;}',
  '.gate-provider{display:flex;align-items:center;justify-content:center;',
  'height:44px;border:1px solid #DCE5F5;border-radius:10px;',
  'background:#FFFFFF;color:#123EDB;font-size:15px;font-weight:500;',
  'text-decoration:none;}',
  '.gate-provider:hover{background:#EAF2FF;}',
  '.gate-provider:focus-visible{outline:2px solid #1A5CEB;',
  'outline-offset:2px;}',
  '.gate-message{margin:12px 0 0;font-size:14px;color:#56648C;}',
  '.gate-refusal{margin:20px 0 0;font-size:14px;color:#56648C;}',
  '.gate-home{display:block;margin-top:16px;font-size:14px;',
  'color:#123EDB;text-decoration:underline;}',
  '@media (min-width:640px){#public-gate{min-height:100vh;',
  'align-items:center;padding:16px;}}',
].join('');

export async function renderPublicGate(
  request: PublicRenderGateRequest,
  versions: PublicAssetVersions,
): Promise<string> {
  try {
    const { style: styleVersion } = versions;
    if (!/^[0-9a-f]{16}$/u.test(styleVersion)) {
      throw new Error();
    }
    const body = await renderToString(
      createSSRApp({
        render: () =>
          h(PublicGate, {
            pageTitle: request.pageTitle,
            slug: request.slug,
            lng: request.lng,
            providers: request.providers,
            message: request.message,
            homeHref: `${request.canonicalOrigin}/`,
          }),
      }),
    );
    const pageURL = `${request.canonicalOrigin}/${request.slug}`;
    const imageURL = request.preview.imageUrl ?? [
      request.canonicalOrigin,
      '/api/v1/public/resumes/',
      request.slug,
      '/og.png',
    ].join('');
    const head = [
      '<meta charset="utf-8">',
      '<meta name="viewport" content="width=device-width, initial-scale=1">',
      `<title>${escapeText(request.pageTitle)}</title>`,
      request.faviconHref === ''
        ? ''
        : `<link rel="icon" href="${escapeAttribute(request.faviconHref)}">`,
      `<link rel="canonical" href="${pageURL}">`,
      previewHead(pageURL, request.preview, imageURL),
      '<link rel="stylesheet" ',
      `href="/_nuxt/assets/print-fonts.css?v=${styleVersion}">`,
      `<style>${GATE_STYLE}</style>`,
    ].join('');
    const html = [
      '<!doctype html>',
      `<html lang="${request.lng}"><head>${head}</head><body>`,
      body,
      '</body></html>',
    ].join('');
    if (Buffer.byteLength(html, 'utf8') > PUBLIC_RENDER_HTML_MAX_BYTES) {
      throw new Error();
    }
    return html;
  } catch {
    throw new Error(PUBLIC_RENDER_FAILURE);
  }
}
