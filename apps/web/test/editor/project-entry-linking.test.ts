import type { Section } from '@aboutme/schema';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { defineComponent, h } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import SectionPanel from '../../app/components/editor/forms/SectionPanel.vue';
import type { Project } from '../../app/components/projects/projectDocument';
import type { EntryLocation } from '../../app/editor/projectLinks';
import { parseRevision } from '../../app/editor/revision';
import type { AcceptedResume } from '../../app/editor/types';
import {
  failed,
  fakeApi,
  fakeAuth,
  ok,
  project,
  resumeSummary,
} from '../projects/fixtures';
import { setSiteLocale } from '../support/locale';
import { acceptedFixture } from './fixture';
import { openEditor } from './held-write-editor';

// An unlinked project entry's Link to a project and Save to my projects, and
// the question From your projects asks when the resume already has the
// project as an entry (docs/design/ui/projects.md "In the resume editor"),
// through the real store, write queue and coordinator; only HTTP is faked.

const fake = vi.hoisted(() => ({
  api: null as unknown,
  resumes: null as unknown,
  auth: null as unknown,
}));
vi.mock('../../app/composables/projectsApi', async (importOriginal) => ({
  ...await importOriginal<
    typeof import('../../app/composables/projectsApi')>(),
  createProjectsApi: () => fake.api,
}));
vi.mock('../../app/editor/resumeApi', async (importOriginal) => ({
  ...await importOriginal<typeof import('../../app/editor/resumeApi')>(),
  createResumeApi: () => fake.resumes,
}));
vi.mock('../../app/composables/useAuth', async (importOriginal) => ({
  ...await importOriginal<typeof import('../../app/composables/useAuth')>(),
  useAuth: () => fake.auth,
}));

const SHOP = '0190f000-0000-7000-8000-0000000000c1';
const OPS = '0190f000-0000-7000-8000-0000000000c2';
const DATA = '0190f000-0000-7000-8000-0000000000c3';
const CREATED = '0190f000-0000-7000-8000-0000000000aa';
const ENTRY = '0190f000-0000-7000-8000-0000000000e1';
const LINKED = '0190f000-0000-7000-8000-0000000000e2';

type Entry = Section['entries'][number];

const written = (overrides: Partial<Entry> = {}): Entry => ({
  id: ENTRY,
  title: 'Old shop',
  subtitle: 'Lead',
  link: 'https://github.com/ada/old-shop',
  dates: { start: { y: 2023 }, end: { y: 2024, m: 6 }, present: false },
  description: '<p>Built &amp; ran it.</p><ul><li>Go</li></ul>',
  ...overrides,
} as Entry);
const linkedOps = { id: LINKED, title: 'Ops', projectId: OPS } as Entry;

const shop = (overrides: Partial<Project> = {}) => project({
  id: SHOP, text: { en: { name: 'Shop site' } }, ...overrides,
});
const ops = () => project({ id: OPS, text: { en: { name: 'Ops tool' } } });
const data = () => project({ id: DATA, text: { en: { name: 'Data kit' } } });

function resume(
  entries: readonly Entry[],
  options: { readonly live?: boolean } = {},
): AcceptedResume {
  const base = acceptedFixture();
  return acceptedFixture({
    metadata: { ...base.metadata, live: options.live ?? false },
    document: {
      ...base.document,
      content: {
        projects: {
          sectionType: 'project',
          displayName: 'Projects',
          entries: [...entries],
        } as Section,
      },
      customization: {
        ...base.document.customization,
        layout: {
          ...base.document.customization.layout,
          sections: { main: ['projects'], sidebar: [] },
        },
      },
    },
    revision: parseRevision('1'),
  });
}

/** This resume and another, with whether each one is published. */
function accountResumes(thisLive: boolean, otherLive = true) {
  return {
    kind: 'ready',
    items: [
      resumeSummary('resume-1', '1', 'Fixture', thisLive
        ? { live: true, slug: 'ada' }
        : {}),
      resumeSummary('other', '1', 'Other', otherLive
        ? { live: true, slug: 'ada-2' }
        : {}),
    ],
  };
}

let api: ReturnType<typeof fakeApi>;
let resumeList: ReturnType<typeof vi.fn>;
const mounted: { unmount: () => void }[] = [];

/** Lets the queue's zero-delay timer, the fake server and Vue settle. */
async function settle(): Promise<void> {
  for (let round = 0; round < 6; round += 1) {
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
}

async function mountEditor(initial: AcceptedResume) {
  const context = openEditor(initial);
  const selected: EntryLocation[] = [];
  const Host = defineComponent({
    setup: () => () => h(SectionPanel, {
      actions: context.actions,
      sectionKey: 'projects',
      section: context.record().current.document.content.projects!,
      onSelectEntry: (location: EntryLocation) => selected.push(location),
    }),
  });
  const wrapper = await mountSuspended(Host, { attachTo: document.body });
  mounted.push(wrapper);
  await settle();
  return { context, wrapper, selected };
}

function inDocument(selector: string): HTMLElement {
  const element = document.body.querySelector<HTMLElement>(selector);
  if (element === null) throw new Error(`Missing ${selector}`);
  return element;
}

const card = (entryId = ENTRY) => `[data-entry-id="${entryId}"]`;
const asked = () => document.body.querySelector('[role="alertdialog"]');

async function openLinkMenu(): Promise<string[]> {
  inDocument(`${card()} [data-action="link-entry-project"]`).click();
  await flushPromises();
  return [...document.body.querySelectorAll('[data-entry-project-option]')]
    .map((option) => option.textContent?.trim() ?? '');
}

async function chooseFromProjects(projectId: string): Promise<void> {
  inDocument('[data-action="add-entry"]').click();
  await flushPromises();
  inDocument('[data-action="from-projects"]').click();
  await flushPromises();
  inDocument(`[data-project-option="${projectId}"]`).click();
  await settle();
}

const links = (context: ReturnType<typeof openEditor>) => context.server.sent
  .filter(({ attempt }) => attempt.operation === 'linkResumeProject')
  .map(({ attempt }) => attempt.payload.kind === 'json'
    ? JSON.parse(attempt.payload.utf8)
    : null);

const entryStatus = () =>
  inDocument(`${card()} [data-entry-project-status]`);

beforeEach(() => {
  setSiteLocale('en');
  api = fakeApi();
  fake.api = api;
  fake.auth = fakeAuth().auth;
  resumeList = vi.fn().mockResolvedValue(accountResumes(false));
  fake.resumes = { list: resumeList };
});

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  setSiteLocale(undefined);
});

describe('Link to a project', () => {
  it('lists the library projects this resume does not show yet', async () => {
    api.list.mockResolvedValue(ok([shop(), ops(), data()]));
    await mountEditor(resume([written(), linkedOps]));

    expect(await openLinkMenu()).toEqual(['Shop site', 'Data kit']);
    expect(document.body.querySelector(
      `${card(LINKED)} [data-action="link-entry-project"]`,
    )).toBeNull();
  });

  it('links the entry through the write queue, keeps every field and shows '
    + 'the linked note', async () => {
    api.list.mockResolvedValue(ok([shop(), ops()]));
    api.read.mockResolvedValue(ok(shop()));
    const { context, selected } = await mountEditor(resume([written()]));
    await openLinkMenu();

    inDocument(`[data-entry-project-option="${SHOP}"]`).click();
    await settle();

    expect(links(context)).toEqual([{ projectId: SHOP, entryId: ENTRY }]);
    expect(context.server.stored.document.content.projects?.entries)
      .toEqual([{ ...written(), projectId: SHOP }]);
    expect(context.store.saveStateFor(context.id)).toBe('saved');
    expect(inDocument(`${card()} [data-project-note]`).textContent)
      .toContain('This entry comes from the project Shop site.');
    expect(document.body.querySelector(
      `${card()} [data-action="link-entry-project"]`,
    )).toBeNull();
    expect(selected).toEqual([{ sectionKey: 'projects', entryId: ENTRY }]);
  });

  it('says so when the entry could not be linked, and keeps it unlinked',
    async () => {
      api.list.mockResolvedValue(ok([shop()]));
      api.read.mockResolvedValue(ok(shop()));
      const { context } = await mountEditor(resume([written()]));
      context.server.failNextWrite({
        kind: 'rejected', code: 'project_not_found',
      });
      await openLinkMenu();

      inDocument(`[data-entry-project-option="${SHOP}"]`).click();
      await settle();

      expect(entryStatus().getAttribute('role')).toBe('status');
      expect(entryStatus().textContent?.trim())
        .toBe('The entry could not be linked to the project. Try again.');
      expect(document.body.querySelector(`${card()} [data-project-note]`))
        .toBeNull();
      expect(links(context)).toHaveLength(1);
    });

  it('asks first when another published resume shows the project; Cancel '
    + 'sends nothing and Add anyway sends the link once', async () => {
    resumeList.mockResolvedValue(accountResumes(true));
    api.list.mockResolvedValue(ok([shop()]));
    api.read.mockResolvedValue(ok(shop({ linkedResumeIds: ['other'] })));
    const { context } = await mountEditor(resume([written()], { live: true }));

    await openLinkMenu();
    inDocument(`[data-entry-project-option="${SHOP}"]`).click();
    await settle();
    expect(asked()?.textContent)
      .toContain('This project is already shown on another public resume.');
    inDocument('[data-action="cancel-project-privacy"]').click();
    await settle();
    expect(links(context)).toEqual([]);

    await openLinkMenu();
    inDocument(`[data-entry-project-option="${SHOP}"]`).click();
    await settle();
    inDocument('[data-action="confirm-project-privacy"]').click();
    await settle();

    expect(links(context)).toEqual([{ projectId: SHOP, entryId: ENTRY }]);
    expect(resumeList).toHaveBeenCalledTimes(2);
    expect(api.read).toHaveBeenCalledTimes(2);
  });

  it('decides once: a link resent after an unknown outcome reads and asks '
    + 'nothing again', async () => {
    resumeList.mockResolvedValue(accountResumes(true));
    api.list.mockResolvedValue(ok([shop()]));
    api.read.mockResolvedValue(ok(shop()));
    const { context } = await mountEditor(resume([written()], { live: true }));
    context.server.loseNextResponse({ applied: true });
    await openLinkMenu();

    inDocument(`[data-entry-project-option="${SHOP}"]`).click();
    await settle();
    api.read.mockResolvedValue(ok(shop({ linkedResumeIds: ['other'] })));
    await new Promise((resolve) => setTimeout(resolve, 400));
    await settle();

    const keys = context.server.sent.map(({ attempt }) =>
      attempt.idempotencyKey);
    expect(context.server.sent.map(({ result }) => result))
      .toEqual(['unknown', 'complete']);
    expect(new Set(keys).size).toBe(1);
    expect(asked()).toBeNull();
    expect([resumeList.mock.calls.length, api.read.mock.calls.length])
      .toEqual([1, 1]);
    expect(context.record().current.document.content.projects?.entries[0])
      .toEqual({ ...written(), projectId: SHOP });
  });
});

describe('Save to my projects', () => {
  const created = () => project({
    id: CREATED, slug: 'old-shop', text: { en: { name: 'Old shop' } },
  });

  it('creates the project from the entry, links the entry, then offers '
    + 'Open project', async () => {
    api.list.mockResolvedValueOnce(ok([shop()]))
      .mockResolvedValue(ok([shop(), created()]));
    api.create.mockResolvedValue(ok(created()));
    const { context, selected } = await mountEditor(resume([written()]));

    inDocument(`${card()} [data-action="save-entry-project"]`).click();
    await settle();

    expect(api.create).toHaveBeenCalledOnce();
    const [body, key, csrf] = api.create.mock.calls[0]!;
    expect(body).toEqual({
      defaultLanguage: 'en',
      dates: { start: { y: 2023 }, end: { y: 2024, m: 6 }, present: false },
      repositoryUrl: 'https://github.com/ada/old-shop',
      text: {
        en: { name: 'Old shop', role: 'Lead', summary: 'Built & ran it. Go' },
      },
    });
    expect([typeof key, csrf]).toEqual(['string', 'csrf-1']);
    expect(links(context)).toEqual([{ projectId: CREATED, entryId: ENTRY }]);
    expect(context.server.stored.document.content.projects?.entries)
      .toEqual([{ ...written(), projectId: CREATED }]);
    const note = inDocument(`${card()} [data-project-note]`);
    expect(note.textContent).toContain('This entry comes from the project '
      + 'Old shop.');
    expect(note.querySelector('[data-action="open-project"]')
      ?.getAttribute('href')).toBe(`/app/projects/${CREATED}`);
    expect(entryStatus().textContent?.trim()).toBe('Saved to your projects.');
    expect(selected).toEqual([{ sectionKey: 'projects', entryId: ENTRY }]);
  });

  it('keeps the project when the link fails afterwards, says so, and '
    + 'retries nothing', async () => {
    api.list.mockResolvedValue(ok([]));
    api.create.mockResolvedValue(ok(created()));
    const { context } = await mountEditor(resume([written()]));
    context.server.failNextWrite({ kind: 'rejected', code: 'entry_not_found' });

    inDocument(`${card()} [data-action="save-entry-project"]`).click();
    await settle();
    await new Promise((resolve) => setTimeout(resolve, 300));
    await settle();

    expect(api.create).toHaveBeenCalledOnce();
    expect(api.remove).not.toHaveBeenCalled();
    expect(links(context)).toHaveLength(1);
    expect(entryStatus().textContent).toContain('The project was saved, but '
      + 'this entry could not be linked to it. Use Link to a project.');
    expect(inDocument(`${card()} [data-action="open-saved-project"]`)
      .getAttribute('href')).toBe(`/app/projects/${CREATED}`);
    expect(context.record().current.document.content.projects?.entries)
      .toEqual([written()]);
  });

  it('is hidden at 30 projects, with the limit line in its place',
    async () => {
      const library = Array.from({ length: 30 }, (_, index) => project({
        id: `0190f000-0000-7000-8000-0000000001${String(index).padStart(2,
          '0')}`,
      }));
      api.list.mockResolvedValue(ok(library));
      await mountEditor(resume([written()]));

      expect(document.body.querySelector(
        `${card()} [data-action="save-entry-project"]`,
      )).toBeNull();
      expect(inDocument(`${card()} [data-project-limit]`).textContent?.trim())
        .toBe('You have reached the limit of 30 projects.');
    });

  it('is offered below the limit', async () => {
    api.list.mockResolvedValue(ok(Array.from({ length: 29 }, (_, index) =>
      project({ id: `p-${index}` }))));
    await mountEditor(resume([written()]));

    expect(document.body.querySelector(
      `${card()} [data-action="save-entry-project"]`,
    )).not.toBeNull();
    expect(document.body.querySelector(`${card()} [data-project-limit]`))
      .toBeNull();
  });

  it('asks first when the account resumes cannot be read, and Cancel '
    + 'creates nothing', async () => {
    api.list.mockResolvedValue(ok([]));
    resumeList.mockResolvedValue({ kind: 'failed', reason: 'network' });
    const { context } = await mountEditor(resume([written()], { live: true }));

    inDocument(`${card()} [data-action="save-entry-project"]`).click();
    await settle();
    expect(asked()).not.toBeNull();
    inDocument('[data-action="cancel-project-privacy"]').click();
    await settle();

    expect(api.create).not.toHaveBeenCalled();
    expect(links(context)).toEqual([]);
  });

  it('does not ask on a published resume, since a new project shows on no '
    + 'other', async () => {
    api.list.mockResolvedValue(ok([]));
    api.create.mockResolvedValue(ok(created()));
    resumeList.mockResolvedValue(accountResumes(true));
    const { context } = await mountEditor(resume([written()], { live: true }));

    inDocument(`${card()} [data-action="save-entry-project"]`).click();
    await settle();

    expect(asked()).toBeNull();
    expect(api.read).not.toHaveBeenCalled();
    expect(links(context)).toEqual([{ projectId: CREATED, entryId: ENTRY }]);
  });

  it('says to try again when the create fails, and sends the same request '
    + 'with the same key', async () => {
    api.list.mockResolvedValue(ok([]));
    api.create.mockResolvedValueOnce(failed('unavailable'))
      .mockResolvedValueOnce(ok(created()));
    const { context } = await mountEditor(resume([written()]));

    inDocument(`${card()} [data-action="save-entry-project"]`).click();
    await settle();
    expect(entryStatus().textContent?.trim())
      .toBe('Could not save to your projects. Try again.');
    expect(links(context)).toEqual([]);
    inDocument(`${card()} [data-action="save-entry-project"]`).click();
    await settle();

    const [first, second] = api.create.mock.calls;
    expect(second![1]).toBe(first![1]);
    expect(second![0]).toEqual(first![0]);
    expect(links(context)).toEqual([{ projectId: CREATED, entryId: ENTRY }]);
  });

  it('needs a title within the project limits and sends nothing without one',
    async () => {
      api.list.mockResolvedValue(ok([]));
      await mountEditor(resume([written({ title: '  ' })]));

      inDocument(`${card()} [data-action="save-entry-project"]`).click();
      await settle();

      expect(entryStatus().textContent?.trim()).toBe('To save it as a '
        + 'project, the entry needs a title, and a title and subtitle of at '
        + 'most 120 characters each.');
      expect(api.create).not.toHaveBeenCalled();
      expect(resumeList).not.toHaveBeenCalled();
    });
});

describe('From your projects for a project the resume has as an entry', () => {
  const question = (title: string) => `This resume already has an entry `
    + `“${title}”. Link that entry to the project, or add a new one?`;

  it('asks when an unlinked entry has the same title, ignoring case and '
    + 'surrounding space, and Link that entry, the default, links it',
  async () => {
    api.list.mockResolvedValue(ok([shop()]));
    api.read.mockResolvedValue(ok(shop()));
    const { context, selected } = await mountEditor(resume([
      written({ title: '  shop SITE ', link: '' }),
    ]));

    await chooseFromProjects(SHOP);
    const dialog = inDocument('[role="alertdialog"]');
    expect(dialog.textContent).toContain(question('shop SITE'));
    expect(document.activeElement?.getAttribute('data-action'))
      .toBe('link-duplicate-entry');
    expect(resumeList).not.toHaveBeenCalled();
    inDocument('[data-action="link-duplicate-entry"]').click();
    await settle();

    expect(links(context)).toEqual([{ projectId: SHOP, entryId: ENTRY }]);
    expect(context.server.stored.document.content.projects?.entries)
      .toEqual([{ ...written({ title: '  shop SITE ', link: '' }),
        projectId: SHOP }]);
    expect(selected).toEqual([{ sectionKey: 'projects', entryId: ENTRY }]);
  });

  it('asks when an unlinked entry has the same link, and Add a new entry '
    + 'adds one as before', async () => {
    api.list.mockResolvedValue(ok([shop({
      demoUrl: 'https://shop.example', text: { en: { name: 'Shop site' } },
    })]));
    api.read.mockResolvedValue(ok(shop()));
    const { context } = await mountEditor(resume([
      written({ title: 'My store', link: 'https://shop.example' }),
    ]));

    await chooseFromProjects(SHOP);
    expect(inDocument('[role="alertdialog"]').textContent)
      .toContain(question('My store'));
    inDocument('[data-action="add-duplicate-entry"]').click();
    await settle();

    expect(links(context)).toEqual([{ projectId: SHOP }]);
  });

  it('adds without asking when no unlinked entry matches', async () => {
    api.list.mockResolvedValue(ok([shop({
      repositoryUrl: 'https://github.com/ada/shop',
    })]));
    api.read.mockResolvedValue(ok(shop()));
    const { context } = await mountEditor(resume([
      written({ title: 'Shop site 2', link: 'https://github.com/ada/other' }),
      { id: LINKED, title: 'Shop site', projectId: OPS } as Entry,
    ]));

    await chooseFromProjects(SHOP);

    expect(asked()).toBeNull();
    expect(links(context)).toEqual([{ projectId: SHOP }]);
  });

  it('sends nothing when the question is dismissed', async () => {
    api.list.mockResolvedValue(ok([shop()]));
    const { context } = await mountEditor(resume([
      written({ title: 'Shop site' }),
    ]));

    await chooseFromProjects(SHOP);
    document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Escape', bubbles: true,
    }));
    await settle();

    expect(asked()).toBeNull();
    expect(links(context)).toEqual([]);
    expect(resumeList).not.toHaveBeenCalled();
  });

  it.each([
    ['Link that entry', 'link-duplicate-entry', { entryId: ENTRY }],
    ['Add a new entry', 'add-duplicate-entry', {}],
  ])('asks about privacy after %s when another published resume shows the '
    + 'project', async (_name, action, extra) => {
    resumeList.mockResolvedValue(accountResumes(true));
    api.list.mockResolvedValue(ok([shop()]));
    api.read.mockResolvedValue(ok(shop({ linkedResumeIds: ['other'] })));
    const { context } = await mountEditor(resume([
      written({ title: 'Shop site' }),
    ], { live: true }));

    await chooseFromProjects(SHOP);
    inDocument(`[data-action="${action}"]`).click();
    await settle();
    expect(asked()?.textContent)
      .toContain('A reader of both can tell they belong to the same person.');
    expect(links(context)).toEqual([]);
    inDocument('[data-action="confirm-project-privacy"]').click();
    await settle();

    expect(links(context)).toEqual([{ projectId: SHOP, ...extra }]);
  });
});
