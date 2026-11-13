import type { ProjectEntry } from '@aboutme/schema';

import {
  characterCount,
  isLanguageKey,
  TEXT_LIMITS,
  type CreateProjectRequest,
} from './projectDocument';

const CODE_HOSTS: ReadonlySet<string> = new Set([
  'github.com', 'gitlab.com', 'bitbucket.org', 'codeberg.org',
]);

const BLOCK_TAGS: ReadonlySet<string> = new Set(['P', 'UL', 'OL', 'LI', 'BR']);

export function canSaveEntry(entry: ProjectEntry): boolean {
  const name = entry.title?.trim() ?? '';
  return name !== ''
    && characterCount(name) <= TEXT_LIMITS.name
    && characterCount(entry.subtitle?.trim() ?? '') <= TEXT_LIMITS.role;
}

export function projectLanguage(
  lng: string | null | undefined,
  siteLocale: string,
): string {
  const primary = (lng ?? '').toLowerCase().split('-')[0] ?? '';
  return primary !== 'und' && isLanguageKey(primary) ? primary : siteLocale;
}

export function projectFromEntry(
  entry: ProjectEntry,
  language: string,
): CreateProjectRequest {
  const role = entry.subtitle?.trim() ?? '';
  const summary = cut(plainText(entry.description ?? ''), TEXT_LIMITS.summary);
  const link = httpsLink(entry.link);
  return {
    defaultLanguage: language,
    ...(entry.dates === undefined ? {} : { dates: copyDates(entry.dates) }),
    ...(link === null
      ? {}
      : CODE_HOSTS.has(link.host)
        ? { repositoryUrl: link.href }
        : { demoUrl: link.href }),
    text: {
      [language]: {
        name: entry.title?.trim() ?? '',
        ...(role === '' ? {} : { role }),
        ...(summary === '' ? {} : { summary }),
      },
    },
  };
}

function httpsLink(
  value: string | undefined,
): { readonly href: string; readonly host: string } | null {
  if (value === undefined || !value.startsWith('https://')) return null;
  try {
    return { href: value, host: new URL(value).hostname };
  } catch {
    return null;
  }
}

function copyDates(
  dates: NonNullable<ProjectEntry['dates']>,
): NonNullable<CreateProjectRequest['dates']> {
  const yearMonth = (value: { y: number; m?: number }) =>
    value.m === undefined ? { y: value.y } : { y: value.y, m: value.m };
  return {
    start: yearMonth(dates.start),
    end: dates.end === null ? null : yearMonth(dates.end),
    present: dates.present,
  };
}

function plainText(html: string): string {
  if (html === '' || typeof DOMParser === 'undefined') return '';
  const parsed = new DOMParser().parseFromString(html, 'text/html');
  let out = '';
  const walk = (node: ChildNode): void => {
    if (node.nodeType === 3) {
      out += node.textContent ?? '';
      return;
    }
    const block = node.nodeType === 1
      && BLOCK_TAGS.has((node as Element).tagName);
    if (block) out += ' ';
    node.childNodes.forEach(walk);
    if (block) out += ' ';
  };
  parsed.body.childNodes.forEach(walk);
  return out.replace(/\s+/gu, ' ').trim();
}

function cut(text: string, limit: number): string {
  if (characterCount(text) <= limit) return text;
  let out = '';
  let count = 0;
  const graphemes = new Intl.Segmenter(undefined, { granularity: 'grapheme' });
  for (const { segment } of graphemes.segment(text)) {
    const length = characterCount(segment);
    if (count + length > limit) break;
    out += segment;
    count += length;
  }
  return out.trimEnd();
}
