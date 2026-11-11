import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import ProjectEditorPage from '../../app/pages/app/projects/[id].vue';
import { setSiteLocale } from '../support/locale';
import {
  failed,
  fakeApi,
  ok,
  project,
  stale,
} from './fixtures';
import {
  click,
  dialogText,
  inDocument,
  reauthCalls,
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

async function mountEditor(loaded = project()) {
  api.read.mockResolvedValue(ok(loaded));
  const wrapper = await mountSuspended(ProjectEditorPage, {
    route: `/app/projects/${loaded.id}`,
    attachTo: document.body,
  });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

async function save(wrapper: Awaited<ReturnType<typeof mountEditor>>) {
  await wrapper.get('[data-testid="project-form"]').trigger('submit');
  await flushPromises();
}

describe('project editor handling', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNuxtData();
    registerSignedIn();
    reauthCalls.length = 0;
    api = fakeApi();
    fake.api = api;
    vi.mocked(navigateTo).mockClear();
  });

  it('Saved: shows Saved and takes the new revision', async () => {
    const wrapper = await mountEditor();
    await typeInto('#project-en-name', 'Shop');
    expect(wrapper.get('[data-state-mark]').text()).toBe('Unsaved');
    api.replace.mockResolvedValue(ok(project({
      revision: '4', text: { en: { name: 'Shop', role: 'Lead engineer' } },
    })));

    await save(wrapper);

    expect(wrapper.get('[data-state-mark="saved"]').text()).toBe('Saved');
    expect(wrapper.get('[data-testid="project-title"]').text()).toBe('Shop');
    await typeInto('#project-en-role', 'Engineer');
    api.replace.mockResolvedValue(ok(project({ revision: '5' })));
    await save(wrapper);
    expect(api.replace.mock.calls.map((call) => call[1])).toEqual(['3', '4']);
  });

  it('Revision mismatch: keeps the typed text and offers Reload', async () => {
    const wrapper = await mountEditor();
    await typeInto('#project-en-name', 'Typed');
    api.replace.mockResolvedValue(stale(project({
      revision: '9', text: { en: { name: 'Elsewhere' } },
    })));

    await save(wrapper);

    expect(wrapper.get('[data-testid="project-notice"]').text())
      .toContain('This project changed elsewhere.');
    expect(inDocument<HTMLInputElement>('#project-en-name').value)
      .toBe('Typed');
    await wrapper.get('[data-action="project-reload"]').trigger('click');
    await flushPromises();
    expect(inDocument<HTMLInputElement>('#project-en-name').value)
      .toBe('Elsewhere');
    expect(wrapper.find('[data-testid="project-notice"]').exists())
      .toBe(false);
  });

  it('Validation error: marks the fields and focuses the error summary',
    async () => {
      const wrapper = await mountEditor();
      await typeInto('#project-en-name', ' ');
      await typeInto('#project-repository-url', 'http://example.com');

      await save(wrapper);
      await flushPromises();

      expect(api.replace).not.toHaveBeenCalled();
      expect(inDocument('#project-en-name').getAttribute('aria-invalid'))
        .toBe('true');
      expect(inDocument('#project-en-name-error').textContent?.trim())
        .toBe('Enter a name in the default language.');
      expect(inDocument('#project-repository-url-error').textContent?.trim())
        .toBe('Use an address that starts with https://.');
      expect(document.activeElement?.closest('.editor-error-summary'))
        .not.toBeNull();
    });

  it('project_not_found: shows a not-found panel linking to Projects',
    async () => {
      api.read.mockResolvedValue(failed('not-found'));
      const wrapper = await mountSuspended(ProjectEditorPage, {
        route: '/app/projects/gone',
      });
      mounted.push(wrapper);
      await flushPromises();

      const panel = wrapper.get('[data-testid="project-not-found"]');
      expect(panel.text()).toContain('Project not found');
      expect(panel.get('a').attributes('href')).toBe('/app/projects');
    });

  it.each([['rate-limited'], ['unavailable']] as const)(
    'Rate limited or unavailable (%s): keeps the text and says try again',
    async (failure) => {
      const wrapper = await mountEditor();
      await typeInto('#project-en-name', 'Typed');
      api.replace.mockResolvedValue(failed(failure));

      await save(wrapper);

      expect(wrapper.get('[data-testid="project-notice"]').text())
        .toBe('That could not finish right now. Try again.');
      expect(inDocument<HTMLInputElement>('#project-en-name').value)
        .toBe('Typed');
    },
  );

  it.each([
    ['slug-taken', 'This address is in use.'],
    ['slug-held', 'This account used this address recently. Choose another.'],
  ] as const)('%s: says why the address cannot be used', async (code, text) => {
    const wrapper = await mountEditor();
    await wrapper.get('#project-change-address').trigger('click');
    await flushPromises();
    await typeInto('#project-new-address', 'new-shop');
    api.replace.mockResolvedValue(failed(code));

    await submitDialog();

    expect(inDocument('#project-new-address-error').textContent?.trim())
      .toBe(text);
    expect(dialogText()).toContain(text);
  });

  it('reauth_required on address: reauthenticates, then repeats once',
    async () => {
      const wrapper = await mountEditor();
      await wrapper.get('#project-change-address').trigger('click');
      await flushPromises();
      await typeInto('#project-new-address', 'new-shop');
      api.replace
        .mockResolvedValueOnce(failed('reauth-required'))
        .mockResolvedValueOnce(
          ok(project({ slug: 'new-shop', revision: '4' })),
        );

      await submitDialog();
      expect(dialogText())
        .toContain('This action needs you to reauthenticate.');
      await typeInto('[data-action="project-password"]', 'secret');
      await click('[data-action="project-password-reauth"]');

      expect(reauthCalls).toHaveLength(1);
      expect(api.replace).toHaveBeenCalledTimes(2);
      const keys = api.replace.mock.calls.map((call) => call[3]);
      expect(keys[1]).toBe(keys[0]);
      expect(wrapper.get('[data-testid="project-slug"]').text())
        .toBe('new-shop');
      expect(document.body.querySelector('[role="dialog"]')).toBeNull();
    });

  it('reauth_required on delete: reauthenticates, deletes, returns to list',
    async () => {
      const wrapper = await mountEditor();
      await wrapper.get('#project-delete').trigger('click');
      await flushPromises();
      expect(dialogText()).toContain('Delete “Shop site”?');
      api.remove
        .mockResolvedValueOnce(failed('reauth-required'))
        .mockResolvedValueOnce(ok(null));

      await submitDialog();
      await typeInto('[data-action="project-password"]', 'secret');
      await click('[data-action="project-password-reauth"]');

      expect(api.remove).toHaveBeenCalledTimes(2);
      expect(api.remove.mock.calls[1]).toEqual(api.remove.mock.calls[0]);
      expect(navigateTo).toHaveBeenCalledWith('/app/projects');
    });
});

describe('project editor text and details', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNuxtData();
    registerSignedIn();
    api = fakeApi();
    fake.api = api;
  });

  it('adds a language, makes it the default and removes the old one',
    async () => {
      const wrapper = await mountEditor();
      await wrapper.get('#project-add-language').trigger('click');
      await flushPromises();
      await submitDialog();

      const tabs = () => wrapper.findAll('[role="tab"]').map((tab) =>
        tab.text());
      expect(tabs()).toEqual(['English · default', 'Tiếng Việt']);
      await typeInto('#project-vi-name', 'Trang bán hàng');
      await click('[data-testid="project-language-actions"]');
      await click('[data-action="project-make-default"]');
      expect(tabs()).toEqual(['English', 'Tiếng Việt · default']);

      await wrapper.get('[data-language="en"]').trigger('mousedown');
      await flushPromises();
      await click('[data-testid="project-language-actions"]');
      await click('[data-action="project-remove-language"]');
      expect(tabs()).toEqual(['Tiếng Việt · default']);
      api.replace.mockResolvedValue(ok(project({ revision: '4' })));
      await save(wrapper);
      expect(api.replace.mock.calls[0]?.[2]).toMatchObject({
        defaultLanguage: 'vi',
      });
      expect(api.replace.mock.calls[0]?.[2].text)
        .toEqual({ vi: { name: 'Trang bán hàng' } });
    });

  it('shows a counter only within 20 characters of the limit', async () => {
    await mountEditor();
    await typeInto('#project-en-name', 'x'.repeat(99));
    expect(document.body.querySelector('#project-en-name-hint')).toBeNull();
    await typeInto('#project-en-name', 'x'.repeat(100));
    expect(inDocument('#project-en-name-hint').textContent?.trim())
      .toBe('100 / 120 characters');
  });

  it('keeps Save disabled until something changes', async () => {
    const wrapper = await mountEditor();
    const button = () => wrapper.get('[data-action="project-save"]');
    expect(button().attributes('disabled')).toBeDefined();
    await typeInto('#project-demo-url', 'https://demo.example');
    expect(button().attributes('disabled')).toBeUndefined();
  });
});
