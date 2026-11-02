// @vitest-environment node
import type { Resume } from '@aboutme/schema';
import { describe, expect, it } from 'vitest';

import { samplePdfChunks, samplePdfContentMatches }
  from '../e2e/sample-pdf-content';
import type { PdfTextPage } from '../e2e/pdf-display';

const sample: Resume = {
  schemaVersion: 5,
  personalDetails: { fullName: 'Lê An', headline: 'Kỹ sư', details: [
    { id: 'location', type: 'location', value: 'Huế', isHidden: false },
    { id: 'hidden-contact', type: 'phone', value: '123', isHidden: true },
  ] },
  content: {
    work: { sectionType: 'work', displayName: 'Công việc', entries: [
      { id: 'first', jobTitle: 'Kỹ sư trưởng', employer: 'Công ty A',
        dates: { start: { y: 2024, m: 1 }, end: null, present: true },
        description: '<p>Thiết kế &amp; phát triển.</p>' },
      { id: 'second', jobTitle: 'Kỹ sư phần mềm', employer: 'Công ty A' },
      { id: 'hidden-entry', jobTitle: 'Ẩn', isHidden: true },
    ] },
    skill: { sectionType: 'skill', displayName: 'Kỹ năng', entries: [
      { id: 'skill', name: 'TypeScript' },
    ] },
    unused: { sectionType: 'profile', displayName: 'Không in', entries: [
      { id: 'unused', text: '<p>Không xuất hiện.</p>' },
    ] },
  },
  customization: {
    font: { family: 'inter', baseSizePx: 14 },
    colors: { primary: '#111111', text: '#222222', background: '#ffffff' },
    spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.5 },
    heading: { style: 'uppercase', showRule: false },
    header: { align: 'left', detailsLayout: 'inline', iconStyle: 'outline' },
    layout: { columns: 2, sections: { main: ['work'], sidebar: ['skill'] } },
    sectionDisplay: { skill: { style: 'text' }, language: { style: 'text' } },
    pageFormat: 'a4', dateFormat: 'MM/YYYY',
  },
};
const layout = {
  header: { left: 10, right: 290, top: 10, bottom: 60 },
  sidebar: { left: 210, right: 290, top: 70, bottom: 390 },
};
const lines = [
  ['Lê An', 10, 20], ['Kỹ sư', 10, 35], ['Huế', 10, 50],
  ['CÔNG VIỆC', 10, 80], ['Kỹ sư trưởng', 10, 100],
  ['Công ty A', 10, 120], ['01/2024 – Hiện tại', 10, 140],
  ['Thiết kế & phát triển.', 10, 160], ['Kỹ sư phần mềm', 10, 180],
  ['Công ty A', 10, 200], ['KỸ NĂNG', 210, 80],
  ['TypeScript', 210, 100],
] as const;
function page(values: readonly (readonly [string, number, number])[]
  = lines): PdfTextPage {
  return { width: 300, height: 400, text: '', items: values.map(
    ([text, x, top]) => ({ text, x, y: 400 - top - 10,
      width: 70, height: 10,
      bounds: { left: x, right: x + 70, top, bottom: top + 10 } }),
  ) };
}

describe('sample PDF input expectations', () => {
  it('derives visible text, repeated occurrences and column order from input',
    () => {
      const chunks = samplePdfChunks(sample, 'vi');
      expect(chunks.map(({ region, value }) => [region, value])).toEqual([
        ['header', 'Lê An'], ['header', 'Kỹ sư'], ['header', 'Huế'],
        ['main', 'CÔNG VIỆC'], ['main', 'Kỹ sư trưởng'],
        ['main', 'Công ty A'], ['main', '01/2024 – Hiện tại'],
        ['main', 'Thiết kế & phát triển.'], ['main', 'Kỹ sư phần mềm'],
        ['main', 'Công ty A'], ['sidebar', 'KỸ NĂNG'],
        ['sidebar', 'TypeScript'],
      ]);
      expect(samplePdfContentMatches(page(), chunks, layout)).toBe(true);
      const duplicate = [...lines, lines[5]!];
      expect(samplePdfContentMatches(page(duplicate), chunks, layout))
        .toBe(false);
      expect(samplePdfContentMatches(page(lines.filter((_, i) => i !== 5)),
        chunks, layout)).toBe(false);
    });

  it.each(['accents', 'replacement', 'order', 'column'] as const)(
    'rejects a sample %s mutation without changing the expected input',
    (mutation) => {
      const altered: [string, number, number][] = lines.map(
        (line) => [...line],
      );
      if (mutation === 'accents') {
        altered[7]![0] = 'Thiet ke & phat trien.';
      } else if (mutation === 'replacement') altered[7]![0] = 'Nội dung khác.';
      else if (mutation === 'order') {
        // Move whole entries; counts and extraction stream order stay intact.
        for (const index of [4, 5, 6, 7]) altered[index]![2] += 120;
        for (const index of [8, 9]) altered[index]![2] -= 80;
      } else altered[8]![1] = 210;
      expect(samplePdfContentMatches(page(altered),
        samplePdfChunks(sample, 'vi'), layout)).toBe(false);
    },
  );

  it('preserves NFC accents across split PDF runs and ignores extraction order',
    () => {
      const values: [string, number, number][] = lines.map((line) => [...line]);
      values[0]![0] = 'Le\u0302 An';
      values[7]![0] = 'Thiết kế &';
      values.push(['phát triển.', 90, 160]);
      expect(samplePdfContentMatches(page(values.reverse()),
        samplePdfChunks(sample, 'vi'), layout)).toBe(true);
    });

  it('derives rich text styles and localized dates without renderer helpers',
    () => {
      const styled = structuredClone(sample);
      const work = styled.content.work!;
      if (work.sectionType !== 'work') throw new Error('Expected work input.');
      work.entries[0]!.description = '<p>A&#39; <strong>bold</strong> '
        + '<a href="https://example.com">link</a></p>';
      styled.customization.dateFormat = 'Mon YYYY';
      styled.customization.heading.style = 'titlecase';
      const chunks = samplePdfChunks(styled, 'en');
      expect(chunks.find(({ role }) => role === 'heading')!.value)
        .toBe('Công Việc');
      expect(chunks.find(({ role }) => role === 'meta')!.value)
        .toBe('Jan 2024 – Present');
      expect(chunks.find(({ text }) => text === 'A\''))
        .toMatchObject({ strong: false, linked: false });
      expect(chunks.find(({ text }) => text === 'bold'))
        .toMatchObject({ strong: true, linked: false });
      expect(chunks.find(({ text }) => text === 'link'))
        .toMatchObject({ strong: false, linked: true });
    });

  it('derives contact labels and link display independently from input', () => {
    const contacts = structuredClone(sample);
    contacts.customization.header!.iconStyle = 'none';
    contacts.personalDetails.details = [
      { id: 'email', type: 'email', value: 'fixture@example.com',
        isHidden: false },
      { id: 'phone', type: 'phone', value: '+84 900 000 001', isHidden: false },
      { id: 'web', type: 'website', value: 'https://example.com/',
        label: 'Hồ sơ', display: 'label', isHidden: false },
      { id: 'custom', type: 'custom', label: 'Zalo', value: 'cùng số',
        isHidden: false },
      { id: 'location', type: 'location', value: 'Đà Nẵng', isHidden: false },
    ];
    expect(samplePdfChunks(contacts, 'vi')
      .filter(({ region }) => region === 'header')
      .map(({ value, linked }) => [value, linked])).toEqual([
      ['Lê An', false], ['Kỹ sư', false], ['Email:', false],
      ['fixture@example.com', true], ['Điện thoại:', false],
      ['+84 900 000 001', true], ['Hồ sơ', true], ['Zalo:', false],
      ['cùng số', false], ['Địa chỉ:', false], ['Đà Nẵng', false],
    ]);
  });
});
