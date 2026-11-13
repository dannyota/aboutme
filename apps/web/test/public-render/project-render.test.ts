// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { HOSTILE_CORPUS } from '@aboutme/schema/sanitizer';
import { ALLOWED_TAGS } from '@aboutme/schema/sanitizer-policy';
import { JSDOM } from 'jsdom';
import { describe, expect, it } from 'vitest';

import {
  decodePublicRenderEnvelope,
  PUBLIC_RENDER_FAILURE,
} from '../../server/utils/public-render/envelope';
import type {
  PublicRenderProjectListRequest,
  PublicRenderProjectRequest,
} from '../../server/utils/public-render/project-envelope';
import {
  renderPublicProject,
  renderPublicProjectList,
} from '../../server/workers/public-render/project-render';
import {
  PROJECT_PAGE_STYLE,
} from '../../server/workers/public-render/project-style';

const corpus = JSON.parse(readFileSync(
  resolve(
    process.cwd(),
    '../server/internal/publicapi/testdata/project-render.json',
  ),
  'utf8',
)) as {
  styleVersion: string;
  cases: { name: string; envelope: Record<string, unknown>; html: string }[];
};

const VERSIONS = { style: '0123456789abcdef', script: 'fedcba9876543210' };

type Envelope = Record<string, unknown>;

const envelopeNamed = (name: string): Envelope =>
  structuredClone(corpus.cases.find((entry) => entry.name === name)!.envelope);

// Every request goes through the closed decoder first, as in the worker.
const listRequest = (change: (envelope: Envelope) => void = () => {}) => {
  const envelope = envelopeNamed('list-vi-two-projects');
  change(envelope);
  return decodePublicRenderEnvelope(
    JSON.stringify(envelope),
  ) as PublicRenderProjectListRequest;
};

const projectRequest = (
  change: (envelope: Envelope) => void = () => {},
  name = 'project-en-every-field',
) => {
  const envelope = envelopeNamed(name);
  change(envelope);
  // Go derives the page description from the project's text: the summary,
  // else the first 160 characters of the outcome, else none.
  const { summary, outcome }
    = envelope.project as Record<string, string | undefined>;
  (envelope.preview as Envelope).description = summary
    ?? (outcome === undefined ? '' : [...outcome].slice(0, 160).join(''));
  return decodePublicRenderEnvelope(
    JSON.stringify(envelope),
  ) as PublicRenderProjectRequest;
};

const project = (envelope: Envelope) => envelope.project as Envelope;
const resume = (envelope: Envelope) => envelope.resume as Envelope;
const rows = (envelope: Envelope) => envelope.projects as Envelope[];

const parse = (html: string) => new JSDOM(html).window.document;

const texts = (root: ParentNode, selector: string) =>
  [...root.querySelectorAll(selector)].map((node) => node.textContent);

const attributes = (element: Element) =>
  Object.fromEntries([...element.attributes].map((attribute) =>
    [attribute.name, attribute.value]));

const listHtml = (change?: (envelope: Envelope) => void) =>
  renderPublicProjectList(listRequest(change), VERSIONS);

const projectHtml = (
  change?: (envelope: Envelope) => void,
  name?: string,
) => renderPublicProject(projectRequest(change, name), VERSIONS);

// What the contract forbids anywhere in either page
// (docs/design/projects-contract.md "Render envelope").
const expectClosedDocument = (document: Document) => {
  expect(document.querySelectorAll(
    'script, form, iframe, img, video, audio, object, embed, source, base',
  )).toHaveLength(0);
  expect(document.querySelectorAll('main#public-project')).toHaveLength(1);
  expect(document.querySelectorAll('main')).toHaveLength(1);
  expect(document.querySelectorAll('h1')).toHaveLength(1);
  expect(texts(document, 'a[href="#public-project"]'))
    .toEqual(['Skip to content']);
  // No inline CSS: the pages run under style-src 'self'.
  expect(document.querySelectorAll('style, [style]')).toHaveLength(0);
  expect([...document.querySelectorAll('link[rel="stylesheet"]')]
    .map((link) => link.getAttribute('href'))).toEqual([
    '/_nuxt/assets/print-fonts.css?v=0123456789abcdef',
    '/_nuxt/assets/project-pages.css?v=0123456789abcdef',
  ]);
  for (const element of document.querySelectorAll('*')) {
    for (const { name } of element.attributes) {
      expect(name.startsWith('on'), name).toBe(false);
      expect(['style', 'src', 'srcset', 'action', 'data', 'ping'])
        .not.toContain(name);
    }
  }
  const credits = document.querySelectorAll('a.public-credit');
  expect(credits).toHaveLength(1);
  expect(attributes(credits[0]!)).toEqual({
    class: 'public-credit', href: 'https://aboutme.example/',
  });
  const back = document.querySelectorAll('a.public-back');
  expect(back).toHaveLength(1);
  expect(back[0]!.getAttribute('href')).toBe('https://aboutme.example/ada');
  expect(document.querySelector('.public-download')).toBeNull();
};

// Internal anchors carry only href and class; external ones are the
// project's buttons with the fixed rel.
const expectAnchorAttributes = (document: Document) => {
  for (const anchor of document.querySelectorAll('a')) {
    if (anchor.closest('.project-description') !== null) continue;
    const names = [...anchor.attributes].map(({ name }) => name).sort();
    if (anchor.classList.contains('public-back')) {
      expect(names).toEqual(['aria-label', 'class', 'href']);
    } else if (anchor.classList.contains('project-external')) {
      expect(names).toEqual(['class', 'href', 'rel']);
      expect(anchor.getAttribute('rel')).toBe('noopener noreferrer nofollow');
    } else {
      expect(names.every((name) => name === 'href' || name === 'class'))
        .toBe(true);
    }
  }
};

describe('project list page', () => {
  it('writes a complete, deterministic Vietnamese document', async () => {
    const html = await listHtml();
    await expect(listHtml()).resolves.toBe(html);
    expect(html.startsWith('<!doctype html><html lang="vi-VN" '
      + 'data-scheme="system"><head>')).toBe(true);
    const document = parse(html);
    expectClosedDocument(document);
    expectAnchorAttributes(document);
    expect(document.title).toBe('Dự án · Ada Lovelace');
    expect(document.querySelector('h1')!.textContent).toBe('Dự án');
    expect(document.querySelector('.public-credit')!.textContent)
      .toBe('Tạo bằng aboutme.vn');
    const back = document.querySelector('.public-back')!;
    expect(back.textContent).toBe('← Ada Lovelace');
    expect(back.getAttribute('aria-label')).toBe('Về CV của Ada Lovelace');
    const owner = document.querySelector('.project-owner a')!;
    expect(owner.getAttribute('href')).toBe('https://aboutme.example/ada');
    expect(owner.textContent).toBe('Ada Lovelace');
  });

  it('writes the list head with no description', async () => {
    const document = parse(await listHtml());
    expect(document.querySelector('link[rel="canonical"]')!
      .getAttribute('href')).toBe('https://aboutme.example/ada/projects');
    expect(document.querySelector('meta[name="description"]')).toBeNull();
    expect(document.querySelector('meta[property="og:description"]'))
      .toBeNull();
    expect(document.querySelector('meta[property="og:type"]')!
      .getAttribute('content')).toBe('website');
    expect(document.querySelector('meta[property="og:url"]')!
      .getAttribute('content')).toBe('https://aboutme.example/ada/projects');
    expect(document.querySelector('meta[property="og:title"]')!
      .getAttribute('content')).toBe('Dự án · Ada Lovelace');
    expect(document.querySelector('link[rel="icon"]')).toBeNull();
    expect(document.querySelector('meta[name="robots"]')).toBeNull();
  });

  it('writes one bordered list of rows in entry order', async () => {
    const document = parse(await listHtml());
    expect(document.querySelectorAll('ul.project-rows')).toHaveLength(1);
    const items = [...document.querySelectorAll('.project-rows > li')];
    expect(items.map((item) => item.querySelector('h2 a')!.textContent))
      .toEqual(['Máy phân tích', 'Difference Engine study']);
    expect(items.map((item) => item.querySelector('h2 a')!
      .getAttribute('href'))).toEqual([
      'https://aboutme.example/ada/projects/may-phan-tich',
      'https://aboutme.example/ada/projects/difference-engine',
    ]);
  });

  it('writes every row field that is present', async () => {
    const document = parse(await listHtml());
    const row = document.querySelector('.project-rows > li')!;
    expect(row.querySelector('.project-row-meta')!.textContent)
      .toBe('Tác giả · 2019 – 2021');
    expect(row.querySelector('.project-row-summary')!.textContent)
      .toBe('Ghi chú về máy tính cơ học đầu tiên.');
    expect(texts(row, '.project-chips li')).toEqual([
      'Toán học', 'Bìa đục lỗ', 'Thuật toán', 'Số Bernoulli', 'Dịch thuật',
      'Ghi chú', '+1',
    ]);
    expect(row.querySelector('.project-chips')!.getAttribute('aria-label'))
      .toBe('Công nghệ');
  });

  it('omits every row field that is absent', async () => {
    const document = parse(await listHtml());
    const row = document.querySelectorAll('.project-rows > li')[1]!;
    expect(row.querySelector('.project-row-meta')).toBeNull();
    expect(row.querySelector('.project-row-summary')).toBeNull();
    expect(row.querySelector('.project-chips')).toBeNull();
    expect(row.querySelectorAll('h2')).toHaveLength(1);
  });

  it('joins role and dates only when both are present', async () => {
    const roleOnly = parse(await listHtml((envelope) => {
      delete rows(envelope)[0]!.dates;
    }));
    expect(roleOnly.querySelector('.project-row-meta')!.textContent)
      .toBe('Tác giả');
    const datesOnly = parse(await listHtml((envelope) => {
      delete rows(envelope)[0]!.role;
    }));
    expect(datesOnly.querySelector('.project-row-meta')!.textContent)
      .toBe('2019 – 2021');
  });

  it('shows no more chip when the row shows every technology', async () => {
    const document = parse(await listHtml((envelope) => {
      delete rows(envelope)[0]!.moreTechnologies;
    }));
    expect(document.querySelector('.project-chips-more')).toBeNull();
    expect(document.querySelectorAll('.project-chips li')).toHaveLength(6);
  });

  it('marks only the project text of a fallback-language row', async () => {
    const document = parse(await listHtml((envelope) => {
      Object.assign(rows(envelope)[1]!, {
        role: 'Author', summary: 'A study.', technologies: ['Brass'],
        dates: { start: { y: 1922, m: 3 }, end: null, present: true },
      });
    }));
    const row = document.querySelectorAll('.project-rows > li')[1]!;
    expect(row.querySelector('h2')!.getAttribute('lang')).toBe('en');
    expect(row.querySelector('.project-row-meta span[lang="en"]')!
      .textContent).toBe('Author');
    expect(row.querySelector('.project-row-summary')!.getAttribute('lang'))
      .toBe('en');
    // Dates follow the resume language and the chips are not text.
    expect(row.querySelector('.project-row-meta')!.textContent)
      .toBe('Author · 1922 – Hiện tại');
    expect(row.querySelector('.project-chips')!.hasAttribute('lang'))
      .toBe(false);
    const first = document.querySelector('.project-rows > li')!;
    expect(first.querySelectorAll('[lang]')).toHaveLength(0);
  });

  it('writes English copy for a resume in any other language', async () => {
    const document = parse(await listHtml((envelope) => {
      Object.assign(resume(envelope), { lng: 'fr', uiLng: 'en' });
    }));
    expect(document.documentElement.getAttribute('lang')).toBe('fr');
    expect(document.querySelector('h1')!.textContent).toBe('Projects');
    expect(document.querySelector('.public-credit')!.textContent)
      .toBe('Built with aboutme.vn');
    expect(document.querySelector('.public-back')!.getAttribute('aria-label'))
      .toBe('Back to Ada Lovelace\'s resume');
    expect(document.querySelector('.project-chips')!.getAttribute('aria-label'))
      .toBe('Technologies');
  });

  it('escapes every text and attribute value', async () => {
    const hostile = '"><script>alert(1)</script>\'&';
    const html = await listHtml((envelope) => {
      envelope.pageTitle = hostile;
      Object.assign(envelope.preview as Envelope, {
        title: hostile, imageAlt: hostile,
      });
      resume(envelope).ownerName = hostile;
      Object.assign(rows(envelope)[0]!, {
        name: hostile, role: hostile, summary: hostile,
        technologies: [hostile, 'b', 'c', 'd', 'e', 'f'],
      });
    });
    expect(html).not.toContain('<script');
    const document = parse(html);
    expect(document.querySelectorAll('script')).toHaveLength(0);
    expect(document.title).toBe(hostile);
    expect(document.querySelector('.public-back')!.textContent)
      .toBe(`← ${hostile}`);
    expect(document.querySelector('.project-rows h2 a')!.textContent)
      .toBe(hostile);
    expect(document.querySelector('meta[property="og:image:alt"]')!
      .getAttribute('content')).toBe(hostile);
  });
});

describe('project page', () => {
  it('writes a complete document with every field', async () => {
    const html = await projectHtml();
    await expect(projectHtml()).resolves.toBe(html);
    expect(html.startsWith('<!doctype html><html lang="en" '
      + 'data-scheme="dark"><head>')).toBe(true);
    const document = parse(html);
    expectClosedDocument(document);
    expectAnchorAttributes(document);
    expect(document.title).toBe('Analytical Engine notes · Ada Lovelace');
    expect(document.querySelector('h1')!.textContent)
      .toBe('Analytical Engine notes');
    expect(document.querySelector('.project-lead')!.textContent)
      .toBe('Notes on the first mechanical general-purpose computer.');
    expect([...document.querySelectorAll('.project-breadcrumb a')]
      .map((anchor) => [anchor.textContent, anchor.getAttribute('href')]))
      .toEqual([
        ['Ada Lovelace', 'https://aboutme.example/ada'],
        ['Projects', 'https://aboutme.example/ada/projects'],
      ]);
  });

  it('writes the page head with its description and favicon', async () => {
    const document = parse(await projectHtml());
    const content = (selector: string) =>
      document.querySelector(selector)!.getAttribute('content');
    const description
      = 'Notes on the first mechanical general-purpose computer.';
    expect(content('meta[name="description"]')).toBe(description);
    expect(content('meta[property="og:description"]')).toBe(description);
    expect(content('meta[property="og:url"]'))
      .toBe('https://aboutme.example/ada/projects/analytical-engine');
    expect(document.querySelector('link[rel="canonical"]')!
      .getAttribute('href'))
      .toBe('https://aboutme.example/ada/projects/analytical-engine');
    expect(document.querySelector('link[rel="icon"]')!.getAttribute('href'))
      .toMatch(/^data:image\/svg\+xml,/u);
    expect(content('meta[property="og:image"]')).toBe(
      'https://aboutme.example/api/v1/public/resumes/ada/og/'
      + '0123456789abcdef.png',
    );
  });

  it('names the resume card alias when no stored card is sent', async () => {
    const document = parse(await projectHtml((envelope) => {
      delete (envelope.preview as Envelope).imageUrl;
    }));
    expect(document.querySelector('meta[property="og:image"]')!
      .getAttribute('content'))
      .toBe('https://aboutme.example/api/v1/public/resumes/ada/og.png');
  });

  it('places the facts column before the text', async () => {
    const document = parse(await projectHtml());
    const columns = document.querySelector('.project-columns')!;
    expect([...columns.children].map((child) => child.className))
      .toEqual(['project-facts', 'project-text']);
  });

  it('writes the link buttons with Repository filled', async () => {
    const document = parse(await projectHtml());
    const links = [...document.querySelectorAll('a.project-external')];
    expect(links.map((link) => [
      link.textContent, link.getAttribute('href'), link.className,
    ])).toEqual([
      ['Repository', 'https://github.com/ada/analytical-engine',
        'project-external project-external-primary'],
      ['Demo', 'https://ada.example/demo', 'project-external'],
      ['Link', 'https://ada.example/notes', 'project-external'],
    ]);
    for (const link of links) {
      const icon = link.querySelector('svg')!;
      expect(icon.getAttribute('aria-hidden')).toBe('true');
      expect(icon.getAttribute('focusable')).toBe('false');
    }
  });

  it('fills Demo when it is the only button link', async () => {
    const document = parse(await projectHtml((envelope) => {
      delete project(envelope).repositoryUrl;
      delete envelope.entryLink;
    }));
    const links = [...document.querySelectorAll('a.project-external')];
    expect(links.map((link) => [link.textContent, link.className])).toEqual([
      ['Demo', 'project-external project-external-primary'],
    ]);
  });

  it('shows the entry link only when it is sent', async () => {
    const document = parse(await projectHtml((envelope) => {
      delete envelope.entryLink;
    }));
    expect(texts(document, 'a.project-external'))
      .toEqual(['Repository', 'Demo']);
    const alone = parse(await projectHtml((envelope) => {
      delete project(envelope).repositoryUrl;
      delete project(envelope).demoUrl;
    }));
    expect([...alone.querySelectorAll('a.project-external')]
      .map((link) => [link.textContent, link.className]))
      .toEqual([['Link', 'project-external']]);
  });

  it('writes role and dates as a definition list', async () => {
    const document = parse(await projectHtml());
    expect(texts(document, '.project-facts dl dt'))
      .toEqual(['Role', 'Dates']);
    expect(texts(document, '.project-facts dl dd'))
      .toEqual(['Author', 'Oct 2020 – Sep 2021']);
  });

  it('formats dates with the resume format and language', async () => {
    const document = parse(await projectHtml((envelope) => {
      Object.assign(resume(envelope), {
        lng: 'vi', uiLng: 'vi', dateFormat: 'Mon YYYY',
      });
      project(envelope).lang = 'en';
      project(envelope).dates = {
        start: { y: 2024, m: 3 }, end: null, present: true,
      };
    }));
    expect(texts(document, '.project-facts dl dt'))
      .toEqual(['Vai trò', 'Thời gian']);
    expect(document.querySelectorAll('.project-facts dl dd')[1]!.textContent)
      .toBe('thg 3 2024 – Hiện tại');
  });

  it('shows every technology chip under its heading', async () => {
    const technologies = Array.from({ length: 20 }, (_, index) =>
      `Tech ${index + 1}`);
    const document = parse(await projectHtml((envelope) => {
      project(envelope).technologies = technologies;
    }));
    expect(document.querySelector('.project-facts h2')!.textContent)
      .toBe('Technologies');
    expect(texts(document, '.project-facts ul li')).toEqual(technologies);
    expect(document.querySelector('.project-chips-more')).toBeNull();
  });

  it('writes the outcome, the given description and the footer', async () => {
    const envelope = envelopeNamed('project-en-every-field');
    const html = await projectHtml();
    const document = parse(html);
    expect(texts(document, '.project-text h2'))
      .toEqual(['Outcome', 'About the project']);
    expect(document.querySelector('.project-outcome p')!.textContent)
      .toBe('Published the first algorithm intended for a machine.');
    expect(document.querySelectorAll('.project-description')).toHaveLength(1);
    expect(html).toContain(
      `<div class="project-description">${
        project(envelope).description as string}</div>`,
    );
    const more = document.querySelector('.project-more a')!;
    expect([more.textContent, more.getAttribute('href')]).toEqual([
      'See other projects', 'https://aboutme.example/ada/projects',
    ]);
  });

  it('names each text section by its heading', async () => {
    const document = parse(await projectHtml());
    for (const section of document.querySelectorAll('.project-text section')) {
      const heading = section.querySelector('h2')!;
      expect(section.getAttribute('aria-labelledby')).toBe(heading.id);
    }
  });

  it('omits the footer when no other project is available', async () => {
    const document = parse(await projectHtml((envelope) => {
      envelope.otherProjects = false;
    }));
    expect(document.querySelector('.project-more')).toBeNull();
  });

  it('omits the facts column when it has nothing to show', async () => {
    const document = parse(await projectHtml((envelope) => {
      const { role: _role, dates: _dates, repositoryUrl: _repository,
        demoUrl: _demo, ...rest } = project(envelope);
      envelope.project = rest;
      project(envelope).technologies = [];
      delete envelope.entryLink;
    }));
    expect(document.querySelector('.project-facts')).toBeNull();
    expect(document.querySelector('.project-columns')).toBeNull();
    expect(document.querySelector('main > .project-text')).not.toBeNull();
  });

  it('omits the text column when it has nothing to show', async () => {
    const document = parse(await projectHtml((envelope) => {
      delete project(envelope).outcome;
      delete project(envelope).description;
      envelope.otherProjects = false;
    }));
    expect(document.querySelector('.project-text')).toBeNull();
    expect(document.querySelector('.project-columns')).toBeNull();
    expect(document.querySelector('main > .project-facts')).not.toBeNull();
  });

  it('omits each empty part with its heading', async () => {
    const document = parse(await projectHtml((envelope) => {
      const { role: _role, outcome: _outcome, summary: _summary, ...rest }
        = project(envelope);
      envelope.project = rest;
      project(envelope).technologies = [];
    }));
    expect(texts(document, '.project-facts dt')).toEqual(['Dates']);
    expect(document.querySelector('.project-facts h2')).toBeNull();
    expect(document.querySelector('.project-facts ul')).toBeNull();
    expect(document.querySelector('.project-outcome')).toBeNull();
    expect(document.querySelector('.project-lead')).toBeNull();
    expect(texts(document, 'h2')).toEqual(['About the project']);
  });

  it('writes only the breadcrumb and the name for a name-only page',
    async () => {
      const document = parse(await projectHtml(undefined,
        'project-en-name-only'));
      expectClosedDocument(document);
      const main = document.querySelector('main')!;
      expect([...main.children].map((child) => child.tagName))
        .toEqual(['NAV', 'H1']);
      expect(document.querySelector('meta[name="description"]')).toBeNull();
      expect(document.documentElement.getAttribute('data-scheme'))
        .toBe('light');
    });

  it('marks the project text of a fallback-language page', async () => {
    const document = parse(await projectHtml((envelope) => {
      Object.assign(resume(envelope), { lng: 'vi-VN', uiLng: 'vi' });
      project(envelope).lang = 'en';
    }));
    const marked = [...document.querySelectorAll('[lang]')]
      .filter((element) => element !== document.documentElement)
      .map((element) => [element.tagName, element.className,
        element.getAttribute('lang')]);
    expect(marked).toEqual([
      ['H1', '', 'en'],
      ['P', 'project-lead', 'en'],
      ['DD', '', 'en'],
      ['P', '', 'en'],
      ['DIV', 'project-description', 'en'],
    ]);
    expect(texts(document, 'h2')).toEqual(['Công nghệ', 'Kết quả', 'Mô tả']);
    expect(document.querySelector('.project-more a')!.textContent)
      .toBe('Xem các dự án khác');
    expect(texts(document, 'a.project-external'))
      .toEqual(['Repository', 'Demo', 'Liên kết']);
  });

  it('marks nothing when the text is in the resume language', async () => {
    const document = parse(await projectHtml());
    expect(document.querySelectorAll('body [lang]')).toHaveLength(0);
  });

  it('shows no evidence placeholder', async () => {
    const html = await projectHtml();
    expect(html).not.toMatch(/evidence|github-evidence/iu);
  });

  it('escapes every text and attribute value', async () => {
    const hostile = '"><script>alert(1)</script>\'&';
    const html = await projectHtml((envelope) => {
      resume(envelope).ownerName = hostile;
      Object.assign(project(envelope), {
        name: hostile, role: hostile, summary: hostile, outcome: hostile,
        technologies: [hostile],
        repositoryUrl: `https://x.example/"><script>alert(1)</script>`,
      });
      envelope.entryLink = 'https://x.example/\'onmouseover=\'alert(1)';
    });
    expect(html).not.toContain('<script');
    const document = parse(html);
    expect(document.querySelectorAll('script')).toHaveLength(0);
    expect(document.querySelector('h1')!.textContent).toBe(hostile);
    expect(document.querySelector('.project-outcome p')!.textContent)
      .toBe(hostile);
    expect(document.querySelector('.project-external')!.getAttribute('href'))
      .toBe('https://x.example/"><script>alert(1)</script>');
    expect([...document.querySelectorAll('a.project-external')].at(-1)!
      .getAttribute('href')).toBe('https://x.example/\'onmouseover=\'alert(1)');
    expectAnchorAttributes(document);
  });
});

describe('project page frame', () => {
  it('opts out of telephone and date detection as the resume page does',
    async () => {
      // Safari links digit runs such as 03/2022 – 06/2024 as telephone
      // numbers unless the head says otherwise.
      const head = '<meta charset="utf-8"><meta name="viewport" '
        + 'content="width=device-width, initial-scale=1"><meta '
        + 'name="format-detection" '
        + 'content="telephone=no, date=no, address=no, email=no"><title>';
      for (const html of [await listHtml(), await projectHtml()]) {
        expect(html).toContain(head);
        expect(parse(html)
          .querySelectorAll('meta[name="format-detection"]')).toHaveLength(1);
      }
    });

  it('rejects asset versions that are not 16 hex digits', async () => {
    await expect(renderPublicProject(projectRequest(), {
      style: '0123', script: VERSIONS.script,
    })).rejects.toThrow(PUBLIC_RENDER_FAILURE);
    await expect(renderPublicProjectList(listRequest(), {
      style: '0123456789ABCDEF', script: VERSIONS.script,
    })).rejects.toThrow(PUBLIC_RENDER_FAILURE);
  });

  it('carries each closed color scheme on the root', async () => {
    for (const colorScheme of ['light', 'dark', 'system']) {
      const document = parse(await listHtml((envelope) => {
        resume(envelope).colorScheme = colorScheme;
      }));
      expect(document.documentElement.getAttribute('data-scheme'))
        .toBe(colorScheme);
    }
  });

  it('writes no style element or style attribute and no style text',
    async () => {
      for (const html of [await listHtml(), await projectHtml()]) {
        expect(html).not.toMatch(/<style|\sstyle=/iu);
        // The stylesheet's rules reach the page only through its link.
        expect(html).not.toContain(PROJECT_PAGE_STYLE.slice(0, 40));
      }
    });

  it('links the fonts and the project stylesheet at one content version',
    async () => {
      const html = await renderPublicProject(projectRequest(), {
        style: 'fedcba9876543210', script: VERSIONS.script,
      });
      expect(html).toContain('<link rel="stylesheet" '
        + 'href="/_nuxt/assets/print-fonts.css?v=fedcba9876543210">'
        + '<link rel="stylesheet" '
        + 'href="/_nuxt/assets/project-pages.css?v=fedcba9876543210">'
        + '</head>');
    });
});

describe('a Go-sanitized hostile description', () => {
  // The server sanitizer's pinned output for each hostile payload
  // (apps/server/internal/sanitize/sanitize_test.go, TestCorpusOutputGolden):
  // exactly what the library stores and Go sends.
  const sanitized = JSON.parse(readFileSync(
    resolve(
      process.cwd(),
      '../server/internal/sanitize/testdata/corpus-output.golden.json',
    ),
    'utf8',
  )) as Record<string, string>;
  const cases = HOSTILE_CORPUS
    .filter(({ id }) => (sanitized[id] ?? '') !== '')
    .map(({ id }) => [id, sanitized[id]!] as const);

  it('has hostile payloads that survive sanitizing as text or links', () => {
    expect(cases.map(([id]) => id)).toEqual(expect.arrayContaining([
      'event-handler-anchor-onclick',
      'nested-script-tag-stripping-bypass',
      'rel-hardening-target-blank-missing-rel',
    ]));
  });

  it.each(cases)('renders %s as given and nothing more',
    async (_id, description) => {
      const html = await projectHtml((envelope) => {
        project(envelope).description = description;
      });
      expect(html.split('<div class="project-description">')).toHaveLength(2);
      expect(html).toContain(
        `<div class="project-description">${description}</div>`);
      const document = parse(html);
      expectClosedDocument(document);
      expectAnchorAttributes(document);
      const region = document.querySelector('.project-description')!;
      expect(region.innerHTML).toBe(new JSDOM(description).window.document
        .body.innerHTML);
      for (const element of region.querySelectorAll('*')) {
        expect(ALLOWED_TAGS).toContain(element.localName);
      }
      for (const anchor of region.querySelectorAll('a')) {
        expect(anchor.getAttribute('href')).toMatch(/^https:\/\//u);
        expect(anchor.getAttribute('rel')).toBe('noopener noreferrer');
      }
    });
});

describe('project HTML parity with the shared corpus', () => {
  // Go's project page validator reads the same corpus, so real renderer
  // output and Go's closed rules are checked against one fixture
  // (docs/design/projects-contract.md "Render envelope").
  it.each(corpus.cases.map((entry) => [entry.name, entry] as const))(
    'renders exactly the corpus HTML for %s',
    async (_name, entry) => {
      const request = decodePublicRenderEnvelope(
        JSON.stringify(entry.envelope),
      );
      const versions = { style: corpus.styleVersion, script: VERSIONS.script };
      const html = request.mode === 'project-list'
        ? await renderPublicProjectList(request, versions)
        : await renderPublicProject(
            request as PublicRenderProjectRequest, versions);
      expect(html).toBe(entry.html);
    },
  );
});
