import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';

// This is the explicit worker-relative component boundary.
// eslint-disable-next-line max-len
import PublicResumeApp from '../../../app/components/public/PublicResumeApp.vue';
import {
  PUBLIC_RENDER_FAILURE,
  PUBLIC_RENDER_HTML_MAX_BYTES,
  type PublicRenderRequest,
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

const escapeText = (value: string): string => value
  .replaceAll('&', '&amp;')
  .replaceAll('<', '&lt;')
  .replaceAll('>', '&gt;')
  .replaceAll('"', '&quot;');

const isHTTPSURL = (value: string): boolean => {
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'https:' && parsed.host !== '';
  } catch {
    return false;
  }
};

const jsonLd = (request: PublicRenderRequest): string => {
  if (!request.discoveryEnabled) return '';
  const person = request.publicResume.document.personalDetails;
  const details = person.details ?? [];
  const sameAs = [...new Set(details.flatMap((detail) => (
    ['website', 'linkedin', 'github', 'twitter'].includes(detail.type)
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

export async function renderPublicResume(
  request: PublicRenderRequest,
): Promise<string> {
  try {
    const body = await renderToString(
      createSSRApp({
        render: () =>
          h(PublicResumeApp, { publicResume: request.publicResume }),
      }),
    );
    const person = request.publicResume.document.personalDetails;
    const discoveryScript = jsonLd(request);
    const head = [
      '<meta charset="utf-8">',
      '<meta name="viewport" content="width=device-width, initial-scale=1">',
      `<title>${escapeText(`${person.fullName} — Resume`)}</title>`,
      `<link rel="canonical" href="${request.canonicalOrigin}/`,
      `${request.publicResume.slug}">`,
      discoveryScript,
    ].join('');
    const html = [
      '<!doctype html>',
      `<html lang="${request.publicResume.lng}"><head>${head}</head>`,
      '<body><main id="public-resume" ',
      `data-revision="${request.publicResume.revision}">${body}</main>`,
      '<script type="module" src="/_nuxt/assets/public-resume.mjs"></script>',
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
