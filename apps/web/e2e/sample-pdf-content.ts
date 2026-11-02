import type { DateRange, PersonalDetail, Resume, Section, YearMonth }
  from '@aboutme/schema';

import type { PdfTextPage } from './pdf-display';
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
export function samplePdfChunks(
  sample: Resume, lng: 'en' | 'vi',
): SamplePdfChunk[] {
  const chunks: SamplePdfChunk[] = [];
  let region: Region = 'header';
  const add = (text: string | undefined, role: string,
    linked = false, strong = false) => {
    if (!text?.trim()) return;
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
    if (value.m === undefined || format === 'YYYY') return String(value.y);
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
    for (const token of value?.match(/<[^>]*>|[^<]+/gu) ?? []) {
      if (token.startsWith('<')) {
        const tag = /^<(\/?)([a-z]+)(?:\s[^>]*)?>$/iu.exec(token);
        if (!tag || !['p', 'ul', 'ol', 'li', 'strong', 'b', 'a', 'em', 'i',
          'u', 's', 'br'].includes(tag[2]!.toLowerCase())) {
          throw new Error('Unsupported sample rich text.');
        }
        const name = tag[2]!.toLowerCase();
        if (name === 'br') continue;
        if (tag[1] === '/') {
          if (stack.pop() !== name) {
            throw new Error('Invalid sample rich text.');
          }
        } else stack.push(name);
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
      }
    }
    if (stack.length > 0) throw new Error('Invalid sample rich text.');
  };
  add(sample.personalDetails.fullName, 'name');
  add(sample.personalDetails.headline, 'headline');
  for (const detail of sample.personalDetails.details ?? []) {
    if (detail.isHidden) continue;
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
      if (!section?.entries.some((entry) => !entry.isHidden)) continue;
      add(section.displayName, 'heading');
      for (const entry of section.entries) {
        if (entry.isHidden) continue;
        const fields = entry as Section['entries'][number] & {
          jobTitle?: string; degree?: string; name?: string; title?: string;
          employer?: string; school?: string; subtitle?: string;
          issuer?: string;
          employerLink?: string; schoolLink?: string; titleLink?: string;
          link?: string; dates?: DateRange; date?: YearMonth;
          city?: string; country?: string; text?: string;
          description?: string; infoHtml?: string;
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
          if (index > 0) add('·', 'meta');
          add(text, 'meta');
        });
        html(fields.text ?? fields.description ?? fields.infoHtml);
      }
    }
  }
  return chunks;
}

function contactLinked(detail: PersonalDetail): boolean {
  if (detail.type === 'phone') {
    return /^\+?\d{3,20}$/u.test(detail.value.replace(/[ .()-]/gu, ''));
  }
  return detail.type === 'email' && detail.value.length <= 254
    && /^[^\s@<>"'`()\\,;:?#%]+@[^\s@.]+(?:\.[^\s@.]+)+$/u.test(detail.value);
}

export function samplePdfContentMatches(
  page: PdfTextPage,
  chunks: readonly SamplePdfChunk[],
  layout: { sidebar: PdfBox | null; header: PdfBox | null },
): boolean {
  if (!layout.header || page.items.some((item) => !item.bounds)) return false;
  const regions = { header: '', main: '', sidebar: '' };
  const ordered = [...page.items].sort((a, b) => b.y - a.y || a.x - b.x);
  for (const item of ordered) {
    const box = item.bounds!;
    const region = box.top < layout.header.bottom - 1
      ? 'header'
      : layout.sidebar && box.left >= layout.sidebar.left - 1
        ? 'sidebar'
        : 'main';
    regions[region] += compact(item.text);
  }
  return (['header', 'main', 'sidebar'] as const).every((region) => {
    const values = chunks.filter((chunk) => chunk.region === region)
      .map((chunk) => compact(chunk.value));
    const expected = values.join('');
    const actual = regions[region];
    const count = (text: string, value: string) => text.split(value).length - 1;
    const same = (value: string) =>
      count(actual, value) === count(expected, value);
    if (!values.every(same)) {
      return false;
    }
    let cursor = 0;
    return values.every((value) => {
      const index = actual.indexOf(value, cursor);
      if (index === -1) return false;
      cursor = index + value.length;
      return true;
    });
  });
}
