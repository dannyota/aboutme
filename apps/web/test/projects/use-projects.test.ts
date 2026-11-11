import { flushPromises } from '@vue/test-utils';
import { effectScope, ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import { createReleaseAction } from '../../app/composables/projectsApi';
import {
  useProjectEditor,
  useProjectList,
} from '../../app/composables/useProjects';
import {
  failed,
  fakeApi,
  fakeAuth,
  ok,
  project,
  sequence,
  stale,
} from './fixtures';

function inScope<T>(run: () => T): T {
  return effectScope().run(run)!;
}

function listSetup(locale: 'vi' | 'en' = 'vi') {
  const api = fakeApi();
  const { auth } = fakeAuth();
  const navigate = vi.fn();
  api.list.mockResolvedValue(ok([]));
  const list = inScope(() => useProjectList(ref(locale), {
    api, auth, uuid: sequence(), navigate,
  }));
  return { api, auth, navigate, list };
}

async function editorSetup(loaded = project()) {
  const api = fakeApi();
  const { auth } = fakeAuth();
  const navigate = vi.fn();
  api.read.mockResolvedValue(ok(loaded));
  const editor = inScope(() => useProjectEditor(loaded.id, {
    api, auth, uuid: sequence(), navigate,
  }));
  await flushPromises();
  return { api, auth, navigate, editor };
}

function releaseSetup(options: { hasPassword?: boolean } = {}) {
  const api = fakeApi();
  const { auth, identities } = fakeAuth(options);
  const onDone = vi.fn();
  const onStale = vi.fn();
  const providerLogin = ref(false);
  const action = createReleaseAction({
    api, auth, uuid: sequence(), providerLogin, onDone, onStale,
  });
  return { api, auth, identities, onDone, onStale, providerLogin, action };
}

describe('project list', () => {
  it('loads the library newest update first', async () => {
    const { api, list } = listSetup();
    const older = project({ id: 'older', updatedAt: '2026-11-01T00:00:00Z' });
    const newer = project({ id: 'newer', updatedAt: '2026-11-05T00:00:00Z' });
    api.list.mockResolvedValue(ok([older, newer]));
    await list.retry();

    expect(list.items.value.map((item) => item.id)).toEqual(['newer', 'older']);
  });

  it('creates in the site language and opens the editor', async () => {
    const { api, list, navigate } = listSetup('vi');
    await flushPromises();
    api.create.mockResolvedValue(ok(project({ id: 'created' })));

    await list.create('  Cửa hàng  ');

    expect(api.create).toHaveBeenCalledWith(
      { defaultLanguage: 'vi', text: { vi: { name: 'Cửa hàng' } } },
      'k1',
      'csrf-1',
    );
    expect(navigate).toHaveBeenCalledWith('/app/projects/created');
  });

  it('retries an unfinished create with the same key only', async () => {
    const { api, list } = listSetup('en');
    await flushPromises();
    api.create.mockResolvedValueOnce(failed('unavailable'));
    await list.create('Shop');
    expect(list.createState.value).toBe('try-again');

    api.create.mockResolvedValueOnce(failed('rate-limited'));
    await list.create('Shop');
    api.create.mockResolvedValueOnce(failed('unavailable'));
    await list.create('Shop site');

    expect(api.create.mock.calls.map((call) => call[1]))
      .toEqual(['k1', 'k1', 'k2']);
  });

  it('reports the project limit on create', async () => {
    const { api, list } = listSetup();
    await flushPromises();
    api.create.mockResolvedValue(failed('limit'));

    await list.create('Shop');

    expect(list.createState.value).toBe('limit');
  });
});

describe('project editor', () => {
  it('loads the project into an unchanged draft', async () => {
    const { editor, api } = await editorSetup();

    expect(api.read).toHaveBeenCalledWith('project-1');
    expect(editor.view.value).toBe('ready');
    expect(editor.draft.value?.text.en?.name).toBe('Shop site');
    expect(editor.dirty.value).toBe(false);
  });

  it('shows not found for a missing project', async () => {
    const api = fakeApi();
    const { auth } = fakeAuth();
    api.read.mockResolvedValue(failed('not-found'));
    const editor = inScope(() => useProjectEditor('gone', {
      api, auth, uuid: sequence(), navigate: vi.fn(),
    }));
    await flushPromises();

    expect(editor.view.value).toBe('not-found');
  });

  it('saves the whole document with the loaded revision', async () => {
    const { editor, api } = await editorSetup();
    editor.draft.value!.text.en!.name = 'Shop';
    expect(editor.dirty.value).toBe(true);
    const saved = project({
      revision: '4', text: { en: { name: 'Shop', role: 'Lead engineer' } },
    });
    api.replace.mockResolvedValue(ok(saved));

    await editor.save();

    expect(api.replace).toHaveBeenCalledWith('project-1', '3', {
      slug: 'shop-site',
      technologies: ['Go', 'Vue'],
      defaultLanguage: 'en',
      text: { en: { name: 'Shop', role: 'Lead engineer' } },
    }, 'k1', 'csrf-1');
    expect(editor.loaded.value?.revision).toBe('4');
    expect(editor.dirty.value).toBe(false);

    editor.draft.value!.text.en!.role = 'Engineer';
    api.replace.mockResolvedValue(ok(project({ revision: '5' })));
    await editor.save();
    expect(api.replace.mock.calls[1]?.slice(1, 2)).toEqual(['4']);
    expect(api.replace.mock.calls[1]?.[3]).toBe('k2');
  });

  it('keeps typed text on a revision mismatch and reloads the winner',
    async () => {
      const { editor, api } = await editorSetup();
      editor.draft.value!.text.en!.name = 'Typed';
      const winner = project({
        revision: '9', text: { en: { name: 'Elsewhere' } },
      });
      api.replace.mockResolvedValue(stale(winner));

      await editor.save();

      expect(editor.notice.value).toBe('changed-elsewhere');
      expect(editor.draft.value?.text.en?.name).toBe('Typed');
      await editor.reload();
      expect(editor.loaded.value?.revision).toBe('9');
      expect(editor.draft.value?.text.en?.name).toBe('Elsewhere');
      expect(editor.notice.value).toBeNull();
    });

  it('marks invalid fields without sending', async () => {
    const { editor, api } = await editorSetup();
    editor.draft.value!.text.en!.name = '  ';
    editor.draft.value!.demoUrl = 'http://example.com';

    await editor.save();

    expect(api.replace).not.toHaveBeenCalled();
    expect(editor.issues.value).toEqual([
      { path: 'text.en.name', code: 'required' },
      { path: 'demoUrl', code: 'format' },
    ]);
  });

  it('keeps typed text and retries the same request after a rate limit',
    async () => {
      const { editor, api } = await editorSetup();
      editor.draft.value!.text.en!.name = 'Typed';
      api.replace.mockResolvedValueOnce(failed('rate-limited'));

      await editor.save();
      expect(editor.notice.value).toBe('try-again');
      expect(editor.draft.value?.text.en?.name).toBe('Typed');

      api.replace.mockResolvedValueOnce(ok(project({ revision: '4' })));
      await editor.save();
      expect(api.replace.mock.calls.map((call) => call[3]))
        .toEqual(['k1', 'k1']);
    });

  it('adds, defaults and removes languages within the limit', async () => {
    const { editor } = await editorSetup();

    expect(editor.addLanguage('vi')).toBe(true);
    expect(editor.addLanguage('vi')).toBe(false);
    expect(editor.addLanguage('JA')).toBe(false);
    editor.makeDefault('vi');
    editor.removeLanguage('vi');
    expect(Object.keys(editor.draft.value!.text)).toEqual(['en', 'vi']);
    editor.makeDefault('en');
    editor.removeLanguage('vi');
    expect(Object.keys(editor.draft.value!.text)).toEqual(['en']);
    expect(editor.addLanguage('ja')).toBe(true);
    expect(editor.addLanguage('ko')).toBe(true);
    expect(editor.addLanguage('fr')).toBe(true);
    expect(editor.canAddLanguage.value).toBe(false);
    expect(editor.addLanguage('de')).toBe(false);
  });
});

describe('address change and deletion', () => {
  it('sends the stored document with the new slug', async () => {
    const { api, action, onDone } = releaseSetup();
    const renamed = project({ slug: 'new-shop', revision: '4' });
    api.replace.mockResolvedValue(ok(renamed));

    await action.start('address', project(), 'new-shop');

    expect(api.replace).toHaveBeenCalledWith('project-1', '3', {
      slug: 'new-shop',
      technologies: ['Go', 'Vue'],
      defaultLanguage: 'en',
      text: { en: { name: 'Shop site', role: 'Lead engineer' } },
    }, 'k1', 'csrf-1');
    expect(onDone).toHaveBeenCalledWith('address', renamed);
    expect(action.state.value).toEqual({ kind: 'done' });
  });

  it.each([
    ['slug-taken'],
    ['slug-held'],
  ] as const)('reports %s', async (failure) => {
    const { api, action, onDone } = releaseSetup();
    api.replace.mockResolvedValue(failed(failure));

    await action.start('address', project(), 'taken');

    expect(action.state.value).toEqual({ kind: failure });
    expect(onDone).not.toHaveBeenCalled();
  });

  it('reauthenticates, then repeats the action once with the same key',
    async () => {
      const { api, auth, action, onDone } = releaseSetup();
      api.replace
        .mockResolvedValueOnce(failed('reauth-required'))
        .mockResolvedValueOnce(failed('reauth-required'));

      await action.start('address', project(), 'new-shop');
      expect(action.state.value)
        .toEqual({ kind: 'reauth-required', method: 'password' });

      await action.reauthPassword('secret');
      expect(auth.mutate).toHaveBeenCalledWith(
        '/api/v1/auth/password/reauth',
        { method: 'POST', body: { password: 'secret' } },
      );
      expect(api.replace).toHaveBeenCalledTimes(2);
      expect(action.state.value)
        .toEqual({ kind: 'reauth-required', method: 'password' });

      api.replace.mockResolvedValueOnce(ok(project({ slug: 'new-shop' })));
      await action.reauthPassword('secret');
      expect(api.replace).toHaveBeenCalledTimes(3);
      expect(api.replace.mock.calls.map((call) => call[3]))
        .toEqual(['k1', 'k1', 'k1']);
      expect(onDone).toHaveBeenCalledTimes(1);
    });

  it('offers the provider step to an account without a password',
    async () => {
      const { api, action, identities, providerLogin } = releaseSetup({
        hasPassword: false,
      });
      providerLogin.value = true;
      identities.value = [{
        id: 'identity-1', provider: 'google', createdAt: '2026-01-01T00:00:00Z',
      }];
      api.remove.mockResolvedValue(failed('reauth-required'));

      await action.start('delete', project());

      expect(action.state.value)
        .toEqual({ kind: 'reauth-required', method: 'provider' });
    });

  it('deletes with the loaded revision', async () => {
    const { api, action, onDone } = releaseSetup();
    api.remove.mockResolvedValue(ok(null));

    await action.start('delete', project());

    expect(api.remove).toHaveBeenCalledWith('project-1', '3', 'k1', 'csrf-1');
    expect(onDone).toHaveBeenCalledWith('delete', null);
  });

  it('hands a newer project to the page on a revision mismatch', async () => {
    const { api, action, onStale } = releaseSetup();
    const winner = project({ revision: '8' });
    api.remove.mockResolvedValue(stale(winner));

    await action.start('delete', project());

    expect(action.state.value).toEqual({ kind: 'stale' });
    expect(onStale).toHaveBeenCalledWith(winner);
  });
});
