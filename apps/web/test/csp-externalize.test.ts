// @vitest-environment node

import { Buffer } from 'node:buffer';
import { describe, expect, it } from 'vitest';

import { externalizeNuxtBootstrap } from '../server/utils/cspExternalize';

describe('Nuxt CSP bootstrap externalizer', () => {
  it('moves executable config and payload bytes out of inline scripts', () => {
    const config = '{"app":{"baseURL":"/","buildId":"fixed"}}';
    const payload = '[["Reactive",1],{"unsafe":"<tag>"}]';
    const html = [
      '<main>safe</main>',
      `<script>window.__NUXT__={};window.__NUXT__.config=${config}</script>`,
      '<script type="application/json" data-nuxt-data="nuxt-app"',
      ` data-ssr="true" id="__NUXT_DATA__">${payload}</script>`,
    ].join('');

    const output = externalizeNuxtBootstrap(html);
    expect(output).not.toContain('<script>');
    expect(output).not.toContain('type="application/json"');
    expect(output).toContain('<script src="/csp-bootstrap.js"></script>');
    const encoded = [...output.matchAll(/content="([A-Za-z0-9+/=]+)"/g)]
      .map((match) => Buffer.from(match[1]!, 'base64').toString('utf8'));
    expect(encoded).toEqual([config, payload]);
  });

  it('leaves non-Nuxt bodies unchanged and rejects partial bootstraps', () => {
    expect(externalizeNuxtBootstrap('<main>plain</main>'))
      .toBe('<main>plain</main>');
    expect(() => externalizeNuxtBootstrap(
      '<script>window.__NUXT__={};window.__NUXT__.config={}</script>',
    )).toThrow('Nuxt bootstrap must contain config and payload together.');
  });
});
