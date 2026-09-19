import type { Resume } from '@aboutme/schema';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';

import PrintResumeApp from '../../../app/components/print/PrintResumeApp.vue';
import {
  renderPageRule,
  useResumeStyles,
} from '../../../app/components/resume/useResumeStyles';
import {
  PRINT_FAILURE,
  PRINT_HTML_MAX_BYTES,
  type PrintEnvelope,
} from '../../utils/print/envelope';

const HTML_ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
};

/**
 * The print page title, which Chromium copies into the PDF Title. Control and
 * format characters become spaces, white space collapses, and a blank name
 * leaves "Resume" (docs/adr/0045-pdf-download-name-and-metadata.md).
 */
export function printTitle(fullName: string): string {
  const name = fullName
    .replace(/[\p{Cc}\p{Cf}]/gu, ' ')
    .replace(/\s+/gu, ' ')
    .trim();
  const title = name === '' ? 'Resume' : `${name} - Resume`;
  return title.replace(/[&<>"]/gu, (character) => HTML_ESCAPES[character]!);
}

export async function renderPrintResume(
  envelope: PrintEnvelope,
): Promise<string> {
  try {
    const body = await renderToString(createSSRApp({
      render: () => h(PrintResumeApp, {
        document: envelope.document,
        lng: envelope.lng,
      }),
    }));
    const styles = useResumeStyles(
      envelope.document.customization as unknown as Resume['customization'],
    );
    const title = printTitle(envelope.document.personalDetails.fullName);
    const html = [
      '<!doctype html>',
      `<html lang="${envelope.lng}"><head>`,
      '<meta charset="utf-8">',
      '<meta name="viewport" content="width=device-width, initial-scale=1">',
      `<title>${title}</title>`,
      '<link rel="stylesheet" href="/_nuxt/assets/print-fonts.css">',
      '<link rel="stylesheet" href="/_nuxt/assets/print.css">',
      `<style>${renderPageRule(styles.page)}</style>`,
      '</head><body class="resume-print">',
      `<main data-print-document="true" data-revision="${envelope.revision}">`,
      body,
      '</main></body></html>',
    ].join('');
    if (
      Buffer.byteLength(html, 'utf8') > PRINT_HTML_MAX_BYTES
      || /<script\b/iu.test(html)
    ) throw new Error();
    return html;
  } catch {
    throw new Error(PRINT_FAILURE);
  }
}
