import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import ProjectLinkNote
  from '../../app/components/editor/forms/entries/ProjectLinkNote.vue';
import ProjectEditorPage from '../../app/pages/app/projects/[id].vue';
import ProjectListPage from '../../app/pages/app/projects/index.vue';
import { editorFieldsCopy } from '../../app/i18n/editor-fields';
import type { ResumeListResult } from '../../app/editor/resumeApi';
import { setSiteLocale } from '../support/locale';
import { fakeApi, ok, project, resumeSummary } from './fixtures';
import { registerSignedIn } from './page-support';

const fake = vi.hoisted(() => ({
  api: null as unknown,
  linker: null as unknown,
}));
vi.mock('../../app/composables/projectsApi', async (importOriginal) => ({
  ...await importOriginal<
    typeof import('../../app/composables/projectsApi')>(),
  createProjectsApi: () => fake.api,
}));
vi.mock('../../app/components/projects/projectLinks', async (original) => ({
  ...await original<
    typeof import('../../app/components/projects/projectLinks')>(),
  createProjectLinker: () => fake.linker,
}));
mockNuxtImport('navigateTo', () => vi.fn());

let api: ReturnType<typeof fakeApi>;
const mounted: { unmount: () => void }[] = [];

beforeEach(() => {
  setSiteLocale('en');
  clearNuxtData();
  registerSignedIn();
  api = fakeApi();
  fake.api = api;
  fake.linker = {
    resumes: vi.fn<() => Promise<ResumeListResult>>().mockResolvedValue({
      kind: 'ready',
      items: [
        resumeSummary('resume-1', '1', 'Backend'),
        resumeSummary('resume-2', '1', 'Frontend'),
      ],
    }),
    link: vi.fn(),
  };
});

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

async function mountList() {
  const wrapper = await mountSuspended(ProjectListPage, {
    route: '/app/projects',
  });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

async function mountEditor() {
  api.read.mockResolvedValue(ok(project({ linkedResumeIds: ['resume-1'] })));
  const wrapper = await mountSuspended(ProjectEditorPage, {
    route: '/app/projects/project-1',
    attachTo: document.body,
  });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

function expectCard(classes: string[]): void {
  for (const name of [
    'rounded-[var(--radius-card)]', 'border', 'border-border', 'bg-card',
  ]) {
    expect(classes, name).toContain(name);
  }
  expect(classes).not.toContain('rounded-lg');
  expect(classes.filter((name) => /shadow/u.test(name))).toEqual([]);
}

function expectHairlineRows(classes: string[]): void {
  expect(classes).toEqual(expect.arrayContaining([
    'divide-y', 'divide-hairline',
  ]));
  expect(classes).not.toContain('divide-border');
}

function expectPageHeading(classes: string[]): void {
  for (const name of ['text-2xl', 'font-extrabold', 'tracking-[-0.02em]']) {
    expect(classes, name).toContain(name);
  }
  expect(classes).not.toContain('font-bold');
}

describe('project list identity', () => {
  it('sets the page heading at 32 px, weight 800', async () => {
    api.list.mockResolvedValue(ok([project()]));
    const wrapper = await mountList();
    expectPageHeading(wrapper.get('#projects-title').classes());
  });

  it('holds the rows in one card separated by hairlines', async () => {
    api.list.mockResolvedValue(ok([
      project({ id: 'one' }), project({ id: 'two' }),
    ]));
    const wrapper = await mountList();
    const list = wrapper.get('[data-testid="project-list"]');
    expect(list.element.tagName).toBe('UL');
    expectCard(list.classes());
    expectHairlineRows(list.classes());
  });

  it('draws technologies as mist tags and every chip at weight 600',
    async () => {
      api.list.mockResolvedValue(ok([project({
        technologies: ['Go', 'Vue', 'Postgres', 'Caddy'],
      })]));
      const wrapper = await mountList();
      const row = wrapper.get('[data-testid="project-row-project-1"]');
      const technologies = row.findAll(
        '[data-project-technologies] [data-slot="badge"]',
      );
      expect(technologies.map((chip) => chip.text()))
        .toEqual(['Go', 'Vue', 'Postgres', '+1']);
      for (const chip of technologies.slice(0, 3)) {
        for (const name of [
          'bg-surface-mist', 'border-transparent', 'text-foreground',
          'font-semibold', 'rounded-full',
        ]) {
          expect(chip.classes(), name).toContain(name);
        }
        expect(chip.classes()).not.toContain('bg-secondary');
      }
      const chips = row.findAll('[data-slot="badge"]');
      expect(chips.length).toBeGreaterThan(4);
      for (const chip of chips) {
        expect(chip.classes(), chip.text()).toContain('font-semibold');
        expect(chip.classes(), chip.text()).not.toContain('font-medium');
      }
    });

  it('makes New project a main pill with a plus icon', async () => {
    api.list.mockResolvedValue(ok([project()]));
    const wrapper = await mountList();
    const button = wrapper.get('[data-testid="new-project"]');
    expect(button.attributes('data-variant')).toBeUndefined();
    expect(button.classes()).toEqual(expect.arrayContaining([
      'bg-primary', 'text-primary-foreground', 'rounded-full',
    ]));
    expect(button.find('svg').exists()).toBe(true);
  });

  it('shows the empty list in one card', async () => {
    api.list.mockResolvedValue(ok([]));
    const wrapper = await mountList();
    expectCard(wrapper.get('[data-testid="projects-empty"]').classes());
  });

  it('loads in the same card with hairline rows', async () => {
    api.list.mockReturnValue(new Promise(() => {}));
    const wrapper = await mountList();
    const loading = wrapper.get('[data-testid="projects-loading"]');
    expectCard(loading.classes());
    expectHairlineRows(loading.classes());
  });
});

describe('project editor identity', () => {
  it('sets the name at 32 px and each section title at 20 px, weight 700',
    async () => {
      const wrapper = await mountEditor();
      expectPageHeading(wrapper.get('[data-testid="project-title"]').classes());
      for (const id of [
        'project-text-title',
        'project-details-title',
        'project-resumes-title',
        'project-address-title',
        'project-delete-title',
      ]) {
        const title = wrapper.get(`#${id}`);
        expect(title.classes(), id).toEqual(
          expect.arrayContaining(['text-lg', 'font-bold']),
        );
        expect(title.classes(), id).not.toContain('font-semibold');
      }
    });

  it('lists resumes in one card separated by hairlines', async () => {
    const wrapper = await mountEditor();
    const list = wrapper.get('[data-resume-row="resume-1"]')
      .element.parentElement!;
    const classes = [...list.classList];
    expectCard(classes);
    expectHairlineRows(classes);
  });

  it('keeps Delete project a destructive outline pill', async () => {
    const wrapper = await mountEditor();
    expect(wrapper.get('#project-delete').attributes('data-variant'))
      .toBe('destructive');
  });
});

describe('linked entry note', () => {
  it('sits on mist with the 10 px radius', async () => {
    const wrapper = await mountSuspended(ProjectLinkNote, {
      props: {
        projectId: 'project-1',
        name: 'Shop site',
        fallbackLanguage: null,
        refreshing: false,
        copy: editorFieldsCopy.en.projectLinks,
        cancelLabel: 'Cancel',
      },
    });
    mounted.push(wrapper);
    const note = wrapper.get('[data-project-note]');
    for (const name of [
      'bg-surface-mist', 'rounded-[var(--radius)]', 'text-foreground',
    ]) {
      expect(note.classes(), name).toContain(name);
    }
    expect(note.classes()).not.toContain('rounded-md');
  });
});
