import type { Page } from '@playwright/test';

export const ENTRY_POINTS_MARKER = 'data-entry-points-proof';

export interface TitleLink {
  readonly entry: string;
  readonly href: string;
}

export interface BarLink {
  readonly label: string;
  readonly href: string;
}

export interface EntryPointFacts {
  readonly titles: readonly TitleLink[];
  readonly bars: readonly BarLink[];
  readonly kept: boolean;
}

export function withoutField(
  entry: Readonly<Record<string, unknown>>,
  field: string,
): Record<string, unknown> {
  return Object.fromEntries(Object.entries(entry).filter(([key]) => key !== field));
}

export function sortedTitles(titles: readonly TitleLink[]): TitleLink[] {
  return [...titles].sort((left, right) => left.entry.localeCompare(right.entry));
}

export async function markPublicRoot(page: Page): Promise<void> {
  await page.evaluate((marker) => {
    document.querySelector('main#public-resume')?.setAttribute(marker, 'kept');
  }, ENTRY_POINTS_MARKER);
}

export async function entryPointFacts(page: Page): Promise<EntryPointFacts> {
  const facts = await page.evaluate((marker) => {
    const root = document.querySelector('main#public-resume');
    return {
      titles: Array.from(root?.querySelectorAll('a[data-project-entry]') ?? [], (anchor) => ({
        entry: anchor.getAttribute('data-project-entry') ?? '',
        href: anchor.getAttribute('href') ?? '',
      })),
      bars: Array.from(root?.querySelectorAll('a.public-projects') ?? [], (anchor) => ({
        label: anchor.textContent?.trim() ?? '',
        href: anchor.getAttribute('href') ?? '',
      })),
      kept: root?.getAttribute(marker) === 'kept',
    };
  }, ENTRY_POINTS_MARKER);
  return { ...facts, titles: sortedTitles(facts.titles) };
}

export interface ServerEntryPoints {
  readonly marker: boolean;
  readonly titles: number;
  readonly bars: number;
}

export function serverEntryPoints(html: string): ServerEntryPoints {
  return {
    marker: html.includes('data-project-links=""'),
    titles: html.match(/data-project-entry="/gu)?.length ?? 0,
    bars: html.match(/class="public-projects"/gu)?.length ?? 0,
  };
}

export interface FetchedPDF {
  readonly status: number;
  readonly contentType: string | null;
  readonly bytes: readonly number[];
}

export function fetchPDF(
  page: Page,
  path: string,
  credentials: 'include' | 'omit',
): Promise<FetchedPDF> {
  return page.evaluate(
    async ({ credentials, path }) => {
      const response = await fetch(path, { cache: 'no-store', credentials });
      return {
        status: response.status,
        contentType: response.headers.get('content-type'),
        bytes: Array.from(new Uint8Array(await response.arrayBuffer())),
      };
    },
    { credentials, path },
  );
}

export function isPDF(bytes: readonly number[]): boolean {
  return String.fromCharCode(...bytes.slice(0, 5)) === '%PDF-';
}

const LITERAL_ESCAPES: Readonly<Record<string, string>> = {
  'n': '\n',
  'r': '\r',
  't': '\t',
  'b': '\b',
  'f': '\f',
  '\n': '',
  '\r': '',
};

function literalString(value: string): string {
  return value.replace(/\\([0-7]{1,3}|[^])/gu, (_, escaped: string) =>
    /^[0-7]/u.test(escaped)
      ? String.fromCharCode(Number.parseInt(escaped, 8))
      : LITERAL_ESCAPES[escaped] ?? escaped);
}

function hexString(value: string): string {
  const digits = value.replace(/\s/gu, '');
  return Buffer.from(digits.length % 2 === 0 ? digits : `${digits}0`, 'hex').toString('latin1');
}

export function pdfLinkURIs(bytes: readonly number[]): string[] {
  const text = Buffer.from(bytes).toString('latin1');
  return Array.from(
    text.matchAll(/\/URI\s*(?:\(((?:\\[^]|[^\\)])*)\)|<([0-9A-Fa-f\s]*)>)/gu),
    (match) => match[1] === undefined ? hexString(match[2] ?? '') : literalString(match[1]),
  );
}
