import { readFileSync } from 'node:fs';
import { join } from 'node:path';

export const RETIRED_STYLE = new RegExp([
  'surface-(?:blue|indigo|sand)',
  'radius-feature',
  'shadow-product',
  'brand-(?:jade|ochre|indigo|blue|deep-blue)',
  '--primary\\b',
  '\\bbg-primary\\b',
  '#[\\da-f]{3,8}\\b',
].join('|'), 'iu');

export interface StyleRule {
  readonly at: readonly string[];
  readonly selectors: readonly string[];
  readonly declarations: Readonly<Record<string, string>>;
}

export function webSource(path: string): string {
  return readFileSync(join(process.cwd(), path), 'utf8');
}

export function sfcRules(path: string): StyleRule[] {
  const css = [...webSource(path).matchAll(/<style[^>]*>([\s\S]*?)<\/style>/gu)]
    .map((match) => match[1]!)
    .join('\n')
    .replace(/\/\*[\s\S]*?\*\//gu, '');
  const rules: StyleRule[] = [];
  const stack: string[] = [];
  let start = 0;
  for (let index = 0; index < css.length; index += 1) {
    const char = css[index];
    if (char === '{') {
      stack.push(squash(css.slice(start, index)));
      start = index + 1;
    } else if (char === '}') {
      const prelude = stack.pop() ?? '';
      if (!prelude.startsWith('@')) {
        rules.push({
          at: stack.filter((outer) => outer.startsWith('@')),
          selectors: split(prelude),
          declarations: parse(css.slice(start, index)),
        });
      }
      start = index + 1;
    }
  }
  return rules;
}

export function declared(
  rules: readonly StyleRule[], selector: string, at: readonly string[] = [],
): Record<string, string> {
  const wanted = squash(selector);
  return Object.assign({}, ...rules
    .filter((rule) => rule.selectors.includes(wanted)
      && rule.at.join(' | ') === at.map(squash).join(' | '))
    .map((rule) => rule.declarations));
}

function squash(text: string): string {
  return text.replace(/\s+/gu, ' ').replace(/\( /gu, '(').replace(/ \)/gu, ')')
    .trim();
}

function split(prelude: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let start = 0;
  for (let index = 0; index < prelude.length; index += 1) {
    const char = prelude[index];
    if (char === '(') depth += 1;
    else if (char === ')') depth -= 1;
    else if (char === ',' && depth === 0) {
      parts.push(squash(prelude.slice(start, index)));
      start = index + 1;
    }
  }
  parts.push(squash(prelude.slice(start)));
  return parts;
}

function parse(body: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of body.split(';')) {
    const colon = line.indexOf(':');
    if (colon < 0) continue;
    out[squash(line.slice(0, colon))] = squash(line.slice(colon + 1));
  }
  return out;
}
