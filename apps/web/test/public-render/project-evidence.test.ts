// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { JSDOM } from 'jsdom';
import { afterEach, describe, expect, it } from 'vitest';

import { evidenceRows } from '../../app/components/public/evidenceRows';
import {
  decodePublicRenderEnvelope,
} from '../../server/utils/public-render/envelope';
import type {
  PublicRenderProjectListRequest,
  PublicRenderProjectRequest,
} from '../../server/utils/public-render/project-envelope';
import {
  renderPublicProject,
  renderPublicProjectList,
} from '../../server/workers/public-render/project-render';

type Envelope = Record<string, unknown>;

const corpus = JSON.parse(readFileSync(
  resolve(
    process.cwd(),
    '../server/internal/publicapi/testdata/project-render.json',
  ),
  'utf8',
)) as { cases: { name: string; envelope: Envelope }[] };

const VERSIONS = { style: '0123456789abcdef', script: 'fedcba9876543210' };

const envelopeNamed = (name: string): Envelope =>
  structuredClone(corpus.cases.find((entry) => entry.name === name)!.envelope);

const items = (envelope: Envelope) => envelope.evidence as Envelope[];
const resume = (envelope: Envelope) => envelope.resume as Envelope;

const render = async (
  change: (envelope: Envelope) => void = () => {},
  name = 'project-vi-evidence',
) => {
  const envelope = envelopeNamed(name);
  change(envelope);
  const request = decodePublicRenderEnvelope(JSON.stringify(envelope));
  const html = await renderPublicProject(
    request as PublicRenderProjectRequest, VERSIONS);
  return { html, document: new JSDOM(html).window.document };
};

const rows = (document: Document) =>
  [...document.querySelectorAll('li.project-evidence-item')];

const text = (element: Element | null) => element?.textContent ?? null;

const SECTION = { vi: 'Bằng chứng trên GitHub', en: 'GitHub evidence' };
const LABELS = {
  vi: ['Chủ sở hữu kho mã này', 'Tác giả của pull request đã được merge',
    'Tác giả của bản phát hành này'],
  en: ['Owner of this repository', 'Author of a merged pull request',
    'Author of this release'],
};

describe('project evidence section', () => {
  const zone = process.env.TZ;
  afterEach(() => {
    process.env.TZ = zone;
  });

  it('follows About and precedes the footer in the text column', async () => {
    const { document } = await render();
    const column = document.querySelector('.project-text')!;
    expect([...column.children].map((child) => child.className)).toEqual([
      'project-about', 'project-evidence-section', 'project-more',
    ]);
    expect(document.querySelector('.project-facts')).toBeNull();
    expect(document.querySelectorAll('.project-evidence-section'))
      .toHaveLength(1);
  });

  it('stays out of the facts panel beside every fact', async () => {
    const { document } = await render(undefined, 'project-en-every-field');
    expect(document.querySelector('.project-evidence-section')).toBeNull();
    const withFacts = await render((envelope) => {
      envelope.evidence = items(envelopeNamed('project-vi-evidence'));
    }, 'project-en-every-field');
    const column = withFacts.document.querySelector('.project-text')!;
    expect([...column.children].map((child) => child.className)).toEqual([
      'project-outcome', 'project-about', 'project-evidence-section',
      'project-more',
    ]);
    expect(withFacts.document.querySelector(
      '.project-facts .project-evidence, .project-facts '
      + '.project-evidence-section')).toBeNull();
  });

  it('gives a page with only evidence the text column', async () => {
    const { document } = await render(undefined, 'project-en-evidence-only');
    const main = document.querySelector('main')!;
    expect([...main.children].map((child) => child.tagName + '.'
      + child.className)).toEqual(['NAV.project-breadcrumb', 'H1.',
      'DIV.project-text']);
    expect(document.querySelector('.project-text')!.children)
      .toHaveLength(1);
  });

  it.each(['vi', 'en'] as const)('names its region by its %s heading',
    async (lng) => {
      const { document } = await render((envelope) => {
        Object.assign(resume(envelope), { lng, uiLng: lng });
      });
      const section = document.querySelector('.project-evidence-section')!;
      expect(section.tagName).toBe('SECTION');
      const heading = section.querySelector('h2')!;
      expect(section.getAttribute('aria-labelledby')).toBe(heading.id);
      expect(heading.id).toBe('project-evidence-title');
      expect(heading.textContent).toBe(SECTION[lng]);
      expect(section.querySelectorAll('ul.project-evidence-list'))
        .toHaveLength(1);
      expect(rows(document).map((row) => text(row.querySelector(
        '.project-evidence-label')))).toEqual([...LABELS[lng], null]);
    });

  it('writes a confirmed row and a plain row', async () => {
    const { document } = await render();
    const [confirmed, , , plain] = rows(document);
    expect([...confirmed!.children].map((child) => child.className)).toEqual([
      'project-evidence-label', 'project-evidence-heading',
      'project-evidence-meta',
    ]);
    expect(text(confirmed!.querySelector('.project-evidence-meta')))
      .toBe('03/2019 · kiểm tra 11/2026');
    expect([...plain!.children].map((child) => child.className)).toEqual([
      'project-evidence-heading', 'project-evidence-meta',
    ]);
    expect(text(plain!.querySelector('.project-evidence-meta')))
      .toBe('babbage/engine');
    expect(text(rows(document)[1]!.querySelector('.project-evidence-meta')))
      .toBe('ada/may-phan-tich · 12/2025 · kiểm tra 11/2026');
  });

  it('writes each link with exactly its attributes and title', async () => {
    const { html, document } = await render();
    const anchors = [...document.querySelectorAll('a.project-evidence')];
    expect(anchors.map((anchor) => [
      [...anchor.attributes].map(({ name, value }) => `${name}=${value}`),
      anchor.textContent,
    ])).toEqual(items(envelopeNamed('project-vi-evidence')).map((item) => [
      ['class=project-evidence', `href=${item.url as string}`,
        'rel=noopener noreferrer nofollow'],
      item.title,
    ]));
    expect(html).toContain('<a class="project-evidence" '
      + 'href="https://github.com/babbage/engine/pull/12" '
      + 'rel="noopener noreferrer nofollow">'
      + 'Sửa &lt;lỗi&gt; &amp; &quot;trích dẫn&quot;</a>');
    for (const anchor of anchors) {
      expect(anchor.children).toHaveLength(0);
      expect(anchor.parentElement!.className).toBe('project-evidence-heading');
    }
  });

  it('keeps the owner\'s order', async () => {
    const { document } = await render((envelope) => {
      envelope.evidence = [...items(envelope)].reverse();
    });
    expect([...document.querySelectorAll('a.project-evidence')]
      .map((anchor) => anchor.textContent)).toEqual([
      'Sửa <lỗi> & "trích dẫn"', 'v1.0', 'Thêm ghi chú G', 'ada/may-phan-tich',
    ]);
  });

  it('writes no section and no evidence markup when there is none',
    async () => {
      const { html } = await render((envelope) => {
        envelope.evidence = [];
      });
      expect(html).not.toMatch(/project-evidence|GitHub/u);
      const list = await renderPublicProjectList(decodePublicRenderEnvelope(
        JSON.stringify(envelopeNamed('list-vi-two-projects')),
      ) as PublicRenderProjectListRequest, VERSIONS);
      expect(list).not.toMatch(/project-evidence|GitHub/u);
    });

  it.each([
    ['vi', 'MM/YYYY', '12/2025 · kiểm tra 11/2026'],
    ['vi', 'Mon YYYY', 'thg 12 2025 · kiểm tra thg 11 2026'],
    ['vi', 'YYYY', '2025 · kiểm tra 2026'],
    ['en', 'MM/YYYY', '12/2025 · checked 11/2026'],
    ['en', 'Mon YYYY', 'Dec 2025 · checked Nov 2026'],
    ['en', 'YYYY', '2025 · checked 2026'],
  ] as const)('formats %s dates as %s', async (lng, dateFormat, meta) => {
    const { document } = await render((envelope) => {
      Object.assign(resume(envelope), { lng, uiLng: lng, dateFormat });
      items(envelope)[0]!.itemDate = '2025-12-31T23:30:00Z';
    });
    expect(text(rows(document)[0]!.querySelector('.project-evidence-meta')))
      .toBe(meta);
  });

  it('reads dates in the resume language and copy in the page language',
    async () => {
      const { document } = await render((envelope) => {
        Object.assign(resume(envelope), {
          lng: 'fr', uiLng: 'en', dateFormat: 'Mon YYYY',
        });
      });
      expect(text(rows(document)[0]!.querySelector('.project-evidence-meta')))
        .toBe('Mar 2019 · checked Nov 2026');
    });

  it('reads the month from the UTC string in any time zone', async () => {
    process.env.TZ = 'Asia/Ho_Chi_Minh';
    expect(new Date('2025-12-31T23:30:00Z').getMonth()).toBe(0);
    const { document } = await render((envelope) => {
      items(envelope)[0]!.itemDate = '2025-12-31T23:30:00Z';
      items(envelope)[0]!.checkedAt = '2025-12-31T23:30:00Z';
    });
    expect(text(rows(document)[0]!.querySelector('.project-evidence-meta')))
      .toBe('12/2025 · kiểm tra 12/2025');
  });

  it('draws hidden icons outside each link', async () => {
    const { document } = await render();
    for (const row of rows(document)) {
      const heading = row.querySelector('.project-evidence-heading')!;
      expect([...heading.children].map((child) => child.tagName.toLowerCase()))
        .toEqual(['svg', 'a']);
      for (const icon of row.querySelectorAll('svg')) {
        expect(icon.getAttribute('aria-hidden')).toBe('true');
        expect(icon.getAttribute('focusable')).toBe('false');
        expect([icon.getAttribute('width'), icon.getAttribute('height')])
          .toEqual(['16', '16']);
      }
    }
    const label = rows(document)[0]!.querySelector('.project-evidence-label')!;
    expect(label.firstElementChild!.tagName.toLowerCase()).toBe('svg');
    expect(rows(document)[3]!.querySelectorAll('svg')).toHaveLength(1);
  });

  it('escapes hostile text', async () => {
    const hostile = '"><script>alert(1)</script>\'&';
    const { html, document } = await render((envelope) => {
      Object.assign(items(envelope)[1]!, {
        title: hostile, repositoryName: `${hostile}x`,
      });
    });
    expect(html).not.toContain('<script');
    expect(document.querySelectorAll('script')).toHaveLength(0);
    expect(text(rows(document)[1]!.querySelector('a.project-evidence')))
      .toBe(hostile);
    expect(text(rows(document)[1]!.querySelector('.project-evidence-meta')))
      .toBe(`${hostile}x · 12/2025 · kiểm tra 11/2026`);
  });

  it('leaves out the repository when it equals the title', async () => {
    const { document } = await render((envelope) => {
      items(envelope)[3]!.title = 'babbage/engine';
    });
    expect(rows(document)[3]!.querySelector('.project-evidence-meta'))
      .toBeNull();
    expect(text(rows(document)[0]!.querySelector('.project-evidence-meta')))
      .toBe('03/2019 · kiểm tra 11/2026');
  });
});

describe('evidence rows', () => {
  it('derive the label from the kind and the mark only', () => {
    const item = {
      kind: 'release' as const, title: 'v1', url: 'https://github.com/a/b/releases/tag/v1',
      repositoryName: 'a/b', itemDate: '2024-07-01T00:00:00Z',
      checkedAt: '2026-11-02T08:00:00Z', confirmed: true,
    };
    const [confirmed, plain] = evidenceRows(
      [item, { ...item, confirmed: false }], 'YYYY', 'en', 'en');
    expect(confirmed).toEqual({
      kind: 'release', title: 'v1', url: item.url,
      label: 'Author of this release', meta: 'a/b · 2024 · checked 2026',
    });
    expect(plain).toEqual({
      kind: 'release', title: 'v1', url: item.url, meta: 'a/b',
    });
  });
});
