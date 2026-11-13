// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import {
  decodePublicRenderEnvelope,
  PUBLIC_RENDER_FAILURE,
} from '../../server/utils/public-render/envelope';

// The shared corpus Go encodes from its request types
// (docs/design/projects-contract.md "Render envelope").
const corpus = JSON.parse(readFileSync(
  resolve(
    process.cwd(),
    '../server/internal/publicapi/testdata/project-render.json',
  ),
  'utf8',
)) as { cases: { name: string; envelope: Record<string, unknown> }[] };

type Envelope = Record<string, unknown>;

const envelopeNamed = (name: string): Envelope =>
  structuredClone(corpus.cases.find((entry) => entry.name === name)!.envelope);

const list = () => envelopeNamed('list-vi-two-projects');
const detail = () => envelopeNamed('project-en-every-field');
const nameOnly = () => envelopeNamed('project-en-name-only');

const decode = (value: unknown) =>
  decodePublicRenderEnvelope(JSON.stringify(value));

const rejects = (value: unknown) =>
  expect(() => decode(value)).toThrow(PUBLIC_RENDER_FAILURE);

const withResume = (envelope: Envelope, fields: Envelope): Envelope => ({
  ...envelope,
  resume: { ...envelope.resume as Envelope, ...fields },
});

const withProject = (envelope: Envelope, fields: Envelope): Envelope => ({
  ...envelope,
  project: { ...envelope.project as Envelope, ...fields },
});

const withRow = (envelope: Envelope, index: number, fields: Envelope) => {
  const projects = structuredClone(envelope.projects as Envelope[]);
  projects[index] = { ...projects[index], ...fields };
  return { ...envelope, projects };
};

const without = (value: Envelope, key: string): Envelope => {
  const { [key]: _omitted, ...rest } = value;
  return rest;
};

describe('project envelopes from the shared corpus', () => {
  it.each(corpus.cases.map((entry) => [entry.name, entry.envelope] as const))(
    'decodes %s unchanged',
    (_name, envelope) => {
      expect(decode(envelope)).toEqual(envelope);
    },
  );
});

describe('project list envelope', () => {
  it('rejects a missing or extra top-level key', () => {
    for (const key of Object.keys(list())) {
      rejects(without(list(), key));
    }
    rejects({ ...list(), extra: 'x' });
    rejects({ ...list(), project: detail().project });
    rejects({ ...list(), listUrl: 'https://aboutme.example/ada/projects' });
  });

  it('rejects a list with no project', () => {
    rejects({ ...list(), projects: [] });
  });

  it('rejects more than thirty projects', () => {
    const row = (list().projects as Envelope[])[1]!;
    const projects = Array.from({ length: 31 }, (_, index) => ({
      ...row,
      slug: `p${index}x`,
      url: `https://aboutme.example/ada/projects/p${index}x`,
    }));
    rejects({ ...list(), projects });
    expect(decode({ ...list(), projects: projects.slice(0, 30) }))
      .toBeDefined();
  });

  it('rejects the same project twice', () => {
    const rows = list().projects as Envelope[];
    rejects({ ...list(), projects: [rows[0], rows[0]] });
  });

  it('rejects a row key outside the contract', () => {
    rejects(withRow(list(), 1, { outcome: 'x' }));
    rejects(withRow(list(), 1, { description: '<p>x</p>' }));
    rejects(withRow(list(), 1, { repositoryUrl: 'https://x.example/' }));
    rejects({
      ...list(),
      projects: [without((list().projects as Envelope[])[1]!, 'technologies')],
    });
  });

  it('rejects more than six technologies or a wrong count of the rest', () => {
    const six = ['a', 'b', 'c', 'd', 'e', 'f'];
    rejects(withRow(list(), 1, { technologies: [...six, 'g'] }));
    rejects(withRow(list(), 1, { technologies: ['a'], moreTechnologies: 1 }));
    rejects(withRow(list(), 0, { moreTechnologies: 0 }));
    rejects(withRow(list(), 0, { moreTechnologies: 15 }));
    rejects(withRow(list(), 0, { moreTechnologies: 1.5 }));
    rejects(withRow(list(), 0, { moreTechnologies: '1' }));
    expect(decode(withRow(list(), 0, { moreTechnologies: 14 }))).toBeDefined();
  });

  it('rejects a row whose address is not under the resume', () => {
    rejects(withRow(list(), 0, {
      url: 'https://aboutme.example/bob/projects/may-phan-tich',
    }));
    rejects(withRow(list(), 0, {
      url: 'https://evil.example/ada/projects/may-phan-tich',
    }));
    rejects(withRow(list(), 0, {
      url: 'https://aboutme.example/ada/projects/other-slug',
    }));
  });

  it('rejects a page address other than the list address', () => {
    rejects({ ...list(), pageUrl: 'https://aboutme.example/ada' });
    rejects({ ...list(), pageUrl: 'https://aboutme.example/ada/projects/' });
  });
});

describe('project page envelope', () => {
  it('accepts an absent entry link and rejects an empty one', () => {
    expect(decode(without(detail(), 'entryLink'))).not.toHaveProperty(
      'entryLink',
    );
    rejects({ ...detail(), entryLink: '' });
  });

  it('rejects a missing or extra top-level key', () => {
    for (const key of Object.keys(detail())) {
      if (key === 'entryLink') continue;
      rejects(without(detail(), key));
    }
    rejects({ ...detail(), projects: list().projects });
    rejects({ ...detail(), extra: true });
  });

  it('rejects a project key outside the contract', () => {
    rejects(withProject(detail(), { moreTechnologies: 1 }));
    rejects(withProject(detail(), { id: 'x' }));
    rejects({ ...detail(), project: without(
      detail().project as Envelope, 'technologies') });
  });

  it('rejects an empty optional text', () => {
    for (const key of [
      'lang', 'role', 'summary', 'outcome', 'description', 'repositoryUrl',
      'demoUrl',
    ]) {
      rejects(withProject(detail(), { [key]: '' }));
    }
  });

  it('rejects a blank name and text longer than the library stores', () => {
    rejects(withProject(detail(), { name: '' }));
    rejects(withProject(detail(), { name: '   ' }));
    rejects(withProject(detail(), { name: 'ệ'.repeat(121) }));
    expect(decode(withProject(detail(), { name: 'ệ'.repeat(120) })))
      .toBeDefined();
    rejects(withProject(detail(), { role: 'a'.repeat(121) }));
    rejects(withProject(detail(), { summary: 'a'.repeat(301) }));
    rejects(withProject(detail(), { outcome: 'a'.repeat(601) }));
    rejects(withProject(detail(), {
      description: `<p>${'a'.repeat(16_378)}</p>`,
    }));
    rejects(withProject(detail(), { technologies: ['a'.repeat(41)] }));
    rejects(withProject(detail(), {
      technologies: Array.from({ length: 21 }, (_, index) => `t${index}`),
    }));
  });

  it('accepts only https links for the buttons', () => {
    for (const link of [
      'http://github.com/ada',
      'javascript:alert(1)',
      'HTTPS://github.com/ada',
      'https://',
      '//github.com/ada',
      'data:text/html,x',
      ` https://github.com/ada`,
      `https://${'a'.repeat(2041)}.com`,
    ]) {
      rejects(withProject(detail(), { repositoryUrl: link }));
      rejects(withProject(detail(), { demoUrl: link }));
      rejects({ ...detail(), entryLink: link });
    }
  });

  it('rejects an entry link equal to a button link', () => {
    const project = detail().project as Envelope;
    rejects({ ...detail(), entryLink: project.repositoryUrl });
    rejects({ ...detail(), entryLink: project.demoUrl });
  });

  it('rejects addresses that are not the resume, list or this project', () => {
    rejects({ ...detail(), listUrl: 'https://aboutme.example/ada/project' });
    rejects({ ...detail(), pageUrl: 'https://aboutme.example/ada/projects' });
    rejects(withProject(detail(), {
      url: 'https://aboutme.example/ada/projects/note-g',
    }));
    rejects(withResume(detail(), { url: 'https://aboutme.example/bob' }));
    rejects(withResume(detail(), { slug: 'bob1' }));
  });

  it('rejects a malformed project slug', () => {
    const slugs = ['A', 'a', 'ab-', '-ab', 'a--b', 'a_b', 'a'.repeat(41)];
    for (const slug of slugs) {
      const url = `https://aboutme.example/ada/projects/${slug}`;
      rejects({ ...withProject(detail(), { slug, url }), pageUrl: url });
    }
  });

  it('rejects malformed dates', () => {
    for (const dates of [
      { start: { y: 2020 }, end: null, present: false },
      { start: { y: 2020 }, end: { y: 2021 }, present: true },
      { start: { y: 1899 }, end: null, present: true },
      { start: { y: 2020, m: 13 }, end: null, present: true },
      { start: { y: 2020, m: 0 }, end: null, present: true },
      { start: { y: 2020.5 }, end: null, present: true },
      { start: { y: 2020 }, present: true },
      { start: { y: 2020 }, end: null, present: true, extra: 1 },
      { start: { y: 2020, d: 1 }, end: null, present: true },
      { start: { y: 2020 }, end: { y: 2101 }, present: false },
      null,
    ]) {
      rejects(withProject(detail(), { dates }));
    }
    expect(decode(withProject(detail(), {
      dates: { start: { y: 2020, m: 3 }, end: null, present: true },
    }))).toBeDefined();
  });

  it('rejects a project language that is not a primary subtag', () => {
    for (const lang of ['EN', 'en-US', 'e', 'abcdefghi', 7]) {
      rejects(withProject(detail(), { lang }));
    }
  });

  it('rejects a non-boolean other-projects flag', () => {
    rejects({ ...detail(), otherProjects: 'true' });
    rejects({ ...detail(), otherProjects: null });
  });

  it('accepts a page with no description', () => {
    const decoded = decode(nameOnly());
    expect(decoded).toEqual(nameOnly());
  });
});

describe('project resume and head fields', () => {
  it('rejects a resume key outside the contract', () => {
    for (const key of Object.keys(list().resume as Envelope)) {
      rejects({ ...list(), resume: without(list().resume as Envelope, key) });
    }
    rejects(withResume(list(), { photo: 'x' }));
  });

  it('rejects a copy language that does not follow the resume language', () => {
    rejects(withResume(list(), { uiLng: 'en' }));
    rejects(withResume(detail(), { uiLng: 'vi' }));
    rejects(withResume(detail(), { uiLng: 'fr' }));
    expect(decode(withResume(detail(), { lng: 'fr-CA' }))).toBeDefined();
    expect(decode(withResume(detail(), { lng: 'und' }))).toBeDefined();
    expect(decode(withResume(list(), { lng: 'VI' }))).toBeDefined();
    expect(decode(withResume(detail(), { lng: 'x-private' }))).toBeDefined();
  });

  it('rejects a resume language that is not a language tag', () => {
    for (const lng of ['', 'en US', 'en"x', '-en', 'en-', 'en--us',
      `en-${'a'.repeat(40)}`]) {
      rejects(withResume(detail(), { lng }));
    }
  });

  it('accepts only the closed scheme and date format words', () => {
    for (const colorScheme of ['light', 'dark', 'system']) {
      expect(decode(withResume(detail(), { colorScheme }))).toBeDefined();
    }
    rejects(withResume(detail(), { colorScheme: 'auto' }));
    for (const dateFormat of ['MM/YYYY', 'Mon YYYY', 'YYYY']) {
      expect(decode(withResume(detail(), { dateFormat }))).toBeDefined();
    }
    rejects(withResume(detail(), { dateFormat: 'DD/MM/YYYY' }));
  });

  it('rejects a blank owner name', () => {
    rejects(withResume(detail(), { ownerName: '' }));
    rejects(withResume(detail(), { ownerName: '  ' }));
  });

  it('accepts an empty preview description and rejects other gaps', () => {
    const preview = detail().preview as Envelope;
    expect(decode(nameOnly()).preview.description).toBe('');
    rejects({ ...detail(), preview: { ...preview, title: '' } });
    rejects({ ...detail(), preview: { ...preview, imageAlt: '' } });
    rejects({ ...detail(), preview: without(preview, 'description') });
    rejects({ ...detail(), preview: { ...preview, extra: 'x' } });
  });

  it('accepts only this resume\'s card image', () => {
    const preview = detail().preview as Envelope;
    expect(decode({ ...detail(), preview: without(preview, 'imageUrl') }))
      .toBeDefined();
    rejects({
      ...detail(),
      preview: {
        ...preview,
        imageUrl: 'https://aboutme.example/api/v1/public/resumes/bob1/og/'
          + '0123456789abcdef.png',
      },
    });
  });

  it('rejects a malformed head value', () => {
    rejects({ ...detail(), pageTitle: '' });
    rejects({ ...detail(), faviconHref: 'data:image/svg+xml,<svg>' });
    rejects({ ...detail(), canonicalOrigin: 'https://aboutme.example/' });
    rejects({ ...detail(), discoveryEnabled: 'false' });
  });
});

describe('project envelope values Go can and cannot send', () => {
  // U+1D49C is four bytes in UTF-8, so 300 of them are the longest summary
  // the library stores, and the project page's description.
  const longSummary = '𝒜'.repeat(300);
  const withPreview = (envelope: Envelope, fields: Envelope): Envelope => ({
    ...envelope,
    preview: { ...envelope.preview as Envelope, ...fields },
  });
  const describedBy = (fields: Envelope, description: string) =>
    withPreview(withProject(detail(), fields), { description });

  it('admits the longest summary uncut as the page description', () => {
    const envelope = describedBy({ summary: longSummary }, longSummary);
    expect(Buffer.byteLength(longSummary, 'utf8')).toBe(1200);
    expect(decode(envelope)).toEqual(envelope);
    expect(decode(withRow(list(), 0, { summary: longSummary })))
      .toBeDefined();
  });

  it('rejects a summary one character longer', () => {
    const summary = `${longSummary}𝒜`;
    rejects(describedBy({ summary }, summary));
    rejects(withRow(list(), 0, { summary }));
  });

  it('admits only the summary, else the outcome start, else nothing', () => {
    rejects(describedBy({}, 'Another description.'));
    rejects(describedBy({}, ''));
    const outcome = '𝒜'.repeat(200);
    const noSummary = (description: string) => {
      const envelope = describedBy({ outcome }, description);
      delete (envelope.project as Envelope).summary;
      return envelope;
    };
    expect(decode(noSummary('𝒜'.repeat(160))).preview.description)
      .toBe('𝒜'.repeat(160));
    rejects(noSummary('𝒜'.repeat(161)));
    rejects(noSummary(outcome));
    rejects(noSummary(''));
    const short = describedBy({ outcome: 'Shipped.' }, 'Shipped.');
    delete (short.project as Envelope).summary;
    expect(decode(short)).toBeDefined();
    const neither = describedBy({}, '');
    delete (neither.project as Envelope).summary;
    delete (neither.project as Envelope).outcome;
    expect(decode(neither)).toBeDefined();
    rejects(withPreview(list(), { description: 'Projects' }));
  });

  it('admits the longest uncut project title and no longer', () => {
    // 681 four-byte characters and one three-byte euro sign make 2,727
    // bytes; 683 ASCII letters make 683 code points.
    const titles = {
      accepted: [`${'𝒜'.repeat(681)}€`, 'a'.repeat(683)],
      rejected: ['𝒜'.repeat(682), 'a'.repeat(684)],
    };
    expect(Buffer.byteLength(titles.accepted[0]!, 'utf8')).toBe(2727);
    expect(Buffer.byteLength(titles.rejected[0]!, 'utf8')).toBe(2728);
    for (const title of titles.accepted) {
      expect(decode(withPreview(detail(), { title })).preview.title)
        .toBe(title);
      expect(decode(withPreview(list(), { title }))).toBeDefined();
    }
    for (const title of titles.rejected) {
      rejects(withPreview(detail(), { title }));
      rejects(withPreview(list(), { title }));
    }
  });

  it('keeps the resume and gate preview bounds', () => {
    const gate = {
      mode: 'gate', canonicalOrigin: 'https://resume.example', slug: 'ada1',
      lng: 'en', pageTitle: 'Ada', faviconHref: '',
      preview: { title: 'Ada', description: 'Ada', locale: '',
        imageAlt: 'Ada' },
      providers: [], message: 'none',
    };
    expect(decode(gate)).toBeDefined();
    const preview = (fields: Envelope) => ({
      ...gate, preview: { ...gate.preview, ...fields },
    });
    rejects(preview({ description: longSummary }));
    rejects(preview({ title: 'a'.repeat(2241) }));
  });

  it('rejects an entry link equal to a button link once normalized', () => {
    for (const entryLink of [
      'https://GitHub.com/ada/analytical-engine',
      'https://github.com:443/ada/analytical-engine',
      'https://GITHUB.COM:443/ada/analytical-engine',
      'https://ADA.example/demo',
      'https://ada.example:443/demo',
    ]) {
      rejects({ ...detail(), entryLink });
    }
    const bare = withProject(detail(), { demoUrl: 'https://ada.example' });
    for (const entryLink of [
      'https://ada.example/', 'https://Ada.Example:443/',
      'https://ada.example:/',
    ]) {
      rejects({ ...bare, entryLink });
    }
    const query = withProject(detail(), { demoUrl: 'https://ada.example?x' });
    rejects({ ...query, entryLink: 'https://ada.example/?x' });
  });

  it('accepts an entry link that differs after normalizing', () => {
    for (const entryLink of [
      'https://github.com/Ada/analytical-engine',
      'https://github.com:8443/ada/analytical-engine',
      'https://github.com/ada/analytical-engine/',
      'https://ada.example/demo?x',
      'https://user@github.com/ada/analytical-engine',
    ]) {
      expect(decode({ ...detail(), entryLink }).entryLink, entryLink)
        .toBe(entryLink);
    }
  });

  it('rejects a link with characters Go does not parse', () => {
    for (const link of [
      'https://github.com/ada\tx',
      'https://git\nhub.com/ada',
      'https://github.com/ada\r',
      'https://github.com/\u0000',
      'https://github.com/ada\u001b',
      'https://github.com/ada\u007f',
      'https://git hub.com/ada',
      'https://us er@github.com/ada',
      'https://us"er@github.com/ada',
      'https://github.com{x}/ada',
    ]) {
      rejects(withProject(detail(), { repositoryUrl: link }));
      rejects(withProject(detail(), { demoUrl: link }));
      rejects({ ...detail(), entryLink: link });
    }
  });

  it('accepts a space in a link path, as Go stores it', () => {
    const repositoryUrl = 'https://github.com/ada/my project';
    expect(decode(withProject(detail(), { repositoryUrl })))
      .toBeDefined();
  });

  it('rejects dates that end before they start', () => {
    for (const dates of [
      { start: { y: 2021 }, end: { y: 2020 }, present: false },
      { start: { y: 2020, m: 5 }, end: { y: 2020, m: 4 }, present: false },
      // An omitted month counts as January, as in Go.
      { start: { y: 2020, m: 5 }, end: { y: 2020 }, present: false },
    ]) {
      rejects(withProject(detail(), { dates }));
      rejects(withRow(list(), 0, { dates }));
    }
    for (const dates of [
      { start: { y: 2020 }, end: { y: 2020 }, present: false },
      { start: { y: 2020 }, end: { y: 2020, m: 1 }, present: false },
      { start: { y: 2020, m: 1 }, end: { y: 2020 }, present: false },
      { start: { y: 2020, m: 12 }, end: { y: 2021 }, present: false },
    ]) {
      expect(decode(withProject(detail(), { dates }))).toBeDefined();
    }
  });

  it('rejects NUL and lone surrogates in any text', () => {
    for (const bad of ['a\u0000b', 'a\ud800b', 'a\udc00b', '\ud83d']) {
      for (const key of ['name', 'role', 'outcome']) {
        rejects(withProject(detail(), { [key]: bad }));
      }
      rejects(describedBy({ summary: bad }, bad));
      rejects(withProject(detail(), { description: `<p>${bad}</p>` }));
      rejects(withProject(detail(), { technologies: [bad] }));
      rejects(withRow(list(), 0, { name: bad }));
      rejects(withRow(list(), 0, { technologies: [bad, 'b', 'c', 'd', 'e',
        'f'] }));
      rejects(withResume(detail(), { ownerName: bad }));
    }
    expect(decode(withProject(detail(), { name: 'Rocket 🚀' }))).toBeDefined();
  });
});

describe('gate envelope page', () => {
  const gate = {
    mode: 'gate',
    canonicalOrigin: 'https://resume.example',
    slug: 'ada1',
    lng: 'en',
    pageTitle: 'Ada — Resume',
    faviconHref: '',
    preview: {
      title: 'Ada Lovelace',
      description: 'Writes the first program.',
      locale: 'en_US',
      imageAlt: 'Ada Lovelace',
    },
    providers: ['google'],
    message: 'none',
  };

  it('accepts the list page and a project page', () => {
    for (const page of ['projects', 'projects/note-g', 'projects/a1-b2']) {
      expect(decode({ ...gate, page })).toEqual({ ...gate, page });
    }
  });

  it('rejects any other page', () => {
    for (const page of [
      '', 'projects/', 'project', '/projects', 'projects/Note', 'projects/a',
      'projects/note-g/x', 'projects/note-g?x=1', 'projects/note-g&slug=bob',
      `projects/${'a'.repeat(41)}`, 'projects/../admin', 7, null,
    ]) {
      rejects({ ...gate, page });
    }
  });
});
