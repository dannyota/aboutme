import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import ProjectListPage from '../../app/pages/app/projects/index.vue';
import { setSiteLocale } from '../support/locale';
import { failed, fakeApi, ok, project } from './fixtures';
import {
  dialogText,
  registerSignedIn,
  submitDialog,
  typeInto,
} from './page-support';

const fake = vi.hoisted(() => ({ api: null as unknown }));
vi.mock('../../app/composables/projectsApi', async (importOriginal) => ({
  ...await importOriginal<
    typeof import('../../app/composables/projectsApi')>(),
  createProjectsApi: () => fake.api,
}));
mockNuxtImport('navigateTo', () => vi.fn());

let api: ReturnType<typeof fakeApi>;
const mounted: { unmount: () => void }[] = [];

async function mountList() {
  const wrapper = await mountSuspended(ProjectListPage, {
    route: '/app/projects',
  });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

describe('project list page', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNuxtData();
    registerSignedIn();
    api = fakeApi();
    fake.api = api;
  });

  it('shows the empty panel with the lead line and New project', async () => {
    api.list.mockResolvedValue(ok([]));
    const wrapper = await mountList();

    const panel = wrapper.get('[data-testid="projects-empty"]');
    expect(panel.text()).toContain('No projects yet.');
    expect(panel.text()).toContain(
      'Write each project once, then choose which resumes show it.',
    );
    expect(panel.get('[data-testid="new-project"]').text())
      .toBe('New project');
  });

  it('lists projects newest first with chips, time and count', async () => {
    api.list.mockResolvedValue(ok([
      project({ id: 'old', updatedAt: '2026-10-01T00:00:00Z' }),
      project({
        id: 'new',
        updatedAt: '2026-11-02T00:00:00Z',
        defaultLanguage: 'vi',
        text: { vi: { name: 'Cửa hàng' }, en: { name: 'Shop' }, ja: {} },
        technologies: ['Go', 'Vue', 'Postgres', 'Caddy', 'Podman'],
      }),
    ]));
    const wrapper = await mountList();

    expect(wrapper.get('[data-testid="project-count"]').text()).toBe('2 / 30');
    const rows = wrapper.findAll('[data-testid^="project-row-"]');
    expect(rows.map((row) => row.attributes('data-testid')))
      .toEqual(['project-row-new', 'project-row-old']);
    const first = rows[0]!;
    expect(first.get('[data-project-link]').attributes('href'))
      .toBe('/app/projects/new');
    expect(first.findAll('[data-project-technologies] li').map((chip) =>
      chip.text())).toEqual(['Go', 'Vue', 'Postgres', '+2']);
    expect(first.findAll('[data-project-languages] li').map((chip) =>
      chip.text())).toEqual(['VI · default', 'EN']);
    expect(rows[1]!.get('[data-project-role]').text()).toBe('Lead engineer');
    expect(first.get('time').attributes('datetime'))
      .toBe('2026-11-02T00:00:00Z');
  });

  it('shows three skeleton rows while loading', async () => {
    api.list.mockReturnValue(new Promise(() => {}));
    const wrapper = await mountList();

    const loading = wrapper.get('[data-testid="projects-loading"]');
    expect(loading.attributes('role')).toBe('status');
    expect(loading.findAll(':scope > div')).toHaveLength(3);
  });

  it('shows an alert with Retry when the list fails', async () => {
    api.list.mockResolvedValueOnce(failed('unavailable'));
    const wrapper = await mountList();

    const alert = wrapper.get('[data-testid="projects-unavailable"]');
    expect(alert.attributes('role')).toBe('alert');
    api.list.mockResolvedValueOnce(ok([]));
    await alert.get('[data-action="projects-retry"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-testid="projects-empty"]').exists()).toBe(true);
  });

  it('disables New project at 30 and says the limit is reached', async () => {
    api.list.mockResolvedValue(ok(Array.from({ length: 30 }, (_, index) =>
      project({ id: `project-${index}` }))));
    const wrapper = await mountList();

    expect(wrapper.get('[data-testid="new-project"]').attributes('disabled'))
      .toBeDefined();
    expect(wrapper.get('[data-testid="project-limit"]').text())
      .toBe('You have reached the limit of 30 projects.');
  });

  it('project_limit on create: says the limit of 30 is reached', async () => {
    api.list.mockResolvedValue(ok([project()]));
    api.create.mockResolvedValue(failed('limit'));
    const wrapper = await mountList();
    await wrapper.get('[data-testid="new-project"]').trigger('click');
    await flushPromises();
    await typeInto('#project-create-name', 'Another');

    await submitDialog();

    expect(api.create).toHaveBeenCalledTimes(1);
    expect(dialogText())
      .toContain('You have reached the limit of 30 projects.');
  });

  it('speaks Vietnamese by default', async () => {
    setSiteLocale('vi');
    api.list.mockResolvedValue(ok([project()]));
    const wrapper = await mountList();

    expect(wrapper.get('h1').text()).toBe('Dự án');
    expect(wrapper.get('[data-testid="projects-lead"]').text())
      .toBe('Viết mỗi dự án một lần, rồi chọn CV nào hiển thị nó.');
    expect(wrapper.get('[data-testid="new-project"]').text())
      .toBe('Dự án mới');
  });
});
