// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { JSDOM } from 'jsdom';
import { describe, expect, it } from 'vitest';

import {
  decodePublicRenderEnvelope,
  PUBLIC_RENDER_FAILURE,
  type PublicRenderResumeRequest,
} from '../../server/utils/public-render/envelope';
import { renderPublicResume } from '../../server/workers/public-render/render';

type Envelope = Record<string, unknown>;

const corpus = JSON.parse(readFileSync(
  resolve(
    process.cwd(),
    '../server/internal/publicapi/testdata/resume-links-render.json',
  ),
  'utf8',
)) as {
  styleVersion?: string;
  cases: { name: string; envelope: Envelope; html?: string }[];
};

const VERSIONS = { style: '0123456789abcdef', script: 'fedcba9876543210' };
const PROJECTS_URL = 'https://aboutme.example/do-hoang-nam/projects';
const FIRST = '8c8b959a-0df4-59de-88d9-2ce01b88fb86';
const SECOND = '2ffd46bf-7cc3-5497-bd46-13fbe94653a4';
const WORK = 'd1aa2a3b-dfbe-5c4b-8a11-3f5289b0d3d9';

const envelopeNamed = (name: string): Envelope =>
  structuredClone(corpus.cases.find((entry) => entry.name === name)!.envelope);

const decode = (envelope: Envelope) =>
  decodePublicRenderEnvelope(JSON.stringify(envelope));

const rejects = (envelope: Envelope) =>
  expect(() => decode(envelope)).toThrow(PUBLIC_RENDER_FAILURE);

const linked = (change: (envelope: Envelope) => void): Envelope => {
  const envelope = envelopeNamed('en-two-linked-entries');
  change(envelope);
  return envelope;
};

const links = (envelope: Envelope) =>
  envelope.projectLinks as { entryId: unknown; url: unknown }[];

const document = (envelope: Envelope) =>
  (envelope.publicResume as Envelope).document as {
    content: Record<string, { sectionType: string; entries: Envelope[] }>;
    customization: { layout: { sections: Record<string, string[]> } };
  };

const render = async (envelope: Envelope) => new JSDOM(
  await renderPublicResume(
    decode(envelope) as PublicRenderResumeRequest,
    VERSIONS,
  ),
).window.document;

describe('resume render envelope entry points', () => {
  it.each(corpus.cases.map((entry) => [entry.name, entry] as const))(
    'decodes the corpus envelope %s unchanged',
    (_name, entry) => {
      expect(decode(entry.envelope)).toEqual(entry.envelope);
    },
  );

  it('accepts an envelope without entry points', () => {
    const envelope = linked((value) => {
      delete value.projectLinks;
      delete value.projectsUrl;
    });
    expect(decode(envelope)).toEqual(envelope);
  });

  it('accepts a link to a sidebar project entry', () => {
    const envelope = linked((value) => {
      const sections = document(value).customization.layout.sections;
      sections.main = sections.main!.filter((key) => key !== 'project');
      sections.sidebar = [...sections.sidebar!, 'project'];
    });
    expect(decode(envelope)).toEqual(envelope);
  });

  it('requires the projects address of this origin and slug', () => {
    for (const projectsUrl of [
      'https://other.example/do-hoang-nam/projects',
      'http://aboutme.example/do-hoang-nam/projects',
      'https://aboutme.example/another-slug/projects',
      'https://aboutme.example/do-hoang-nam/projects/',
      'https://aboutme.example/do-hoang-nam/project',
      'https://aboutme.example/do-hoang-nam',
      '',
      7,
      null,
    ]) {
      rejects(linked((value) => {
        value.projectsUrl = projectsUrl;
      }));
    }
  });

  it('rejects links without the projects address', () => {
    rejects(linked((value) => {
      delete value.projectsUrl;
    }));
  });

  it('rejects an empty or malformed link list', () => {
    for (const projectLinks of [[], {}, 'x', null, [null], [[FIRST]]]) {
      rejects(linked((value) => {
        value.projectLinks = projectLinks;
      }));
    }
  });

  it('rejects a missing or extra link key', () => {
    rejects(linked((value) => {
      delete links(value)[0]!.url;
    }));
    rejects(linked((value) => {
      delete links(value)[0]!.entryId;
    }));
    rejects(linked((value) => {
      (links(value)[0] as Envelope).projectSlug = 'vn-address-kit';
    }));
  });

  it('requires each link to be a project page under the projects address',
    () => {
      for (const url of [
        `${PROJECTS_URL}`,
        `${PROJECTS_URL}/`,
        `${PROJECTS_URL}/Atlas`,
        `${PROJECTS_URL}/a`,
        `${PROJECTS_URL}/-atlas`,
        `${PROJECTS_URL}/atlas/extra`,
        `${PROJECTS_URL}/atlas?x=1`,
        `${PROJECTS_URL}/atlas#top`,
        `${PROJECTS_URL}/at%6Cas`,
        'https://other.example/do-hoang-nam/projects/atlas',
        'javascript:alert(1)',
        7,
      ]) {
        rejects(linked((value) => {
          links(value)[1]!.url = url;
        }));
      }
    });

  it('requires each link to name one visible project entry', () => {
    for (const entryId of [
      '', 7, WORK, '00000000-0000-4000-8000-000000000000', FIRST,
    ]) {
      rejects(linked((value) => {
        links(value)[1]!.entryId = entryId;
      }));
    }
    rejects(linked((value) => {
      const sections = document(value).customization.layout.sections;
      sections.main = sections.main!.filter((key) => key !== 'project');
    }));
  });

  it('accepts a link to an entry without a title', () => {
    for (const title of ['', undefined]) {
      const envelope = linked((value) => {
        const entry = document(value).content.project!.entries[1]!;
        if (title === undefined) delete entry.title;
        else entry.title = title;
      });
      expect(decode(envelope)).toEqual(envelope);
    }
  });

  it('rejects a link context inside the public resume', () => {
    rejects(linked((value) => {
      (value.publicResume as Envelope).projectLinks = [];
    }));
  });
});

describe('resume render request size', () => {
  it('decodes a request of exactly 557,056 bytes and refuses one more', () => {
    const source = JSON.stringify(envelopeNamed('en-two-linked-entries'));
    const sized = (bytes: number) => `${source.slice(0, -1)}${
      ' '.repeat(bytes - Buffer.byteLength(source, 'utf8'))}}`;
    expect(Buffer.byteLength(sized(557_056), 'utf8')).toBe(557_056);
    expect(decodePublicRenderEnvelope(sized(557_056)).mode)
      .toBe('continuous');
    expect(() => decodePublicRenderEnvelope(sized(557_057)))
      .toThrow(PUBLIC_RENDER_FAILURE);
  });
});

describe('resume page entry points', () => {
  it('links each mapped title once with exactly its page', async () => {
    const page = await render(envelopeNamed('en-two-linked-entries'));
    const anchors = [...page.querySelectorAll('a[data-project-entry]')];
    expect(anchors.map((anchor) => [
      anchor.getAttribute('data-project-entry'),
      anchor.getAttribute('href'),
      anchor.parentElement?.tagName,
      anchor.textContent,
    ])).toEqual([
      [FIRST, `${PROJECTS_URL}/vn-address-kit`, 'STRONG', 'vn-address-kit'],
      [SECOND, `${PROJECTS_URL}/nextjs-perf-checklist`, 'STRONG',
        'Next.js performance checklist'],
    ]);
    expect(page.querySelector(
      'a[href="https://example.com/do-hoang-nam/vn-address-kit"]',
    )).toBeNull();
  });

  it('puts one Projects link left of Download PDF', async () => {
    const page = await render(envelopeNamed('en-two-linked-entries'));
    const projects = page.querySelectorAll('a.public-projects');
    expect(projects).toHaveLength(1);
    expect(Object.fromEntries([...projects[0]!.attributes]
      .map(({ name, value }) => [name, value]))).toEqual({
      class: 'public-projects', href: PROJECTS_URL,
    });
    expect(projects[0]!.textContent).toBe('Projects');
    const actions = [...page.querySelectorAll('.public-toolbar a')]
      .map((anchor) => anchor.className);
    expect(actions).toEqual([
      'public-credit', 'public-projects', 'public-download',
    ]);
    expect(page.querySelector('.public-toolbar')?.textContent)
      .toBe('Built with aboutme.vnProjectsDownload PDF');
  });

  it('labels the Projects link in Vietnamese without Download PDF',
    async () => {
      const page = await render(envelopeNamed('vi-linked-entries'));
      expect(page.querySelector('a.public-projects')?.textContent)
        .toBe('Dự án');
      expect(page.querySelector('a.public-download')).toBeNull();
      expect(page.querySelector('.public-toolbar')?.textContent)
        .toBe('Tạo bằng aboutme.vnDự án');
    });

  it('links only the visible entry of a hidden one', async () => {
    const page = await render(envelopeNamed('en-hidden-linked-entry'));
    expect([...page.querySelectorAll('a[data-project-entry]')]
      .map((anchor) => anchor.getAttribute('data-project-entry')))
      .toEqual([FIRST]);
    expect(page.querySelectorAll('a.public-projects')).toHaveLength(1);
  });

  it('gives an untitled linked entry no anchor and keeps the bar link',
    async () => {
      const page = await render(linked((value) => {
        delete document(value).content.project!.entries[1]!.title;
      }));
      expect([...page.querySelectorAll('a[data-project-entry]')]
        .map((anchor) => anchor.getAttribute('data-project-entry')))
        .toEqual([FIRST]);
      expect(page.querySelectorAll('a.public-projects')).toHaveLength(1);
    });

  it('keeps the bar link when the only linked entry has no title',
    async () => {
      const envelope = envelopeNamed('en-hidden-linked-entry');
      delete document(envelope).content.project!.entries[0]!.title;
      const page = await render(envelope);
      expect(page.querySelector('a[data-project-entry]')).toBeNull();
      expect(page.querySelector('a.public-projects')?.getAttribute('href'))
        .toBe(PROJECTS_URL);
    });

  it('shows no link and no bar link without an available project',
    async () => {
      const page = await render(envelopeNamed('en-no-available-project'));
      expect(page.querySelector('a[data-project-entry]')).toBeNull();
      expect(page.querySelector('a.public-projects')).toBeNull();
      expect(page.querySelector(
        'a[href="https://example.com/do-hoang-nam/vn-address-kit"]',
      )).not.toBeNull();
    });

  it('marks the root whenever the request carries the projects address',
    async () => {
      for (const entry of corpus.cases) {
        const page = await render(entry.envelope);
        const root = page.querySelector('main#public-resume')!;
        expect(root.getAttribute('data-project-links')).toBe('');
        expect([...root.attributes].map(({ name }) => name)).toEqual([
          'id', 'data-project-links', 'data-revision',
        ]);
      }
      const page = await render(linked((value) => {
        delete value.projectLinks;
        delete value.projectsUrl;
      }));
      expect(page.querySelector('[data-project-links]')).toBeNull();
      expect(page.querySelector('a[data-project-entry]')).toBeNull();
      expect(page.querySelector('a.public-projects')).toBeNull();
    });

  it('shows no project count', async () => {
    for (const entry of corpus.cases) {
      const page = await render(entry.envelope);
      const bar = page.querySelector('.public-toolbar')!.textContent!;
      expect(bar).not.toMatch(/\d/u);
    }
  });
});

describe('resume page HTML parity with the shared corpus', () => {
  it.each(corpus.cases.map((entry) => [entry.name, entry] as const))(
    'renders exactly the corpus HTML for %s',
    async (_name, entry) => {
      expect(corpus.styleVersion).toBe(VERSIONS.style);
      const html = await renderPublicResume(
        decode(entry.envelope) as PublicRenderResumeRequest,
        VERSIONS,
      );
      expect(html).toBe(entry.html);
    },
  );
});
