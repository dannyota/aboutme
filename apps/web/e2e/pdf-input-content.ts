import { expect } from '@playwright/test';
import type { DateRange, PersonalDetail, Resume, Section, YearMonth }
  from '@aboutme/schema';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import type { measurePdfLayout } from './display-assertions';
import type { PdfTextPage, PrintCase } from './pdf-display';
import type { PdfBox } from './pdf-paint';

type Region = 'header' | 'main' | 'sidebar';
export interface SamplePdfChunk {
  readonly text: string;
  readonly value: string;
  readonly role: string;
  readonly region: Region;
  readonly linked: boolean;
  readonly strong: boolean;
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
  'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const LABELS = {
  en: { email: 'Email', phone: 'Phone', location: 'Location',
    website: 'Website', linkedin: 'LinkedIn', github: 'GitHub',
    twitter: 'X', custom: 'Detail' },
  vi: { email: 'Email', phone: 'Điện thoại', location: 'Địa chỉ',
    website: 'Website', linkedin: 'LinkedIn', github: 'GitHub',
    twitter: 'X', custom: 'Thông tin' },
};
const compact = (value: string) => value.normalize('NFC').replace(/\s+/gu, '');

// Expectations use sample fields and the template contract, never render
// output.
function sampleContent(
  sample: Resume, lng: 'en' | 'vi',
) {
  const chunks: SamplePdfChunk[] = [];
  const paragraphs: { text: string }[] = [];
  let region: Region = 'header';
  const add = (text: string | undefined, role: string,
    linked = false, strong = false) => {
    if (!text?.trim()) {
      return;
    }
    text = text.normalize('NFC').trim();
    let value = text;
    if (role === 'heading') {
      if (sample.customization.heading.style === 'uppercase') {
        value = text.toLocaleUpperCase(lng);
      } else if (lng === 'en'
        && sample.customization.heading.style === 'titlecase') {
        value = text.replace(/(^|\s)\p{L}/gu, (letter) => letter.toUpperCase());
      }
    }
    chunks.push({ text, value, role, region, linked, strong });
  };
  const date = (value: YearMonth): string => {
    const format = sample.customization.dateFormat;
    if (value.m === undefined || format === 'YYYY') {
      return String(value.y);
    }
    return format === 'MM/YYYY'
      ? `${String(value.m).padStart(2, '0')}/${value.y}`
      : `${lng === 'vi' ? `thg ${value.m}` : MONTHS[value.m - 1]} ${value.y}`;
  };
  const range = (value: DateRange): string => {
    const start = date(value.start);
    const end = value.present
      ? (lng === 'vi' ? 'Hiện tại' : 'Present')
      : value.end ? date(value.end) : '';
    return end === '' || start === end ? start : `${start} – ${end}`;
  };
  const html = (value: string | undefined) => {
    const stack: string[] = [];
    let paragraph = '';
    for (const token of value?.match(/<[^>]*>|[^<]+/gu) ?? []) {
      if (token.startsWith('<')) {
        const tag = /^<(\/?)([a-z]+)(?:\s[^>]*)?>$/iu.exec(token);
        if (!tag || !['p', 'ul', 'ol', 'li', 'strong', 'b', 'a', 'em', 'i',
          'u', 's', 'br'].includes(tag[2]!.toLowerCase())) {
          throw new Error('Unsupported sample rich text.');
        }
        const name = tag[2]!.toLowerCase();
        if (name === 'br') {
          continue;
        }
        if (tag[1] === '/') {
          if (stack.pop() !== name) {
            throw new Error('Invalid sample rich text.');
          }
          if (name === 'p' && paragraph.trim()) {
            paragraphs.push({ text: paragraph.normalize('NFC').trim() });
          }
        } else {
          stack.push(name);
          if (name === 'p') {
            paragraph = '';
          }
        }
      } else {
        const text = token.replace(/&(#x[\da-f]+|#\d+|amp|lt|gt|quot|apos);/giu,
          (_, entity: string) => {
            if (entity.startsWith('#')) {
              return String.fromCodePoint(
                entity[1]?.toLowerCase() === 'x'
                  ? parseInt(entity.slice(2), 16)
                  : Number(entity.slice(1)));
            }
            const named: Record<string, string> = {
              amp: '&', lt: '<', gt: '>', quot: '"', apos: '\'',
            };
            return named[entity.toLowerCase()]!;
          });
        add(text, 'body', stack.includes('a'),
          stack.includes('b') || stack.includes('strong'));
        if (stack.includes('p')) {
          paragraph += text;
        }
      }
    }
    if (stack.length > 0) {
      throw new Error('Invalid sample rich text.');
    }
  };
  add(sample.personalDetails.fullName, 'name');
  add(sample.personalDetails.headline, 'headline');
  for (const detail of sample.personalDetails.details ?? []) {
    if (detail.isHidden) {
      continue;
    }
    const label = detail.label || LABELS[lng][detail.type];
    const web = detail.type !== 'email' && detail.type !== 'phone'
      && detail.type !== 'location' && detail.value.startsWith('https://');
    const display = web ? detail.display ?? 'short' : 'short';
    const iconless = sample.customization.header?.iconStyle === 'none';
    if (display !== 'label'
      && (iconless || detail.type === 'custom' || detail.label)) {
      add(`${label}:`, 'body');
    }
    const shown = display === 'label'
      ? label
      : web && display === 'short'
        ? detail.value.slice(8).replace(/\/$/u, '') || detail.value
        : detail.value;
    add(shown, 'body', web || contactLinked(detail));
  }
  for (const column of ['main', 'sidebar'] as const) {
    region = column === 'sidebar' && sample.customization.layout.columns === 1
      ? 'main'
      : column;
    for (const key of sample.customization.layout.sections[column]) {
      const section = sample.content[key];
      if (!section?.entries.some((entry) => !entry.isHidden)) {
        continue;
      }
      add(section.displayName, 'heading');
      for (const entry of section.entries) {
        if (entry.isHidden) {
          continue;
        }
        const fields = entry as Section['entries'][number] & {
          jobTitle?: string;
          degree?: string;
          name?: string;
          title?: string;
          employer?: string;
          school?: string;
          subtitle?: string;
          issuer?: string;
          employerLink?: string;
          schoolLink?: string;
          titleLink?: string;
          link?: string;
          dates?: DateRange;
          date?: YearMonth;
          city?: string;
          country?: string;
          text?: string;
          description?: string;
          infoHtml?: string;
        };
        const title = fields.jobTitle ?? fields.degree ?? fields.name
          ?? fields.title;
        const titleLink = fields.titleLink ?? fields.link;
        add(title, 'title', Boolean(titleLink), !titleLink);
        const subtitle = fields.employer ?? fields.school ?? fields.subtitle
          ?? fields.issuer;
        add(subtitle, 'subtitle',
          Boolean(fields.employerLink || fields.schoolLink));
        const meta = [
          fields.dates ? range(fields.dates) : undefined,
          fields.date ? date(fields.date) : undefined,
          fields.city, fields.country,
        ].filter((text): text is string => Boolean(text));
        meta.forEach((text, index) => {
          if (index > 0) {
            add('·', 'meta');
          }
          add(text, 'meta');
        });
        html(fields.text ?? fields.description ?? fields.infoHtml);
      }
    }
  }
  return { chunks, paragraphs };
}

export function samplePdfChunks(
  sample: Resume, lng: 'en' | 'vi',
): SamplePdfChunk[] {
  return sampleContent(sample, lng).chunks;
}

export function samplePdfParagraphs(sample: Resume, lng: 'en' | 'vi') {
  return sampleContent(sample, lng).paragraphs;
}

function contactLinked(detail: PersonalDetail): boolean {
  if (detail.type === 'phone') {
    return /^\+?\d{3,20}$/u.test(detail.value.replace(/[ .()-]/gu, ''));
  }
  return detail.type === 'email' && detail.value.length <= 254
    && /^[^\s@<>"'`()\\,;:?#%]+@[^\s@.]+(?:\.[^\s@.]+)+$/u.test(detail.value);
}

type ContentLayout = {
  sidebar: PdfBox | null;
  header: PdfBox | null;
};

function pdfInputRegions(
  pages: readonly PdfTextPage[], layout: ContentLayout,
): Record<Region, string> | undefined {
  if (!layout.header || pages.some((page) =>
    page.items.some((item) => !item.bounds))) {
    return undefined;
  }
  const regions = { header: '', main: '', sidebar: '' };
  for (const [index, page] of pages.entries()) {
    const ordered = [...page.items].sort((a, b) => b.y - a.y || a.x - b.x);
    for (const item of ordered) {
      const box = item.bounds!;
      const region = index === 0 && box.top < layout.header.bottom - 1
        ? 'header'
        : layout.sidebar && box.left >= layout.sidebar.left - 1
          ? 'sidebar'
          : 'main';
      regions[region] += compact(item.text);
    }
  }
  return regions;
}

const occurrences = (text: string, value: string) =>
  text.split(compact(value)).length - 1;

function expectedRegions(chunks: readonly SamplePdfChunk[]) {
  return {
    header: chunks.filter((chunk) => chunk.region === 'header')
      .map((chunk) => compact(chunk.value)).join(''),
    main: chunks.filter((chunk) => chunk.region === 'main')
      .map((chunk) => compact(chunk.value)).join(''),
    sidebar: chunks.filter((chunk) => chunk.region === 'sidebar')
      .map((chunk) => compact(chunk.value)).join(''),
  };
}

export function pdfInputOccurrences(
  pages: readonly PdfTextPage[], chunks: readonly SamplePdfChunk[],
  layout: ContentLayout,
): number[] {
  const regions = pdfInputRegions(pages, layout);
  return chunks.map((chunk) => regions
    ? occurrences(regions[chunk.region], chunk.value)
    : -1);
}

export function expectedInputOccurrences(
  chunks: readonly SamplePdfChunk[],
): number[] {
  const regions = expectedRegions(chunks);
  return chunks.map((chunk) => occurrences(regions[chunk.region], chunk.value));
}

export function assertPdfInputOccurrences(
  pages: readonly PdfTextPage[], chunks: readonly SamplePdfChunk[],
  layout: ContentLayout,
): void {
  expect(pdfInputOccurrences(pages, chunks, layout),
    'PDF input text occurrences.').toEqual(expectedInputOccurrences(chunks));
}

export function samplePdfContentMatches(
  input: PdfTextPage | readonly PdfTextPage[],
  chunks: readonly SamplePdfChunk[], layout: ContentLayout,
): boolean {
  const pages = 'items' in input ? [input] : input;
  const actual = pdfInputRegions(pages, layout);
  if (!actual) {
    return false;
  }
  const expected = expectedRegions(chunks);
  return (['header', 'main', 'sidebar'] as const).every((region) => {
    const values = chunks.filter((chunk) => chunk.region === region)
      .map((chunk) => compact(chunk.value));
    if (!values.every((value) => occurrences(actual[region], value)
      === occurrences(expected[region], value))) {
      return false;
    }
    let cursor = 0;
    return values.every((value) => {
      const index = actual[region].indexOf(value, cursor);
      if (index === -1) {
        return false;
      }
      cursor = index + value.length;
      return true;
    });
  });
}

export function assertDomInputContent(
  layout: Pick<Awaited<ReturnType<typeof measurePdfLayout>>, 'chunks'>,
  expected: readonly SamplePdfChunk[],
): void {
  const content = ({ text, role, region, linked, strong }:
    typeof layout.chunks[number] | SamplePdfChunk) =>
    ({ text, role, region, linked, strong });
  expect(layout.chunks.map(content),
    'Print DOM text and occurrences from input.')
    .toEqual(expected.map(content));
}

// Overflow inputs are the base JSON fields plus the harness's literal probes.
// Template expectations stay independent of applyTemplate and runtime tokens.
export async function overflowPdfChunks(
  printCase: PrintCase, lng: 'en' | 'vi',
): Promise<SamplePdfChunk[]> {
  const source = lng === 'vi' ? 'vn-full' : 'full';
  const sample: Resume = JSON.parse(await readFile(resolve(
    import.meta.dirname, '../../../packages/schema/fixtures', `${source}.json`,
  ), 'utf8'));
  const skill = sample.content.skill!;
  if (skill.sectionType !== 'skill') {
    throw new Error('Expected skill input fixture.');
  }
  const skillEntry = skill.entries[0]!;
  const main = printCase.fixture === 'print-main-overflow';
  skill.entries = Array.from({ length: main ? 1 : 13 }, (_, index) => {
    const number = index + 1;
    const title = [
      number === 1 ? 'SIDEBAR-START' : '',
      `SIDEBAR-ENTRY-${String(number).padStart(2, '0')}`,
      number === 13 ? 'SIDEBAR-END' : '',
    ].filter(Boolean).join(' ');
    const body = lng === 'vi'
      ? 'Nội dung kiểm tra phân trang'
      : 'Pagination test content';
    return {
      ...skillEntry,
      isHidden: false,
      name: main ? 'SIDEBAR-SHORT' : title,
      infoHtml: main
        ? '<p>SIDEBAR-SHORT</p>'
        : `<p>${body} ${number}.</p>`,
    };
  });
  const section = main ? sample.content.work! : sample.content.profile!;
  if (section.sectionType === 'work') {
    const entry = section.entries[0]!;
    section.entries = Array.from({ length: 9 }, (_, index) => {
      const number = index + 1;
      const title = [
        number === 1 ? 'MAIN-START' : '',
        `MAIN-ENTRY-${String(number).padStart(2, '0')}`,
        number === 9 ? 'MAIN-END' : '',
      ].filter(Boolean).join(' ');
      const body = lng === 'vi'
        ? 'Nội dung kiểm tra phân trang bản in'
        : 'Print pagination test content';
      return {
        ...entry,
        isHidden: false,
        jobTitle: title,
        description: `<p>${body} ${number}.</p>`,
      };
    });
  } else if (section.sectionType === 'profile') {
    section.entries = [{ ...section.entries[0]!, isHidden: false,
      text: '<p>MAIN-SHORT</p>' }];
  } else {
    throw new Error('Expected work or profile input fixture.');
  }
  const key = main ? 'work' : 'profile';
  sample.content = { [key]: section, skill };
  sample.customization.dateFormat = lng === 'vi' ? 'MM/YYYY' : 'Mon YYYY';
  sample.customization.heading.style = 'uppercase';
  sample.customization.header = { align: 'left', detailsLayout: 'inline',
    iconStyle: 'outline' };
  sample.customization.layout = { columns: 2,
    sections: { main: [key], sidebar: ['skill'] } };
  return samplePdfChunks(sample, lng);
}
