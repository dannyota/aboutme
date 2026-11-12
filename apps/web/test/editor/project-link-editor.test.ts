import type { Section } from '@aboutme/schema';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { computed, ref, type Ref } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import ConflictPanel from '../../app/components/editor/ConflictPanel.vue';
import { captureCommand } from '../../app/editor/commands';
import { makeAtomicConflict } from '../../app/editor/conflicts';
import EditorShell from '../../app/components/editor/EditorShell.vue';
import SectionPanel from '../../app/components/editor/forms/SectionPanel.vue';
import type {
  ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import type { ResumeRecord } from '../../app/stores/resumes';
import {
  failed,
  fakeApi,
  ok,
  project,
  resumeSummary,
} from '../projects/fixtures';
import { setSiteLocale } from '../support/locale';
import { acceptedFixture } from './fixture';

const fake = vi.hoisted(() => ({
  api: null as unknown,
  resumes: null as unknown,
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

const SHOP = '0190f000-0000-7000-8000-0000000000c1';
const OPS = '0190f000-0000-7000-8000-0000000000c2';
const ENTRY = '0190f000-0000-7000-8000-0000000000e1';
const NEW_ENTRY = '0190f000-0000-7000-8000-0000000000e2';

const shop = () => project({
  id: SHOP, text: { en: { name: 'Shop site' } },
});
const ops = () => project({
  id: OPS, defaultLanguage: 'en', text: { en: { name: 'Ops tool' } },
});

type Entry = Section['entries'][number];

function recordWith(
  entries: readonly Entry[],
  lng = 'en',
  live = false,
): ResumeRecord {
  const accepted = acceptedFixture();
  accepted.metadata = { ...accepted.metadata, lng, live };
  accepted.document.content = {
    projects: {
      sectionType: 'project', displayName: 'Projects', entries: [...entries],
    } as Section,
  };
  accepted.document.customization.layout.sections = {
    main: ['projects'], sidebar: [],
  };
  return {
    accepted,
    current: {
      document: structuredClone(accepted.document),
      metadata: structuredClone(accepted.metadata),
    },
    pending: [],
    attempt: null,
    conflicts: [],
    issues: {},
    templateState: null,
    photoRead: { kind: 'none' },
    completeReadRequired: false,
    sessionLost: false,
    opaquePhotoOutcome: null,
  };
}

/** Actions whose edits join the record's queue, as the store does. */
function actionsFor(record: Ref<ResumeRecord>) {
  return {
    record: computed(() => record.value),
    createEntityId: vi.fn(() => NEW_ENTRY),
    acceptLatest: vi.fn(async () => {}),
    edit: vi.fn((intent: { kind: string }) => {
      const command = { ...intent, id: `${intent.kind}-command` };
      record.value = {
        ...record.value,
        pending: [...record.value.pending, command as never],
      };
      return { kind: 'enqueued', command };
    }),
  } as unknown as ResumeEditorActions & {
    edit: ReturnType<typeof vi.fn>;
  };
}

let api: ReturnType<typeof fakeApi>;
let resumeList: ReturnType<typeof vi.fn>;
const mounted: { unmount: () => void }[] = [];

async function mountPanel(record: Ref<ResumeRecord>) {
  const actions = actionsFor(record);
  const wrapper = await mountSuspended(SectionPanel, {
    attachTo: document.body,
    props: {
      actions,
      sectionKey: 'projects',
      section: computed(() =>
        record.value.current.document.content.projects!).value,
    },
  });
  mounted.push(wrapper);
  await flushPromises();
  return { wrapper, actions };
}

function inDocument(selector: string): HTMLElement {
  const element = document.body.querySelector<HTMLElement>(selector);
  if (element === null) throw new Error(`Missing ${selector}`);
  return element;
}

async function openMenu(): Promise<void> {
  inDocument('[data-action="add-entry"]').click();
  await flushPromises();
}

async function openProjects(): Promise<string[]> {
  await openMenu();
  inDocument('[data-action="from-projects"]').click();
  await flushPromises();
  return [...document.body.querySelectorAll('[data-project-option]')]
    .map((option) => option.textContent?.trim() ?? '');
}

beforeEach(() => {
  setSiteLocale('en');
  api = fakeApi();
  fake.api = api;
  resumeList = vi.fn().mockResolvedValue({
    kind: 'ready',
    items: [
      resumeSummary('resume-1', '1', 'Fixture', { live: true, slug: 'ada' }),
      resumeSummary('other-public', '1', 'Other', {
        live: true, slug: 'ada-2',
      }),
      resumeSummary('other-draft', '1', 'Draft'),
    ],
  });
  fake.resumes = { list: resumeList };
});

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  setSiteLocale(undefined);
});

describe('From your projects', () => {
  it('lists only the library projects not yet on this resume', async () => {
    api.list.mockResolvedValue(ok([shop(), ops()]));
    await mountPanel(ref(recordWith([
      { id: ENTRY, title: 'Shop', projectId: SHOP } as Entry,
    ])));

    expect(api.list).toHaveBeenCalledOnce();
    expect(await openProjects()).toEqual(['Ops tool']);
  });

  it('links to Projects when the library is empty', async () => {
    api.list.mockResolvedValue(ok([]));
    await mountPanel(ref(recordWith([])));

    expect(await openProjects()).toEqual([]);
    expect(inDocument('[data-action="open-projects"]').textContent?.trim())
      .toBe('Open Projects');
  });

  it('hides From your projects behind Retry when the read fails',
    async () => {
      api.list.mockResolvedValueOnce(failed('unavailable'));
      await mountPanel(ref(recordWith([])));
      await openMenu();

      expect(document.body.querySelector('[data-action="from-projects"]'))
        .toBeNull();
      api.list.mockResolvedValueOnce(ok([ops()]));
      inDocument('[data-action="retry-project-library"]').click();
      await flushPromises();

      expect(await openProjects()).toEqual(['Ops tool']);
    });

  it('keeps Blank entry as before', async () => {
    api.list.mockResolvedValue(ok([]));
    const { actions } = await mountPanel(ref(recordWith([])));
    await openMenu();

    inDocument('[data-action="add-blank-entry"]').click();
    await flushPromises();

    expect(actions.edit).toHaveBeenCalledWith({
      kind: 'entryUpsert', sectionKey: 'projects', entry: { id: NEW_ENTRY },
    });
  });

  it('inserts the chosen project and selects its entry', async () => {
    api.list.mockResolvedValue(ok([shop(), ops()]));
    const record = ref(recordWith([]));
    const { wrapper, actions } = await mountPanel(record);
    await openProjects();

    inDocument(`[data-project-option="${OPS}"]`).click();
    await flushPromises();
    expect(actions.edit).toHaveBeenCalledWith({
      kind: 'projectLink', projectId: OPS,
    });
    expect(wrapper.get('[data-testid="project-link-status"]').text())
      .toBe('Adding the project…');
    record.value = {
      ...record.value,
      pending: [],
      current: {
        ...record.value.current,
        document: recordWith([
          { id: NEW_ENTRY, title: 'Ops tool', projectId: OPS } as Entry,
        ]).current.document,
      },
    };
    await flushPromises();

    expect(wrapper.emitted('selectEntry'))
      .toEqual([[{ sectionKey: 'projects', entryId: NEW_ENTRY }]]);
    expect(api.list).toHaveBeenCalledTimes(3);
    expect(wrapper.get('[data-testid="project-link-status"]').text())
      .toBe('');
  });

  it('says so when the project could not be added', async () => {
    api.list.mockResolvedValue(ok([ops()]));
    const record = ref(recordWith([]));
    const { wrapper } = await mountPanel(record);
    await openProjects();
    inDocument(`[data-project-option="${OPS}"]`).click();
    await flushPromises();

    record.value = { ...record.value, pending: [] };
    await flushPromises();

    expect(wrapper.emitted('selectEntry')).toBeUndefined();
    expect(wrapper.get('[data-testid="project-link-status"]').text())
      .toBe('The project could not be added. Try again.');
  });
});

describe('adding a project another published resume shows', () => {
  const shownOn = (...linkedResumeIds: string[]) =>
    project({ id: OPS, text: { en: { name: 'Ops tool' } }, linkedResumeIds });

  async function choose(live: boolean, ...linkedResumeIds: string[]) {
    api.list.mockResolvedValue(ok([shownOn(...linkedResumeIds)]));
    resumeList.mockResolvedValue({
      kind: 'ready', items: [
        resumeSummary('resume-1', '1', 'Fixture', { live }),
        resumeSummary('other-public', '1', 'Other', { live: true }),
        resumeSummary('other-draft', '1', 'Draft'),
      ],
    });
    const mountedPanel = await mountPanel(ref(recordWith([], 'en', live)));
    await openProjects();
    inDocument(`[data-project-option="${OPS}"]`).click();
    await flushPromises();
    return mountedPanel;
  }

  it('asks if the resume was published elsewhere after the section loaded',
    async () => {
      api.list.mockResolvedValue(ok([shownOn('other-public')]));
      const { actions } = await mountPanel(ref(recordWith([], 'en', false)));
      await openProjects();

      inDocument(`[data-project-option="${OPS}"]`).click();
      await flushPromises();

      expect(document.body.querySelector('[role="alertdialog"]'))
        .not.toBeNull();
      inDocument('[data-action="cancel-project-privacy"]').click();
      await flushPromises();
      expect(actions.edit).not.toHaveBeenCalled();
    });

  it('asks if another public resume linked the project after loading',
    async () => {
      api.list.mockResolvedValueOnce(ok([shownOn()]));
      const { actions } = await mountPanel(ref(recordWith([], 'en', true)));
      api.list.mockResolvedValue(ok([shownOn('other-public')]));
      await openProjects();

      inDocument(`[data-project-option="${OPS}"]`).click();
      await flushPromises();

      expect(document.body.querySelector('[role="alertdialog"]'))
        .not.toBeNull();
      expect(actions.edit).not.toHaveBeenCalled();
    });

  it.each(['resumes', 'project'] as const)(
    'asks if the fresh %s read fails', async (read) => {
      api.list.mockResolvedValueOnce(ok([shownOn()]));
      const { actions } = await mountPanel(ref(recordWith([])));
      if (read === 'resumes') {
        resumeList.mockResolvedValue({ kind: 'failed', reason: 'network' });
      } else api.list.mockResolvedValue(failed('unavailable'));
      await openProjects();

      inDocument(`[data-project-option="${OPS}"]`).click();
      await flushPromises();

      expect(document.body.querySelector('[role="alertdialog"]'))
        .not.toBeNull();
      inDocument('[data-action="cancel-project-privacy"]').click();
      await flushPromises();
      expect(actions.edit).not.toHaveBeenCalled();
    },
  );

  it('asks first on a published resume, focused on Cancel', async () => {
    const { actions } = await choose(true, 'other-public');

    const ask = inDocument('[role="alertdialog"]');
    expect(ask.querySelector('h2')?.textContent?.trim())
      .toBe('This project is already shown on another public resume.');
    expect(ask.querySelector('p')?.textContent?.trim())
      .toBe('A reader of both can tell they belong to the same person.');
    expect(document.activeElement?.getAttribute('data-action'))
      .toBe('cancel-project-privacy');
    expect(actions.edit).not.toHaveBeenCalled();
  });

  it('Cancel sends nothing', async () => {
    const { actions } = await choose(true, 'other-public');

    inDocument('[data-action="cancel-project-privacy"]').click();
    await flushPromises();

    expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
    expect(actions.edit).not.toHaveBeenCalled();
  });

  it('Add anyway sends the link once', async () => {
    const { actions } = await choose(true, 'other-public');

    inDocument('[data-action="confirm-project-privacy"]').click();
    await flushPromises();

    expect(actions.edit.mock.calls)
      .toEqual([[{
        kind: 'projectLink', projectId: OPS, privacyConfirmed: true,
      }]]);
  });

  it('does not ask on a draft resume', async () => {
    const { actions } = await choose(false, 'other-public');

    expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
    expect(actions.edit).toHaveBeenCalledOnce();
    expect(resumeList).toHaveBeenCalledOnce();
  });

  it('does not ask when no other published resume shows it', async () => {
    const first = await choose(true);
    expect(first.actions.edit).toHaveBeenCalledOnce();
    for (const wrapper of mounted.splice(0)) wrapper.unmount();

    const { actions } = await choose(true, 'other-draft');

    expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
    expect(actions.edit).toHaveBeenCalledOnce();
  });
});

describe('a linked entry', () => {
  const linkedEntry = (projectId = SHOP) => ({
    id: ENTRY, title: 'My shop', projectId,
  } as Entry);

  it('shows its project under the title and the note above the fields',
    async () => {
      api.list.mockResolvedValue(ok([shop()]));
      const { wrapper } = await mountPanel(ref(recordWith([linkedEntry()])));

      expect(wrapper.get('[data-entry-project]').text()).toContain('Shop site');
      const note = wrapper.get('[data-project-note]');
      expect(note.text()).toContain('This entry comes from the project Shop '
        + 'site. The text here belongs to this resume.');
      expect(note.get('[data-action="open-project"]').attributes('href'))
        .toBe(`/app/projects/${SHOP}`);
      expect(wrapper.find('[data-project-fallback]').exists()).toBe(false);
    });

  it('asks before refreshing, then sends the refresh through the queue',
    async () => {
      api.list.mockResolvedValue(ok([shop()]));
      const { wrapper, actions } = await mountPanel(
        ref(recordWith([linkedEntry()])),
      );

      await wrapper.get('[data-action="refresh-project-entry"]')
        .trigger('click');
      await flushPromises();
      expect(document.body.textContent).toContain(
        'Replace this entry\'s text with the project\'s current text?',
      );
      inDocument('[data-action="cancel-project-refresh"]').click();
      await flushPromises();
      expect(actions.edit).not.toHaveBeenCalled();

      await wrapper.get('[data-action="refresh-project-entry"]')
        .trigger('click');
      await flushPromises();
      inDocument('[data-action="confirm-project-refresh"]').click();
      await flushPromises();

      expect(actions.edit).toHaveBeenCalledWith({
        kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
      });
    });

  it('focuses Open project while refresh waits and leaves focus there',
    async () => {
      api.list.mockResolvedValue(ok([shop()]));
      const record = ref(recordWith([linkedEntry()]));
      const { wrapper } = await mountPanel(record);
      const trigger = wrapper.get<HTMLButtonElement>(
        '[data-action="refresh-project-entry"]',
      );
      trigger.element.focus();
      await trigger.trigger('click');
      await flushPromises();

      inDocument('[data-action="confirm-project-refresh"]').click();
      await flushPromises();

      expect(trigger.element.disabled).toBe(true);
      expect(document.activeElement).toBe(
        wrapper.get('[data-action="open-project"]').element,
      );
      record.value = { ...record.value, pending: [] };
      await flushPromises();
      expect(trigger.element.disabled).toBe(false);
      expect(document.activeElement).toBe(
        wrapper.get('[data-action="open-project"]').element,
      );
    });

  it.each([
    ['vi', true],
    ['vi-VN', true],
    ['en-GB', false],
    ['und', false],
  ])('for a %s resume names the default language when it falls back: %s',
    async (lng, fallback) => {
      api.list.mockResolvedValue(ok([shop()]));
      const { wrapper } = await mountPanel(
        ref(recordWith([linkedEntry()], lng)),
      );

      const line = wrapper.find('[data-project-fallback]');
      expect(line.exists()).toBe(fallback);
      if (fallback) expect(line.text()).toBe('Project page uses English');
    });

  it('loses its note but keeps its text when the link is removed elsewhere',
    async () => {
      api.list.mockResolvedValue(ok([shop()]));
      const record = ref(recordWith([linkedEntry()]));
      const { wrapper } = await mountPanel(record);
      expect(wrapper.find('[data-project-note]').exists()).toBe(true);

      await wrapper.setProps({
        section: recordWith([{ id: ENTRY, title: 'My shop' } as Entry])
          .current.document.content.projects!,
      });
      await flushPromises();

      expect(wrapper.find('[data-project-note]').exists()).toBe(false);
      expect(wrapper.find('[data-entry-project]').exists()).toBe(false);
      expect(wrapper.get<HTMLInputElement>('[data-entry-field="title"] input')
        .element.value).toBe('My shop');
    });
});

describe('project command feedback', () => {
  async function feedback(kind: 'projectRefresh' | 'projectLink') {
    const record = recordWith([]);
    const command = captureCommand(record.current, {
      resumeId: 'resume-1', ownerId: 'owner-1', sequence: 1, dependencyIds: [],
      intent: kind === 'projectLink'
        ? { kind, projectId: OPS }
        : { kind, sectionKey: 'projects', entryId: ENTRY },
    }, { nowEpochMs: () => 0, uuid: () => 'command', delay: async () => {} });
    const conflict = makeAtomicConflict(command, record.accepted, command.base,
      kind === 'projectLink' ? 'project-privacy' : 'target-changed');
    const actions = {
      ...actionsFor(ref(record)), acceptLatest: vi.fn(), applyMine: vi.fn(),
    };
    const wrapper = await mountSuspended(ConflictPanel, {
      attachTo: document.body, props: { actions, conflicts: [conflict] },
    });
    mounted.push(wrapper);
    await flushPromises();
    return { wrapper, actions };
  }

  it.each([
    ['en', 'This entry changed elsewhere. Refresh again if you still want to.'],
    ['vi', 'Mục đã thay đổi ở nơi khác. Hãy làm mới lại nếu bạn vẫn muốn.'],
  ] as const)('announces a dropped refresh politely in %s',
    async (locale, text) => {
      setSiteLocale(locale);
      const { wrapper } = await feedback('projectRefresh');

      expect(wrapper.get('[role="status"]').text()).toContain(text);
    });

  it('acknowledges a dropped refresh conflict while keeping its status',
    async () => {
      const { wrapper, actions } = await feedback('projectRefresh');

      expect(actions.acceptLatest).toHaveBeenCalledWith('command');
      expect(wrapper.get('[role="status"]').text()).toContain(
        'This entry changed elsewhere. Refresh again if you still want to.',
      );
    });

  it('offers Cancel for privacy discovered by the queue', async () => {
    const { actions } = await feedback('projectLink');

    expect(document.activeElement?.getAttribute('data-action'))
      .toBe('cancel-project-privacy');
    inDocument('[data-action="cancel-project-privacy"]').click();
    await flushPromises();
    expect(actions.acceptLatest).toHaveBeenCalledWith('command');
    expect(actions.applyMine).not.toHaveBeenCalled();
  });

  it('confirms privacy discovered by the queue through its conflict action',
    async () => {
      const { actions } = await feedback('projectLink');

      inDocument('[data-action="confirm-project-privacy"]').click();
      await flushPromises();

      expect(actions.applyMine)
        .toHaveBeenCalledWith('command', { kind: 'field' });
    });
});

describe('selecting a linked entry', () => {
  const stub = {
    name: 'SectionPanel',
    props: ['sectionKey', 'section', 'selectedEntryId'],
    emits: ['selectEntry'],
    template: '<div data-testid="section-stub" :data-section="sectionKey" '
      + ':data-selected="selectedEntryId" />',
  };

  async function mountShell(route: string, record: ResumeRecord) {
    const wrapper = await mountSuspended(EditorShell, {
      props: { actions: actionsFor(ref(record)), record },
      route,
      global: {
        stubs: {
          AccountMenu: true,
          EditorPreview: true,
          LanguageTwinReview: true,
          PDFDownloadButton: true,
          PersonalDetailsPanel: true,
          PublishDialog: true,
          SectionPanel: stub,
        },
      },
    });
    mounted.push(wrapper);
    await flushPromises();
    return wrapper;
  }

  it('opens the editor with the project\'s entry selected', async () => {
    const wrapper = await mountShell(
      `/app/resumes/resume-1?project=${SHOP}`,
      recordWith([{ id: ENTRY, projectId: SHOP } as Entry]),
    );

    const panel = wrapper.get('[data-testid="section-stub"]');
    expect(panel.attributes('data-section')).toBe('projects');
    expect(panel.attributes('data-selected')).toBe(ENTRY);
  });

  it('ignores a project the resume does not show, and asks nothing for it',
    async () => {
      const fetches = vi.spyOn(globalThis, 'fetch');
      const wrapper = await mountShell(
        '/app/resumes/resume-1?project=0190f000-0000-7000-8000-00000000dead',
        recordWith([{ id: ENTRY, projectId: SHOP } as Entry]),
      );

      expect(wrapper.find('[data-testid="section-stub"]').exists())
        .toBe(false);
      expect(fetches.mock.calls.map(([input]) => String(input))
        .filter((url) => url.includes('dead'))).toEqual([]);
      fetches.mockRestore();
    });

  it('follows a selection the section asks for', async () => {
    const record = recordWith([]);
    record.current.document.content.more = {
      sectionType: 'project', entries: [],
    } as Section;
    record.current.document.customization.layout.sections.main.push('more');
    const wrapper = await mountShell('/app/resumes/resume-1', record);
    await wrapper.get('[data-outline-key="projects"]').trigger('click');

    wrapper.findComponent({ name: 'SectionPanel' }).vm.$emit('selectEntry', {
      sectionKey: 'more', entryId: NEW_ENTRY,
    });
    await flushPromises();

    const panel = wrapper.get('[data-testid="section-stub"]');
    expect(panel.attributes('data-section')).toBe('more');
    expect(panel.attributes('data-selected')).toBe(NEW_ENTRY);
  });
});
