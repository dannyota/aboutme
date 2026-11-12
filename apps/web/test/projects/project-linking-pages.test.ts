import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  rememberUnlinked,
  type LinkResult,
} from '../../app/components/projects/projectLinks';
import ProjectEditorPage from '../../app/pages/app/projects/[id].vue';
import ProjectListPage from '../../app/pages/app/projects/index.vue';
import type { ResumeListResult } from '../../app/editor/resumeApi';
import { setSiteLocale } from '../support/locale';
import { fakeApi, ok, project, resumeSummary } from './fixtures';
import {
  dialogText,
  inDocument,
  registerSignedIn,
  submitDialog,
  typeInto,
} from './page-support';

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

const linkOk: LinkResult = { kind: 'ok' };
const linkFailed = (failure: string) =>
  ({ kind: 'failed', failure }) as LinkResult;

function resumes() {
  return [
    resumeSummary('resume-1', '1', 'Backend', { live: true, slug: 'ada' }),
    resumeSummary('resume-2', '1', 'Frontend', { lng: 'vi' }),
    resumeSummary('resume-3', '1', 'Data', { lng: 'und' }),
  ];
}

let api: ReturnType<typeof fakeApi>;
let linker: {
  resumes: ReturnType<typeof vi.fn<() => Promise<ResumeListResult>>>;
  link: ReturnType<typeof vi.fn<(...args: string[]) => Promise<LinkResult>>>;
};
const mounted: { unmount: () => void }[] = [];

beforeEach(() => {
  setSiteLocale('en');
  clearNuxtData();
  registerSignedIn();
  api = fakeApi();
  linker = {
    resumes: vi.fn<() => Promise<ResumeListResult>>()
      .mockResolvedValue({ kind: 'ready', items: resumes() }),
    link: vi.fn<(...args: string[]) => Promise<LinkResult>>()
      .mockResolvedValue(linkOk),
  };
  fake.api = api;
  fake.linker = linker;
  vi.mocked(navigateTo).mockClear();
});

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

async function mountList() {
  api.list.mockResolvedValue(ok([project()]));
  const wrapper = await mountSuspended(ProjectListPage, {
    route: '/app/projects',
  });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

async function mountEditor(loaded = project({
  linkedResumeIds: ['resume-1'],
})) {
  api.read.mockResolvedValue(ok(loaded));
  const wrapper = await mountSuspended(ProjectEditorPage, {
    route: `/app/projects/${loaded.id}`,
    attachTo: document.body,
  });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

function row(wrapper: Awaited<ReturnType<typeof mountEditor>>, id: string) {
  return wrapper.get(`[data-resume-row="${id}"]`);
}

describe('New project shown on resumes', () => {
  it('offers a checkbox per resume with its title and mark, none checked',
    async () => {
      const wrapper = await mountList();
      await wrapper.get('[data-testid="new-project"]').trigger('click');
      await flushPromises();

      expect(dialogText()).toContain('Show on resumes');
      const boxes = [...document.body.querySelectorAll<HTMLElement>(
        '[data-resume-choice]',
      )];
      expect(boxes.map((box) => [
        box.querySelector('label')?.textContent?.trim(),
        box.querySelector('p')?.textContent?.trim(),
      ])).toEqual([
        ['Backend', 'Public'], ['Frontend', 'Draft'], ['Data', 'Draft'],
      ]);
      expect(document.body.querySelectorAll('[role="checkbox"]'
        + '[aria-checked="true"]')).toHaveLength(0);
    });

  it('creates, links each checked resume and opens the editor', async () => {
    api.create.mockResolvedValue(ok(project({ id: 'created' })));
    const wrapper = await mountList();
    await wrapper.get('[data-testid="new-project"]').trigger('click');
    await flushPromises();
    await typeInto('#project-create-name', 'Shop');
    inDocument('[data-resume-choice="resume-1"] [role="checkbox"]').click();
    inDocument('[data-resume-choice="resume-3"] [role="checkbox"]').click();
    await flushPromises();

    await submitDialog();

    expect(linker.link.mock.calls).toEqual([
      ['owner-1', 'resume-1', 'created'],
      ['owner-1', 'resume-3', 'created'],
    ]);
    expect(navigateTo).toHaveBeenCalledWith('/app/projects/created');
  });
});

describe('project editor Resumes section', () => {
  it('lists every resume: linked ones are Shown with Open in editor',
    async () => {
      const wrapper = await mountEditor();

      const section = wrapper.get('[data-testid="project-resumes"]');
      expect(section.get('h2').text()).toBe('Resumes');
      expect(section.text()).toContain('To remove the project from a '
        + 'resume, delete its entry in that resume.');
      const shown = row(wrapper, 'resume-1');
      expect(shown.text()).toContain('Shown');
      expect(shown.get('[data-action="project-open-resume"]')
        .attributes('href')).toBe('/app/resumes/resume-1?project=project-1');
      expect(row(wrapper, 'resume-2')
        .get('[data-action="project-add-to-resume"]').text())
        .toBe('Add to this resume');
    });

  it('Linked: adds at once, the row turns to Shown and the status names '
    + 'the resume', async () => {
    const wrapper = await mountEditor();

    await row(wrapper, 'resume-3').get('[data-action="project-add-to-resume"]')
      .trigger('click');
    await flushPromises();

    expect(linker.link)
      .toHaveBeenCalledWith('owner-1', 'resume-3', 'project-1');
    expect(api.replace).not.toHaveBeenCalled();
    expect(row(wrapper, 'resume-3').text()).toContain('Shown');
    const status = wrapper.get('[data-testid="project-resumes-status"]');
    expect(status.attributes('role')).toBe('status');
    expect(status.text()).toBe('Added to “Data”.');
  });

  it('is disabled while the project has unsaved changes', async () => {
    const wrapper = await mountEditor();
    await typeInto('#project-en-role', 'Typed');

    const add = row(wrapper, 'resume-2')
      .get('[data-action="project-add-to-resume"]');
    expect(add.attributes('disabled')).toBeDefined();
    expect(wrapper.get('[data-testid="project-resumes"]').text())
      .toContain('Save the project first.');
    await add.trigger('click');
    expect(linker.link).not.toHaveBeenCalled();
  });

  it('names the default language for a resume whose language it lacks',
    async () => {
      const wrapper = await mountEditor();

      expect(row(wrapper, 'resume-2').get('[data-project-fallback]').text())
        .toBe('Project page uses English');
      expect(row(wrapper, 'resume-1').find('[data-project-fallback]').exists())
        .toBe(false);
      expect(row(wrapper, 'resume-3').find('[data-project-fallback]').exists())
        .toBe(false);
    });

  it('document_invalid: says the resume could not take it and links to it',
    async () => {
      linker.link.mockResolvedValue(linkFailed('document-invalid'));
      const wrapper = await mountEditor();

      await row(wrapper, 'resume-2')
        .get('[data-action="project-add-to-resume"]').trigger('click');
      await flushPromises();

      const status = wrapper.get('[data-testid="project-resumes-status"]');
      expect(status.text()).toContain('“Frontend” could not take this entry.');
      expect(status.get('a').attributes('href')).toBe('/app/resumes/resume-2');
    });

  it('public_state_busy: says the resume is busy', async () => {
    linker.link.mockResolvedValue(linkFailed('busy'));
    const wrapper = await mountEditor();

    await row(wrapper, 'resume-2').get('[data-action="project-add-to-resume"]')
      .trigger('click');
    await flushPromises();

    expect(wrapper.get('[data-testid="project-resumes-status"]').text())
      .toBe('“Frontend” is busy. Try again in a moment.');
  });

  it('opens with an alert naming the resumes a new project could not join',
    async () => {
      rememberUnlinked('project-1', ['Frontend', 'Data']);
      const wrapper = await mountEditor();

      const alert = wrapper.get('[data-testid="project-not-linked"]');
      expect(alert.attributes('role')).toBe('alert');
      expect(alert.text())
        .toBe('The project was not added to “Frontend”, “Data”.');
    });

  it('Delete names how many resumes the project will leave', async () => {
    const wrapper = await mountEditor(project({
      linkedResumeIds: ['resume-1', 'resume-2'],
    }));

    await wrapper.get('#project-delete').trigger('click');
    await flushPromises();

    expect(dialogText()).toContain('It will be removed from 2 resumes. '
      + 'The entries keep their text.');
  });
});

describe('a second published resume', () => {
  const PRIVACY = 'This project is already shown on another public resume. '
    + 'A reader of both can tell they belong to the same person.';
  const twoPublished = () => [
    resumeSummary('resume-1', '1', 'Backend', { live: true, slug: 'ada' }),
    resumeSummary('resume-2', '1', 'Frontend', { live: true, slug: 'ada-2' }),
    resumeSummary('resume-3', '1', 'Data'),
  ];

  it('asks first in the project editor; Cancel sends nothing, Add anyway '
    + 'sends once', async () => {
    linker.resumes.mockResolvedValue({ kind: 'ready', items: twoPublished() });
    const wrapper = await mountEditor();

    await row(wrapper, 'resume-2').get('[data-action="project-add-to-resume"]')
      .trigger('click');
    await flushPromises();
    expect(inDocument('[role="alertdialog"]').textContent)
      .toContain('This project is already shown on another public resume.');
    expect(document.activeElement?.getAttribute('data-action'))
      .toBe('cancel-project-privacy');
    inDocument('[data-action="cancel-project-privacy"]').click();
    await flushPromises();
    expect(linker.link).not.toHaveBeenCalled();

    await row(wrapper, 'resume-2').get('[data-action="project-add-to-resume"]')
      .trigger('click');
    await flushPromises();
    inDocument('[data-action="confirm-project-privacy"]').click();
    await flushPromises();
    expect(linker.link.mock.calls)
      .toEqual([['owner-1', 'resume-2', 'project-1']]);
    expect(row(wrapper, 'resume-2').text()).toContain('Shown');
  });

  it('shows the note in New project once two published resumes are checked',
    async () => {
      linker.resumes.mockResolvedValue({
        kind: 'ready', items: twoPublished(),
      });
      const wrapper = await mountList();
      await wrapper.get('[data-testid="new-project"]').trigger('click');
      await flushPromises();
      const note = () => document.body.querySelector(
        '[data-testid="project-create-privacy"]',
      );

      inDocument('[data-resume-choice="resume-1"] [role="checkbox"]').click();
      inDocument('[data-resume-choice="resume-3"] [role="checkbox"]').click();
      await flushPromises();
      expect(note()).toBeNull();
      inDocument('[data-resume-choice="resume-2"] [role="checkbox"]').click();
      await flushPromises();

      expect(note()?.textContent?.trim()).toBe(PRIVACY);
      expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
    });
});

describe('the Resumes section in Vietnamese', () => {
  it('uses the decided wording', async () => {
    setSiteLocale('vi');
    const wrapper = await mountEditor(project({
      linkedResumeIds: ['resume-1', 'resume-2'],
    }));

    expect(row(wrapper, 'resume-1').text()).toContain('Đang hiển thị');
    expect(row(wrapper, 'resume-2').text()).toContain('Đang hiển thị');
    await typeInto('#project-en-role', 'Typed');
    expect(wrapper.get('[data-testid="project-resumes"]').text())
      .toContain('Lưu dự án trước.');
    await wrapper.get('#project-delete').trigger('click');
    await flushPromises();
    expect(dialogText()).toContain('Dự án sẽ bị gỡ khỏi 2 CV. '
      + 'Nội dung mục trong CV được giữ lại.');
  });
});

describe('a new project with no resumes to show', () => {
  it('leaves the field out', async () => {
    linker.resumes.mockResolvedValue({ kind: 'ready', items: [] });
    const wrapper = await mountList();
    await wrapper.get('[data-testid="new-project"]').trigger('click');
    await flushPromises();

    expect(dialogText()).toContain('Name');
    expect(dialogText()).not.toContain('Show on resumes');
  });
});
