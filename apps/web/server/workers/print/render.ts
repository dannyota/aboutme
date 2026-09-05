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
    const html = [
      '<!doctype html>',
      `<html lang="${envelope.lng}"><head>`,
      '<meta charset="utf-8">',
      '<meta name="viewport" content="width=device-width, initial-scale=1">',
      '<title>Resume</title>',
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
