// @vitest-environment jsdom

import { HOSTILE_CORPUS } from '@aboutme/schema/sanitizer';
import { describe, expect, it } from 'vitest';

import { sanitizeRichText } from '../../app/utils/sanitizeRichText';
import {
  expectNeutralized,
  neutralizationViolations,
} from './neutralization';

describe('sanitizeRichText client policy', () => {
  it.each(HOSTILE_CORPUS)('neutralizes $id', ({ payload }) => {
    expectNeutralized(sanitizeRichText(payload));
  });

  it('makes every result a fixed point', () => {
    for (const { payload } of HOSTILE_CORPUS) {
      const once = sanitizeRichText(payload);
      expect(sanitizeRichText(once)).toBe(once);
    }
  });

  it('preserves benign content across every allowed tag', () => {
    const source
      = '<p>One<br><strong>two</strong><em>three</em><u>four</u></p>'
        + '<ol><li>five</li></ol><ul><li>six</li></ul>'
        + '<a href="https://example.com" target="_blank">seven</a>';
    const result = sanitizeRichText(source);

    expectNeutralized(result);
    const text = new DOMParser().parseFromString(result, 'text/html').body
      .textContent;
    expect(text).toBe('Onetwothreefourfivesixseven');
  });

  it('normalizes anchor security attributes exactly', () => {
    expect(
      sanitizeRichText(
        '<a href="https://example.com" rel="opener" target="other">safe</a>',
      ),
    ).toBe(
      '<a href="https://example.com" rel="noopener noreferrer">safe</a>',
    );
  });

  it('keeps target only when the input value is exactly _blank', () => {
    expect(
      sanitizeRichText(
        '<a href="https://example.com" target="_blank ">safe</a>',
      ),
    ).toBe(
      '<a href="https://example.com" rel="noopener noreferrer">safe</a>',
    );
  });
});

describe('neutralization predicate controls', () => {
  it.each([
    '<script>alert(1)</script>',
    '<a href="javascript:alert(1)" rel="noopener noreferrer">x</a>',
    '<a href="https://example.com" onclick="alert(1)" rel="noopener noreferrer">x</a>',
    '<a href="https://example.com">x</a>',
    '<a href="https://example.com" rel="opener">x</a>',
    '<a href="https://example.com" rel="noopener noreferrer" target="other">x</a>',
  ])('rejects a hand-built violation: %s', (html) => {
    expect(neutralizationViolations(html)).not.toEqual([]);
  });

  it('accepts dangerous-looking text with no active node', () => {
    expect(neutralizationViolations('javascript:alert(1)')).toEqual([]);
  });
});
