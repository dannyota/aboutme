// @vitest-environment node

import { createHash } from 'node:crypto';
import { describe, expect, it } from 'vitest';

import {
  jsonLdScriptContent,
  scriptHashSource,
  withScriptSource,
} from '../server/utils/cspHash';

function sha256Source(content: string): string {
  const digest = createHash('sha256').update(content, 'utf8').digest('base64');
  return `'sha256-${digest}'`;
}

describe('jsonLdScriptContent', () => {
  it('returns null when the page has no JSON-LD script', () => {
    expect(jsonLdScriptContent('<main>plain</main>')).toBeNull();
  });

  it('extracts the exact bytes between the script tags', () => {
    const json = '{"@type":"WebSite"}';
    const html = `<head><script type="application/ld+json">${json}`
      + '</script></head>';
    expect(jsonLdScriptContent(html)).toBe(json);
  });

  it('matches the tag regardless of attribute order', () => {
    const json = '{"@type":"CreativeWork"}';
    const html = `<script data-hid="a" type="application/ld+json">${json}`
      + '</script>';
    expect(jsonLdScriptContent(html)).toBe(json);
  });

  it('never matches the externalized Nuxt payload script', () => {
    const html = '<script type="application/json" id="__NUXT_DATA__">[]'
      + '</script>';
    expect(jsonLdScriptContent(html)).toBeNull();
  });

  it('rejects a page with more than one JSON-LD script', () => {
    const html = '<script type="application/ld+json">{}</script>'
      + '<script type="application/ld+json">{}</script>';
    expect(() => jsonLdScriptContent(html)).toThrow(
      'Page has 2 JSON-LD scripts; expected at most one.',
    );
  });
});

describe('scriptHashSource', () => {
  it('returns the CSP sha256 source for the exact UTF-8 bytes', () => {
    const content = '{"@context":"https://schema.org"}';
    expect(scriptHashSource(content)).toBe(sha256Source(content));
  });

  it('changes when the content changes by one byte', () => {
    expect(scriptHashSource('a')).not.toBe(scriptHashSource('b'));
  });
});

describe('withScriptSource', () => {
  const base
    = 'default-src \'self\'; script-src \'self\'; style-src \'self\'';

  it('adds one response-specific source to script-src', () => {
    expect(withScriptSource(base, '\'sha256-abc\'')).toBe(
      'default-src \'self\'; script-src \'self\' \'sha256-abc\'; '
      + 'style-src \'self\'',
    );
  });

  it('throws when the base policy has no plain script-src \'self\'', () => {
    expect(() => withScriptSource(
      'default-src \'none\'; script-src \'none\'',
      '\'sha256-abc\'',
    )).toThrow('Base policy is missing "script-src \'self\';".');
  });
});
