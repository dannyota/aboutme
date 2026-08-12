// @vitest-environment jsdom

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import { sanitizeRichText } from '../../app/utils/sanitizeRichText';

interface CanonicalNode {
  type: 'element' | 'text';
  name?: string;
  attributes?: Array<[string, string]>;
  value?: string;
  children?: CanonicalNode[];
}

const golden = JSON.parse(
  readFileSync(
    resolve(
      process.cwd(),
      '../server/internal/sanitize/testdata/corpus-output.golden.json',
    ),
    'utf8',
  ),
) as Record<string, string>;

const canonicalFragment = (html: string): CanonicalNode[] => {
  const document = new DOMParser().parseFromString('', 'text/html');
  const template = document.createElement('template');
  template.innerHTML = html;

  const canonicalize = (node: Node): CanonicalNode | null => {
    if (node.nodeType === Node.COMMENT_NODE) return null;
    if (node.nodeType === Node.TEXT_NODE) {
      const value = node.textContent ?? '';
      return value.trim() === '' ? null : { type: 'text', value };
    }
    if (!(node instanceof Element)) return null;

    const attributes = [...node.attributes]
      .map(({ name, value }): [string, string] => [
        name,
        name === 'rel'
          ? value.split(/\s+/).filter(Boolean).sort().join(' ')
          : value,
      ])
      .sort(([left], [right]) => left.localeCompare(right));
    const children = [...node.childNodes]
      .map(canonicalize)
      .filter((child): child is CanonicalNode => child !== null);
    return {
      type: 'element',
      name: node.tagName.toLowerCase(),
      attributes,
      children,
    };
  };

  return [...template.content.childNodes]
    .map(canonicalize)
    .filter((node): node is CanonicalNode => node !== null);
};

describe('DOMPurify agrees with Go sanitizer output', () => {
  it.each(Object.entries(golden))(
    'keeps %s as a DOM fixed point',
    (_, html) => {
      expect(canonicalFragment(sanitizeRichText(html))).toEqual(
        canonicalFragment(html),
      );
    },
  );
});
