#!/usr/bin/env node
// Builds the *.fo XSL-FO sources for the LinkedIn "Save to PDF" layout
// fixtures. See docs/design/linkedin-import.md ("Save to PDF structure",
// "Tests") for the structure, sizes, and baseline gaps this reproduces, and
// fixtures/README.md for how the *.pdf files are rendered from these sources.
//
// Every visual line is written as its own fo:block inside an absolutely
// positioned fo:block-container, one container per column per page. A line's
// container position (for the first line in a column on a page) or
// space-before (for every later line) is computed from the target baseline
// gap using the embedded font's ascent and descent, so the rendered PDF's
// baselines land at the positions this design records, without relying on
// FOP's automatic pagination or line wrapping. A "wrapped" line is therefore
// just another hand-written line at the wrapped-line gap.
//
// Usage: node generate-fo.mjs (writes each fixture's *.fo next to this file)

import { writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const OUT_DIR = dirname(fileURLToPath(import.meta.url));

// Every line uses line-height equal to its own font-size (1 em), so a
// line's baseline sits a fixed fraction of the font-size below the top of
// its line box (and the box's bottom edge sits the remaining fraction below
// the baseline), for any size. Noto Sans Regular's own ascent and descent
// (hhea and OS/2 typo values agree: 1069 and -293 of 1000 units per em)
// predict 0.888 under XSL's half-leading rule, but a calibration render of
// this file's first version (5 sizes: 9, 10.5, 13, 16, 26 pt, each a
// column's or footer's first line, so its measured y isolates this fraction
// from any other line's space-before) measured 0.755 to 0.764 instead: FOP
// 2.3 does not split the leading strictly evenly. ABOVE_FRAC is the
// measured average; the predicted value put every first line 1 to 3.5 pt
// (up to 0.13 pt per point of font-size) below its target y.
const ABOVE_FRAC = 0.7601;
const BELOW_FRAC = 1 - ABOVE_FRAC; // box bottom from baseline

const PAGE_WIDTH = 612;
const PAGE_HEIGHT = 792;
const TOP_Y = 737.6; // first line's baseline on a fresh page/column
const BOTTOM_MARGIN = 50; // minimum baseline y before a column breaks
const FOOTER_Y = 13.9;
const FOOTER_SIZE = 9;
const SIDEBAR_X = 21.6;
const MAIN_X = 223.6;

function aboveTop(size) {
  return ABOVE_FRAC * size;
}
function belowBottom(size) {
  return BELOW_FRAC * size;
}

function escapeXml(text) {
  return text
    .replace(/&/gu, '&amp;')
    .replace(/</gu, '&lt;')
    .replace(/>/gu, '&gt;');
}

// Lays out one fixture as a sequence of pages, each with a sidebar column
// and a main column. The two columns track separate "current page" cursors,
// since the sidebar (page 1 only, unless continued) and the main column
// (every page) do not share a page boundary: the main column auto-breaks to
// a new page when its next line would fall below BOTTOM_MARGIN, while the
// sidebar only moves to a new page when a caller asks it to
// (`breakSidebarToNextPage`), matching the design's "the sidebar ends on
// page 1" rule.
class Layout {
  constructor() {
    this.pages = [{ sidebar: [], main: [] }];
    this.columnPage = { sidebar: 0, main: 0 };
  }

  ensurePage(index) {
    while (this.pages.length <= index) {
      this.pages.push({ sidebar: [], main: [] });
    }
  }

  // Appends to a column, tracking each line's absolute y so overflow can be
  // detected; breaks the main column to a new page when needed.
  push(column, text, size, gap) {
    let pageIndex = this.columnPage[column];
    this.ensurePage(pageIndex);
    let lines = this.pages[pageIndex][column];
    let y = lines.length === 0 ? TOP_Y : lines[lines.length - 1].y - gap;
    if (column === 'main' && lines.length > 0 && y < BOTTOM_MARGIN) {
      pageIndex += 1;
      this.ensurePage(pageIndex);
      this.columnPage.main = pageIndex;
      lines = this.pages[pageIndex][column];
      y = TOP_Y;
      gap = undefined;
    }
    lines.push({ text, size, gap, y });
  }

  main(text, size, gap) {
    this.push('main', text, size, gap);
  }

  sidebar(text, size, gap) {
    this.push('sidebar', text, size, gap);
  }

  // Forces the sidebar to continue on the next page (wraps-en only), without
  // moving the main column's cursor.
  breakSidebarToNextPage() {
    this.columnPage.sidebar += 1;
    this.ensurePage(this.columnPage.sidebar);
  }

  // Forces the main column to continue on a fresh page (used before
  // Education, as in the real export this mirrors), without moving the
  // sidebar's cursor.
  newPage() {
    this.columnPage.main += 1;
    this.ensurePage(this.columnPage.main);
  }

  get pageCount() {
    return this.pages.length;
  }

  // The line object most recently pushed to a column, for a caller that
  // needs to adjust a flag on it after the fact (limits-en's one line that
  // is allowed to wrap).
  lastLine(column) {
    const page = this.pages[this.columnPage[column]];
    return page[column][page[column].length - 1];
  }
}

// Renders one column's lines to an absolutely positioned fo:block-container.
function renderColumn(x, lines) {
  if (lines.length === 0) return '';
  const width = (PAGE_WIDTH - x - 24).toFixed(1);
  const first = lines[0];
  const top = (PAGE_HEIGHT - first.y - aboveTop(first.size)).toFixed(3);
  let blocks = '';
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    const spaceBefore = i === 0
      ? 0
      : line.gap - belowBottom(lines[i - 1].size) - aboveTop(line.size);
    const wrapOption = line.noWrap === false ? '' : ' wrap-option="no-wrap"';
    blocks += `      <fo:block font-family="NotoSans" font-size="${
      line.size}pt" line-height="${line.size}pt"${wrapOption} `
      + `space-before="${spaceBefore.toFixed(3)}pt">${
        escapeXml(line.text)}</fo:block>\n`;
  }
  return `    <fo:block-container position="absolute" top="${top}pt" `
    + `left="${x}pt" width="${width}pt" overflow="visible">\n${
      blocks}    </fo:block-container>\n`;
}

function renderFooter(pageNumber, totalPages) {
  const text = `Page ${pageNumber} of ${totalPages}`;
  const top = (PAGE_HEIGHT - FOOTER_Y - aboveTop(FOOTER_SIZE)).toFixed(3);
  return `    <fo:block-container position="absolute" top="${top}pt" `
    + `left="${MAIN_X}pt" width="${(PAGE_WIDTH - MAIN_X - 24).toFixed(1)}pt" `
    + 'overflow="visible">\n'
    + `      <fo:block font-family="NotoSans" font-size="${FOOTER_SIZE}pt" `
    + `line-height="${FOOTER_SIZE}pt" wrap-option="no-wrap" `
    + `text-align="right">${escapeXml(text)}</fo:block>\n`
    + '    </fo:block-container>\n';
}

// Renders a full fixture from a Layout, with or without the "Page N of M"
// footer (other.pdf has none). FOP accepts fo:block-container as a direct
// child of fo:flow, so each column and the footer are written straight in.
function renderDocument(layout, { footers = true } = {}) {
  const totalPages = layout.pages.length;
  let sequences = '';
  layout.pages.forEach((page, index) => {
    const pageNumber = index + 1;
    let flow = renderColumn(SIDEBAR_X, page.sidebar);
    flow += renderColumn(MAIN_X, page.main);
    if (footers) flow += renderFooter(pageNumber, totalPages);
    sequences += '  <fo:page-sequence master-reference="page">\n'
      + '    <fo:flow flow-name="xsl-region-body">\n'
      + flow
      + '    </fo:flow>\n'
      + '  </fo:page-sequence>\n';
  });
  return sequences;
}

function wrapRoot(body) {
  return '<?xml version="1.0" encoding="UTF-8"?>\n'
    + '<fo:root xmlns:fo="http://www.w3.org/1999/XSL/Format">\n'
    + '  <fo:layout-master-set>\n'
    + '    <fo:simple-page-master master-name="page" '
    + `page-width="${PAGE_WIDTH}pt" page-height="${PAGE_HEIGHT}pt" `
    + 'margin="0pt">\n'
    + '      <fo:region-body margin="0pt"/>\n'
    + '    </fo:simple-page-master>\n'
    + '  </fo:layout-master-set>\n'
    + body
    + '</fo:root>\n';
}

function write(name, layout, options) {
  const xml = wrapRoot(renderDocument(layout, options));
  writeFileSync(join(OUT_DIR, `${name}.fo`), xml, 'utf8');
  console.log(`wrote ${name}.fo (${layout.pages.length} page(s))`);
  if (process.env.DEBUG_LAYOUT) {
    layout.pages.forEach((page, i) => {
      const lastMain = page.main[page.main.length - 1];
      console.log(`  page ${i + 1}: main lines=${page.main.length} `
        + `last y=${lastMain ? lastMain.y.toFixed(1) : 'n/a'}`);
    });
  }
}

// ---------------------------------------------------------------------------
// basic-en: 3 pages, matching docs/design/linkedin-import.md's measured
// sizes and gaps, mirroring the shape of a real export with synthetic text.
// ---------------------------------------------------------------------------
function basicEn() {
  const l = new Layout();

  // Sidebar, page 1.
  l.sidebar('Contact', 13);
  l.sidebar('0900000000 (Home)', 10.5, 19.5);
  l.sidebar('sample.person@example.com', 10.5, 12.5);
  l.sidebar('linkedin.com/in/sample-person', 11, 24.5);
  l.sidebar('(LinkedIn)', 11, 14.5);
  l.sidebar('example.com/sample-person', 11, 13.5);
  l.sidebar('(Portfolio)', 11, 13);
  l.sidebar('Top Skills', 13, 35);
  l.sidebar('Product Management', 10.5, 19.5);
  l.sidebar('Cross-functional Team Leadership', 10.5, 17.5);
  l.sidebar('Public Speaking', 10.5, 17.5);
  l.sidebar('Languages', 13, 34.5);
  l.sidebar('Vietnamese (Native or Bilingual)', 10.5, 19.5);
  l.sidebar('English (Professional Working)', 10.5, 17.5);
  l.sidebar('Certifications', 13, 34.5);
  l.sidebar('Example Certified Professional', 10.5, 19.5);
  l.sidebar('Example Advanced Certificate', 10.5, 17.5);

  // Main, page 1: name, headline, location, summary, experience.
  l.main('Sample Person', 26);
  l.main('Product Manager # Example-first & Co', 12, 21);
  l.main('Ho Chi Minh City, Vietnam', 12, 15.5);
  l.main('Summary', 16, 37.5);
  l.main('A product manager at Example Co, running a Sample Product team', 12,
    25.5);
  l.main('focused on measurable outcomes over an example number of years', 12,
    18);
  l.main('in software delivery, team building, and customer research.', 12,
    18);
  l.main('Experience', 16, 54);
  l.main('Example Co.', 12, 30.5);
  l.main('Senior Product Manager, Head of PM Function', 11.5, 16);
  l.main('July 2019 - August 2023 (4 years 2 months)', 10.5, 14.5);
  l.main('Ho Chi Minh City, Vietnam', 10.5, 14.5);
  l.main('Led a cross-functional squad shipping billing, notifications,', 10.5,
    21.5);
  l.main('and reporting features across web and mobile clients,', 10.5, 18);
  l.main('coordinating design, engineering, and customer support around', 10.5,
    18);
  l.main('a quarterly roadmap. Ran discovery interviews, wrote specs,', 10.5,
    18);
  l.main('and tracked adoption metrics after each release across two', 10.5,
    18);
  l.main('Mẫu Group', 12, 38.5);
  l.main('Product Manager', 11.5, 16);
  l.main('2 years 1 month', 10.5, 16.5);
  l.main('Product Manager, Growth', 11.5, 21.5);
  l.main('April 2017 - March 2018 (1 year)', 10.5, 14.5);
  l.main('Hanoi, Vietnam', 10.5, 14.5);
  l.main('Product Manager, Platform', 11.5, 35);
  l.main('April 2016 - April 2017 (1 year 1 month)', 10.5, 14.5);
  l.main('Hanoi, Vietnam', 10.5, 14.5);
  l.main('Owned the internal platform roadmap and a small team of two', 10.5,
    21.5);
  l.main('engineers, working closely with design and support.', 10.5, 18);
  l.main('Example Co. Two', 12, 38.5);
  l.main('Founding Product Manager', 11.5, 16);
  l.main('June 2014 - March 2016 (1 year 10 months)', 10.5, 14.5);
  l.main('Da Nang, Vietnam', 10.5, 14.5);
  l.main('Built the first version of the product with a founding team of', 10.5,
    21.5);
  l.main('three, covering discovery, design, and a public launch across', 10.5,
    18);
  l.main('web and a companion mobile app.', 10.5, 18);

  // Education starts on its own page, as in the real export this mirrors.
  l.newPage();
  l.main('Education', 16);
  l.main('University of Sample Studies', 12, 25.5);
  l.main('Bachelor of Science - BS, Computer Science · (December 2011 -', 10.5,
    17.5);
  l.main('November 2015)', 10.5, 18);
  l.main('Sample Vocational College', 12, 33.5);
  l.main('Diploma of Business - DB, Business Administration · (2008 - 2010)',
    10.5, 17.5);

  write('basic-en', l);
}

// ---------------------------------------------------------------------------
// wraps-en: a wrapped headline with a location, a wrapped profile URL,
// skill, language, certificate, and job title; a sidebar continuing to
// page 2.
// ---------------------------------------------------------------------------
function wrapsEn() {
  const l = new Layout();

  l.sidebar('Contact', 13);
  l.sidebar('0900000000 (Home)', 10.5, 19.5);
  l.sidebar('sample.wrap@example.com', 10.5, 12.5);
  l.sidebar('linkedin.com/in/sample-person-with-a-very-long-vanity', 11, 24.5);
  l.sidebar('-slug-that-wraps', 11, 12.5);
  l.sidebar('(LinkedIn)', 11, 14.5);
  l.sidebar('Top Skills', 13, 35);
  l.sidebar('An Example Skill Name That Is Long Enough To Wrap Onto', 10.5,
    19.5);
  l.sidebar('A Second Visual Line', 10.5, 12.5);
  l.sidebar('Public Speaking', 10.5, 17.5);
  l.sidebar('Languages', 13, 34.5);
  l.sidebar('Vietnamese, at a Native or Bilingual level that wraps', 10.5,
    19.5);
  l.sidebar('onto a second line', 10.5, 12.5);
  l.sidebar('Certifications', 13, 34.5);
  l.sidebar('An Example Certification Whose Long Title Wraps Onto A', 10.5,
    19.5);
  l.sidebar('Second Visual Line', 10.5, 12.5);
  // Sidebar continues on page 2: split by hand into a second static block.
  l.breakSidebarToNextPage();
  l.sidebar('Second Example Certificate', 10.5, 17.5);

  l.main('Sample Wrapperson', 26);
  l.main('Product Manager, Example Co # a headline long enough to wrap', 12,
    21);
  l.main('a second visual line, followed by the location', 12, 18);
  l.main('Ho Chi Minh City, Vietnam', 12, 15.5);
  l.main('Summary', 16, 37.5);
  l.main('A short summary paragraph.', 12, 25.5);
  l.main('Experience', 16, 54);
  l.main('Example Co.', 12, 30.5);
  l.main('Senior Product Manager Whose Title Is Long Enough To Wrap', 11.5, 16);
  l.main('Onto A Second Visual Line', 11.5, 12.5);
  l.main('July 2019 - Present (7 years 2 months)', 10.5, 14.5);
  l.main('Ho Chi Minh City, Vietnam', 10.5, 14.5);
  l.main('A short description.', 10.5, 21.5);

  write('wraps-en', l);
}

// ---------------------------------------------------------------------------
// dates-en: every accepted date form, Present, year only, an unreadable
// date, a start after the end, "less than a year" as a date duration and as
// a group duration.
// ---------------------------------------------------------------------------
function datesEn() {
  const l = new Layout();
  l.sidebar('Contact', 13);
  l.sidebar('0900000000 (Home)', 10.5, 19.5);
  l.sidebar('sample.dates@example.com', 10.5, 12.5);

  l.main('Sample Dateperson', 26);
  l.main('Example Analyst', 12, 21);
  l.main('Ho Chi Minh City, Vietnam', 12, 15.5);
  l.main('Experience', 16, 54);

  l.main('Example Co. A', 12, 30.5);
  l.main('Analyst', 11.5, 16);
  l.main('January 2020 - Present (5 years 8 months)', 10.5, 14.5);
  l.main('Remote', 10.5, 14.5);

  l.main('Example Co. B', 12, 38.5);
  l.main('Analyst', 11.5, 16);
  l.main('2018 - 2019 (less than a year)', 10.5, 14.5);
  l.main('Remote', 10.5, 14.5);

  l.main('Example Co. C', 12, 38.5);
  l.main('Analyst', 11.5, 16);
  l.main('March 2021 - February 2021 (less than a year)', 10.5, 14.5);
  l.main('Remote', 10.5, 14.5);

  l.main('Example Co. D', 12, 38.5);
  l.main('Analyst', 11.5, 16);
  l.main('Someday 2019 - Another Day 2020', 10.5, 14.5);
  l.main('Remote', 10.5, 14.5);

  l.main('Example Co. E', 12, 38.5);
  l.main('Analyst', 11.5, 16);
  l.main('less than a year', 10.5, 16.5);
  l.main('Junior Analyst', 11.5, 21.5);
  l.main('June 2022 - August 2022 (less than a year)', 10.5, 14.5);
  l.main('Remote', 10.5, 14.5);

  write('dates-en', l);
}

// ---------------------------------------------------------------------------
// dropped-en: sections the parser drops (design, "What is dropped").
// ---------------------------------------------------------------------------
function droppedEn() {
  const l = new Layout();
  l.sidebar('Contact', 13);
  l.sidebar('0900000000 (Home)', 10.5, 19.5);
  l.sidebar('Honors-Awards', 13, 34.5);
  l.sidebar('Example Award, Example Co.', 10.5, 19.5);
  l.sidebar('Publications', 13, 34.5);
  l.sidebar('Example Publication Title', 10.5, 19.5);
  l.sidebar('Example Journal · 2021', 10.5, 12.5);
  l.sidebar('Patents', 13, 34.5);
  l.sidebar('Example Patent, US0000000', 10.5, 19.5);

  l.main('Sample Droppedperson', 26);
  l.main('Example Engineer', 12, 21);
  l.main('Ho Chi Minh City, Vietnam', 12, 15.5);
  l.main('Volunteer Experience', 16, 54);
  l.main('Example Nonprofit', 12, 30.5);
  l.main('Volunteer', 11.5, 16);
  l.main('2019 - 2020 (1 year)', 10.5, 14.5);
  l.main('Projects', 16, 54);
  l.main('Example Side Project', 12, 30.5);
  l.main('An internal tool built with a small team.', 10.5, 21.5);

  write('dropped-en', l);
}

// ---------------------------------------------------------------------------
// vietnamese-letters: English headings, name and one employer carry
// Vietnamese letters.
// ---------------------------------------------------------------------------
function vietnameseLetters() {
  const l = new Layout();
  l.sidebar('Contact', 13);
  l.sidebar('0900000000 (Home)', 10.5, 19.5);
  l.sidebar('nguyen.van.mau@example.com', 10.5, 12.5);

  l.main('Nguyễn Văn Mẫu', 26);
  l.main('Product Manager', 12, 21);
  l.main('Ho Chi Minh City, Vietnam', 12, 15.5);
  l.main('Experience', 16, 54);
  l.main('Mẫu Group', 12, 30.5);
  l.main('Product Manager', 11.5, 16);
  l.main('January 2020 - Present (5 years 8 months)', 10.5, 14.5);
  l.main('Ho Chi Minh City, Vietnam', 10.5, 14.5);

  write('vietnamese-letters', l);
}

// ---------------------------------------------------------------------------
// localized-vi: same layout, Vietnamese headings, LinkedIn footers.
// ---------------------------------------------------------------------------
function localizedVi() {
  const l = new Layout();
  l.sidebar('Liên hệ', 13);
  l.sidebar('0900000000 (Nhà riêng)', 10.5, 19.5);
  l.sidebar('mau.nguyen@example.com', 10.5, 12.5);
  l.sidebar('Kỹ năng hàng đầu', 13, 35);
  l.sidebar('Quản lý sản phẩm', 10.5, 19.5);
  l.sidebar('Lãnh đạo nhóm', 10.5, 17.5);

  l.main('Nguyễn Văn Mẫu', 26);
  l.main('Quản lý sản phẩm', 12, 21);
  l.main('Thành phố Hồ Chí Minh, Việt Nam', 12, 15.5);
  l.main('Tóm tắt', 16, 37.5);
  l.main('Một quản lý sản phẩm với nhiều năm kinh nghiệm.', 12, 25.5);
  l.main('Kinh nghiệm', 16, 54);
  l.main('Mẫu Group', 12, 30.5);
  l.main('Quản lý sản phẩm', 11.5, 16);
  l.main('Tháng 1 2020 - Hiện tại (5 năm 8 tháng)', 10.5, 14.5);
  l.main('Thành phố Hồ Chí Minh, Việt Nam', 10.5, 14.5);
  l.main('Học vấn', 16, 54);
  l.main('Trường Đại học Mẫu', 12, 25.5);
  l.main('Cử nhân Khoa học Máy tính · (2011 - 2015)', 10.5, 17.5);

  write('localized-vi', l);
}

// ---------------------------------------------------------------------------
// limits-en: 20 pages; a 220-character headline; 80 roles; one description
// over 16 KiB of UTF-8.
// ---------------------------------------------------------------------------
function limitsEn() {
  const l = new Layout();
  l.sidebar('Contact', 13);
  l.sidebar('0900000000 (Home)', 10.5, 19.5);
  l.sidebar('sample.limits@example.com', 10.5, 12.5);

  const headline = 'Example Role # '.repeat(15).slice(0, 220);
  l.main('Sample Limitsperson', 26);
  l.main(headline, 12, 21);
  l.main('Ho Chi Minh City, Vietnam', 12, 15.5);
  l.main('Experience', 16, 54);

  // A description over 16 KiB of UTF-8, as one very long wrapped line: the
  // extracted text is what the parser measures, not the visual layout, so
  // this line is allowed to auto-wrap (unlike every other line here).
  let longDescription = '';
  const sentence = 'This role covered a wide range of example duties across '
    + 'product, design, and engineering collaboration. ';
  while (Buffer.byteLength(longDescription, 'utf8') < 16 * 1024 + 100) {
    longDescription += sentence;
  }

  // 4 roles per page for the 80 roles: an explicit page break every 4th
  // role, so the page count (20) does not depend on the auto-break margin,
  // which the giant description below would otherwise throw off.
  const ROLES_PER_PAGE = 4;
  for (let i = 0; i < 80; i += 1) {
    if (i > 0 && i % ROLES_PER_PAGE === 0) l.newPage();
    const company = `Example Co. ${i + 1}`;
    const gap = i % ROLES_PER_PAGE === 0 ? 30.5 : 38.5;
    l.main(company, 12, gap);
    l.main(`Analyst Level ${i + 1}`, 11.5, 16);
    l.main(`January 20${String(10 + (i % 10)).padStart(2, '0')} - `
      + `Present (1 year)`, 10.5, 14.5);
    l.main('Remote', 10.5, 14.5);
    if (i === 40) {
      // The one role carrying the over-16-KiB description, as a single
      // long fo:block that FOP wraps naturally within the column width. It
      // is allowed to overflow its page visually: pdf.js still extracts
      // every text-showing operator regardless of where it falls on the
      // page, and this fixture's purpose is the character count, not the
      // layout.
      l.push('main', longDescription, 10.5, 21.5);
      l.lastLine('main').noWrap = false;
    }
  }

  if (l.pageCount !== 20) {
    throw new Error(`limits-en must render as 20 pages, got ${l.pageCount}`);
  }

  write('limits-en', l);
}

// ---------------------------------------------------------------------------
// injection-en: script tag, img onerror, and javascript: text in every
// mapped field and contact line.
// ---------------------------------------------------------------------------
function injectionEn() {
  const payload = '<script>alert(1)</script> <img src=x onerror=alert(1)> '
    + 'javascript:alert(1)';
  const l = new Layout();
  l.sidebar('Contact', 13);
  l.sidebar(`0900000000 (Home) ${payload}`, 10.5, 19.5);
  l.sidebar(`sample+${payload}@example.com`, 10.5, 12.5);
  l.sidebar(`example.com/${payload}`, 11, 24.5);
  l.sidebar('(LinkedIn)', 11, 14.5);
  l.sidebar('Top Skills', 13, 35);
  l.sidebar(payload, 10.5, 19.5);

  l.main(`Sample Person ${payload}`, 26);
  l.main(payload, 12, 21);
  l.main(payload, 12, 15.5);
  l.main('Summary', 16, 37.5);
  l.main(payload, 12, 25.5);
  l.main('Experience', 16, 54);
  l.main(payload, 12, 30.5);
  l.main(payload, 11.5, 16);
  l.main('January 2020 - Present (5 years 8 months)', 10.5, 14.5);
  l.main(payload, 10.5, 14.5);
  l.main(payload, 10.5, 21.5);
  l.main('Education', 16, 54);
  l.main(payload, 12, 25.5);
  l.main(`${payload} · (2011 - 2015)`, 10.5, 17.5);

  write('injection-en', l);
}

// ---------------------------------------------------------------------------
// other.pdf: an ordinary two-page document, no "Page N of M" footer.
// ---------------------------------------------------------------------------
function other() {
  const l = new Layout();
  l.main('Sample Document', 16);
  l.main('This is an ordinary two-page document with no LinkedIn export', 12,
    25.5);
  l.main('footer, used to test the not-a-LinkedIn-export message.', 12, 18);
  l.newPage();
  l.main('It has a second page of unrelated text.', 12, 30.5);

  write('other', l, { footers: false });
}

basicEn();
wrapsEn();
datesEn();
droppedEn();
vietnameseLetters();
localizedVi();
limitsEn();
injectionEn();
other();
