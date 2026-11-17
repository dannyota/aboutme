import { expect } from 'vitest';

export interface CssRule {
  readonly at: string;
  readonly selector: string;
  readonly body: Readonly<Record<string, string>>;
}

function declarations(body: string): Record<string, string> {
  return Object.fromEntries(body.split(';')
    .map((part) => part.trim())
    .filter((part) => part.includes(':'))
    .map((part) => {
      const colon = part.indexOf(':');
      return [
        part.slice(0, colon).trim(),
        part.slice(colon + 1).trim().replaceAll(/\s+/gu, ' '),
      ];
    }));
}

export function parseCss(source: string): CssRule[] {
  const rules: CssRule[] = [];
  const walk = (text: string, at: string) => {
    let start = 0;
    let open = text.indexOf('{');
    while (open !== -1) {
      let depth = 1;
      let end = open + 1;
      while (depth > 0 && end < text.length) {
        if (text[end] === '{') depth += 1;
        if (text[end] === '}') depth -= 1;
        end += 1;
      }
      const head = text.slice(start, open);
      const prelude = head.slice(head.lastIndexOf(';') + 1).trim()
        .replaceAll(/\s+/gu, ' ');
      const inner = text.slice(open + 1, end - 1);
      if (prelude.startsWith('@media')) {
        walk(inner, prelude);
      } else {
        const body = declarations(inner);
        for (const selector of prelude.split(',')) {
          rules.push({ at, selector: selector.trim(), body });
        }
      }
      start = end;
      open = text.indexOf('{', start);
    }
  };
  walk(source, '');
  return rules;
}

export function matching(
  rules: readonly CssRule[], selector: string, at: string,
): CssRule[] {
  return rules.filter((rule) => rule.selector === selector && rule.at === at);
}

export function style(
  rules: readonly CssRule[], selector: string, at = '',
): Record<string, string> {
  const found = matching(rules, selector, at);
  expect(found.length, `${at} ${selector}`).toBeGreaterThan(0);
  return Object.assign({}, ...found.map((rule) => rule.body));
}

export function lastIndex(
  rules: readonly CssRule[], selector: string, at = '',
): number {
  return rules.findLastIndex((rule) =>
    rule.selector === selector && rule.at === at);
}
