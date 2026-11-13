// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { JSDOM } from 'jsdom';
import { describe, expect, it } from 'vitest';

import {
  decodePrintEnvelope,
  PRINT_FAILURE,
  type PrintEnvelope,
} from '../../server/utils/print/envelope';
import { renderPrintResume } from '../../server/workers/print/render';

type Envelope = Record<string, unknown>;

const corpus = JSON.parse(readFileSync(
  resolve(
    process.cwd(),
    '../server/internal/printsnapshot/testdata/print-envelopes.json',
  ),
  'utf8',
)) as { cases: { name: string; envelope: Envelope }[] };

const FIRST = '8c8b959a-0df4-59de-88d9-2ce01b88fb86';
const SECOND = '2ffd46bf-7cc3-5497-bd46-13fbe94653a4';
const WORK = 'd1aa2a3b-dfbe-5c4b-8a11-3f5289b0d3d9';
const PAGE = 'https://aboutme.example/do-hoang-nam/projects/vn-address-kit';
const SECOND_PAGE
  = 'https://aboutme.example/do-hoang-nam/projects/nextjs-perf-checklist';

const envelopeNamed = (name: string): Envelope =>
  structuredClone(corpus.cases.find((entry) => entry.name === name)!.envelope);

const linked = (change: (envelope: Envelope) => void = () => {}) => {
  const envelope = envelopeNamed('en-two-linked-entries');
  change(envelope);
  return envelope;
};

const decode = (envelope: Envelope) =>
  decodePrintEnvelope(JSON.stringify(envelope));

const rejects = (envelope: Envelope) =>
  expect(() => decode(envelope)).toThrow(PRINT_FAILURE);

const pages = (envelope: Envelope) =>
  envelope.projectLinks as Record<string, unknown>;

const layout = (envelope: Envelope) =>
  ((envelope.document as Envelope).customization as {
    layout: { sections: Record<string, string[]> };
  }).layout.sections;

const render = async (envelope: Envelope) => new JSDOM(
  await renderPrintResume(decode(envelope) as PrintEnvelope),
).window.document;

describe('print envelope project links', () => {
  it.each(corpus.cases.map((entry) => [entry.name, entry] as const))(
    'decodes the corpus envelope %s unchanged',
    (_name, entry) => {
      expect(decode(entry.envelope)).toEqual(entry.envelope);
    },
  );

  it('accepts a sidebar entry and an http origin', () => {
    const sidebar = linked((envelope) => {
      const sections = layout(envelope);
      sections.main = sections.main!.filter((key) => key !== 'project');
      sections.sidebar = [...sections.sidebar!, 'project'];
    });
    expect(decode(sidebar)).toEqual(sidebar);
    const http = linked((envelope) => {
      pages(envelope)[FIRST] = 'http://localhost:3000/do-hoang-nam/projects/vn';
    });
    expect(decode(http)).toEqual(http);
  });

  it('keeps the version rule', () => {
    rejects(linked((envelope) => {
      envelope.projectLinks = {};
    }));
    rejects(linked((envelope) => {
      delete envelope.projectLinks;
    }));
    rejects(linked((envelope) => {
      envelope.version = 1;
    }));
    rejects(linked((envelope) => {
      envelope.version = 3;
    }));
    for (const value of [null, [], [PAGE], 'x']) {
      rejects(linked((envelope) => {
        envelope.projectLinks = value;
      }));
    }
  });

  it('requires each key to be a visible project entry', () => {
    for (const entryId of ['missing', '', WORK]) {
      rejects(linked((envelope) => {
        pages(envelope)[entryId] = PAGE;
      }));
    }
    rejects(linked((envelope) => {
      const sections = layout(envelope);
      sections.main = sections.main!.filter((key) => key !== 'project');
    }));
  });

  it('requires each value to be a project page address', () => {
    const origin = 'https://aboutme.example';
    for (const value of [
      'ftp://aboutme.example/do-hoang-nam/projects/beacon',
      'HTTPS://aboutme.example/do-hoang-nam/projects/beacon',
      '/do-hoang-nam/projects/beacon',
      '',
      'https:///do-hoang-nam/projects/beacon',
      'https://user@aboutme.example/do-hoang-nam/projects/beacon',
      `${origin}/do-hoang-nam/projects/beacon?x=1`,
      `${origin}/do-hoang-nam/projects/beacon#top`,
      `${origin}/do-hoang-nam/projects/be acon`,
      'javascript:alert(1)',
      'http://outside.example/arbitrary',
      `${origin}/`,
      origin,
      `${origin}/do-hoang-nam/projects`,
      `${origin}/do-hoang-nam/projects/`,
      `${origin}/do-hoang-nam/projects/beacon/`,
      `${origin}/do-hoang-nam/projects/beacon/extra`,
      `${origin}/do-hoang-nam/project/beacon`,
      `${origin}/ada/projects/beacon`,
      `${origin}/login/projects/beacon`,
      `${origin}/do-hoang-nam/projects/Beacon`,
      `${origin}/do%2Dhoang-nam/projects/beacon`,
      `${origin}/do-hoang-nam/projects/../beacon`,
      `${origin}/do-hoang-nam/x/../projects/beacon`,
      `${origin}/do-hoang-nam/projects/beacón`,
      7,
    ]) {
      rejects(linked((envelope) => {
        pages(envelope)[SECOND] = value;
      }));
    }
  });
});

describe('print title links', () => {
  it('links each mapped title to its page and nothing else', async () => {
    const page = await render(linked());
    expect([...page.querySelectorAll('a[data-project-entry]')]
      .map((anchor) => [
        anchor.getAttribute('data-project-entry'),
        anchor.getAttribute('href'),
        anchor.parentElement?.tagName,
      ])).toEqual([
      [FIRST, PAGE, 'STRONG'],
      [SECOND, SECOND_PAGE, 'STRONG'],
    ]);
    expect(page.querySelector(
      'a[href="https://example.com/do-hoang-nam/vn-address-kit"]',
    )).toBeNull();
  });

  it('changes only the titles of a version 1 envelope', async () => {
    const withLinks = await render(linked());
    const without = await render(linked((envelope) => {
      envelope.version = 1;
      delete envelope.projectLinks;
    }));
    expect(without.querySelector('[data-project-entry]')).toBeNull();
    for (const page of [withLinks, without]) {
      for (const title of page.querySelectorAll('.entry-title')) {
        title.replaceChildren(title.textContent ?? '');
      }
    }
    expect(withLinks.documentElement.outerHTML)
      .toBe(without.documentElement.outerHTML);
  });

  it('renders the version 1 corpus envelope without links', async () => {
    const page = await render(envelopeNamed('en-no-available-project'));
    expect(page.querySelector('[data-project-entry]')).toBeNull();
  });
});
