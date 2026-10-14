import { expect, test, type Page } from '@playwright/test';
import { HOSTILE_CORPUS } from '@aboutme/schema/sanitizer';

import { HTML_CSP } from '../app/utils/csp';
import { NEUTRALIZATION_POLICY } from '../test/sanitizer/neutralization';
import { denyExternalRequests } from './support';

// DOMParser checks CSP when it parses a handler, before DOMPurify removes it.
// https://html.spec.whatwg.org/multipage/webappapis.html#event-handler-content-attributes
const inlineHandlerCases = new Set([
  'event-handler-img-onerror',
  'event-handler-svg-onload',
  'event-handler-anchor-onclick',
  'input-autofocus-onfocus',
]);
const inlineHandlerViolation = {
  blockedURI: 'inline',
  effectiveDirective: 'script-src-attr',
  violatedDirective: 'script-src-attr',
};
// Chromium reports a placeholder hash for a blocked handler.
// https://source.chromium.org/chromium/chromium/src/+/main:third_party/blink/renderer/core/frame/csp/csp_directive_list.cc
const inlineHandlerError
  = 'Executing inline event handler violates the following Content Security '
    + "Policy directive 'script-src 'self''."
    + " Either the 'unsafe-inline' keyword, a hash ('sha256-...'), or a "
    + "nonce ('nonce-...') is required to enable inline execution."
    + ' Note that hashes do not apply to event handlers, style attributes '
    + "and javascript: navigations unless the 'unsafe-hashes' keyword is "
    + 'present. The action has been blocked.';

interface BrowserSignals {
  readonly consoleErrors: string[];
  readonly dialogs: string[];
  readonly pageErrors: string[];
}

async function installSignals(page: Page): Promise<BrowserSignals> {
  const signals: BrowserSignals = {
    consoleErrors: [],
    dialogs: [],
    pageErrors: [],
  };
  page.on('console', (message) => {
    if (message.type() === 'error') signals.consoleErrors.push(message.text());
  });
  page.on('dialog', async (dialog) => {
    signals.dialogs.push(`${dialog.type()}:${dialog.message()}`);
    await dialog.dismiss();
  });
  page.on('pageerror', (error) => signals.pageErrors.push(error.message));
  await page.addInitScript(() => {
    const violations: object[] = [];
    Object.defineProperty(window, '__cspViolations', { value: violations });
    document.addEventListener('securitypolicyviolation', (event) => {
      violations.push({
        blockedURI: event.blockedURI,
        effectiveDirective: event.effectiveDirective,
        violatedDirective: event.violatedDirective,
      });
    });
  });
  return signals;
}

async function cspViolations(page: Page): Promise<unknown[]> {
  return page.evaluate(() =>
    (window as Window & { __cspViolations?: unknown[] }).__cspViolations ?? [],
  );
}

test.describe('hostile corpus in Chromium', () => {
  for (const record of HOSTILE_CORPUS) {
    test(record.id, async ({ page, request }, testInfo) => {
      const ssr = await request.get(`/_harness/render?payload=${record.id}`);
      expect(ssr.ok()).toBe(true);
      expect(ssr.headers()['content-security-policy']).toBe(HTML_CSP);
      const ssrBody = await ssr.text();
      expect(ssrBody).not.toContain(record.payload);
      expect(ssrBody).toContain('data-corpus-mount');
      expect(ssrBody).not.toContain('data-corpus-ready=');

      const signals = await installSignals(page);
      const external = await denyExternalRequests(page);
      let response = await page.goto(`/_harness/render?payload=${record.id}`);
      expect(response?.headers()['content-security-policy']).toBe(HTML_CSP);
      await expect(
        page.locator('[data-corpus-ready="sanitized"]'),
      ).toHaveCount(1);

      const violations = await page.locator('[data-corpus-mount]').evaluate(
        (mount, policy) => {
          const failures: { kind: string; value: string }[] = [];
          const allowedTags = new Set(policy.tags);
          const allowedSchemes = new Set(policy.schemes);
          const template = document.createElement('template');
          template.innerHTML = mount.innerHTML;
          for (const element of template.content.querySelectorAll('*')) {
            const tag = element.tagName.toLowerCase();
            if (!allowedTags.has(tag)) {
              failures.push({ kind: 'tag', value: tag });
            }
            const attributes = new Set(policy.attributes[tag] ?? []);
            for (const attribute of element.attributes) {
              const name = attribute.name.toLowerCase();
              if (!attributes.has(name)) {
                failures.push({ kind: 'attribute', value: `${tag}.${name}` });
              }
              if (policy.prefixes.some((prefix) => name.startsWith(prefix))) {
                failures.push({ kind: 'attribute-prefix', value: name });
              }
            }
            if (tag !== 'a') continue;
            const href = element.getAttribute('href');
            if (href !== null) {
              const normalized = [...href]
                .filter((character) => character.charCodeAt(0) > 0x20)
                .join('');
              const scheme = /^([a-z][a-z0-9+.-]*):/i
                .exec(normalized)?.[1]?.toLowerCase();
              if (scheme === undefined || !allowedSchemes.has(scheme)) {
                failures.push({ kind: 'href', value: href });
              }
            }
            if (element.getAttribute('rel') !== policy.externalRel) {
              failures.push({
                kind: 'rel',
                value: element.getAttribute('rel') ?? '<missing>',
              });
            }
            const target = element.getAttribute('target');
            if (target !== null && target !== '_blank') {
              failures.push({ kind: 'target', value: target });
            }
          }
          return failures;
        },
        NEUTRALIZATION_POLICY,
      );
      expect(violations).toEqual([]);
      const hasInlineHandler = inlineHandlerCases.has(record.id);
      expect(await cspViolations(page)).toEqual(
        hasInlineHandler ? [inlineHandlerViolation] : [],
      );
      expect(signals.dialogs).toEqual([]);
      expect(signals.pageErrors).toEqual([]);
      expect(signals.consoleErrors).toEqual(
        hasInlineHandler ? [inlineHandlerError] : [],
      );
      expect(external).toEqual([]);

      signals.consoleErrors.length = 0;
      signals.dialogs.length = 0;
      signals.pageErrors.length = 0;
      response = await page.goto(
        `/_harness/render?payload=${record.id}&raw=1`,
      );
      expect(response?.headers()['content-security-policy']).toBe(HTML_CSP);
      await expect(page.locator('[data-corpus-ready="raw"]')).toHaveCount(1);
      const rawEvidence = {
        consoleErrors: [...signals.consoleErrors],
        cspViolations: await cspViolations(page),
      };
      await testInfo.attach(`${record.id}-raw-csp.json`, {
        body: JSON.stringify(rawEvidence, null, 2),
        contentType: 'application/json',
      });
      expect(signals.dialogs).toEqual([]);
      expect(signals.pageErrors).toEqual([]);
      expect(external).toEqual([]);
    });
  }
});

test('sanitized markup reparses without CSP violations', async ({ page }) => {
  await page.goto('/_harness/render?payload=event-handler-anchor-onclick');
  await expect(page.locator('[data-corpus-ready="sanitized"]')).toHaveCount(1);
  const sanitizedHtml = await page.locator('[data-corpus-mount]').innerHTML();

  const cleanPage = await page.context().newPage();
  try {
    const signals = await installSignals(cleanPage);
    const external = await denyExternalRequests(cleanPage);
    await cleanPage.goto('/_harness/render?payload=js-scheme-bare');
    await expect(
      cleanPage.locator('[data-corpus-ready="sanitized"]'),
    ).toHaveCount(1);
    await cleanPage.locator('[data-corpus-mount]').evaluate(
      (mount, sanitized) => {
        mount.innerHTML = sanitized;
        mount.querySelector('a')?.addEventListener('click', (event) => {
          event.preventDefault();
        });
      },
      sanitizedHtml,
    );
    await cleanPage.locator('[data-corpus-mount] a').click();

    expect(await cspViolations(cleanPage)).toEqual([]);
    expect(signals.dialogs).toEqual([]);
    expect(signals.pageErrors).toEqual([]);
    expect(signals.consoleErrors).toEqual([]);
    expect(external).toEqual([]);
  } finally {
    await cleanPage.close();
  }
});
