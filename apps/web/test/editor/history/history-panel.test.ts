import { flushPromises } from '@vue/test-utils';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import HistoryPanel
  from '../../../app/components/editor/history/HistoryPanel.vue';
import HistoryPreview
  from '../../../app/components/editor/history/HistoryPreview.vue';
import { createHistoryController } from '../../../app/editor/historyController';
import { editorShellCopy } from '../../../app/i18n/editor-shell';
import { editorHistoryCopy } from '../../../app/i18n/editor-history';
import { setSiteLocale } from '../../support/locale';
import { fakeApi, fakeDeps, fakeRecord, item, version } from './support';

type Api = ReturnType<typeof fakeApi>;

beforeEach(() => setSiteLocale('en'));
afterEach(() => {
  document.body.innerHTML = '';
  setSiteLocale(undefined);
});

async function mountPanel(
  api: Api,
  record = fakeRecord(),
  restoreBlocked = false,
) {
  const { deps } = fakeDeps(api, record);
  const controller = createHistoryController(deps);
  const wrapper = await mountSuspended(HistoryPanel, {
    attachTo: document.body,
    props: { controller, record: record as never, restoreBlocked },
  });
  await flushPromises();
  return { wrapper, controller, record };
}

const dialog = () => document.body.querySelector('[role="alertdialog"]');

describe('History panel states', () => {
  it('shows the heading, lead and skeleton rows while loading', async () => {
    const api = fakeApi();
    api.list.mockImplementation(() => new Promise(() => {}));
    const { wrapper } = await mountPanel(api);
    expect(wrapper.get('h2').text()).toBe(editorShellCopy.en.history);
    expect(wrapper.text()).toContain(editorHistoryCopy.en.lead);
    expect(wrapper.findAll('[data-history-skeleton]')).toHaveLength(3);
    expect(wrapper.find('[data-history-count]').exists()).toBe(false);
  });

  it('shows the empty state without Current or Clear history', async () => {
    const { wrapper } = await mountPanel(fakeApi([]));
    const heading = wrapper.get('h3');
    expect(heading.text()).toBe(editorHistoryCopy.en.emptyTitle);
    expect(heading.attributes('tabindex')).toBe('-1');
    expect(wrapper.text()).toContain(editorHistoryCopy.en.emptyBody);
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false);
    expect(wrapper.find('[data-action="open-clear"]').exists()).toBe(false);
    expect(wrapper.find('[data-history-count]').exists()).toBe(false);
  });

  it('shows a load failure with Try again', async () => {
    const api = fakeApi();
    api.list.mockResolvedValue({ kind: 'failed' } as never);
    const { wrapper } = await mountPanel(api);
    expect(wrapper.get('[role="alert"]').text()).toContain(
      editorHistoryCopy.en.loadFailed,
    );
    api.list.mockResolvedValue({ kind: 'ok', value: [item('a')] });
    await wrapper.get('[data-action="history-retry"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[role="listbox"]').exists()).toBe(true);
  });

  it('lists Current, owner and assistant rows with the count', async () => {
    const api = fakeApi([
      item('a', { cause: 'edit' }),
      item('b', {
        cause: 'assistant',
        actor: { kind: 'assistant', name: 'Claude' },
      }),
      item('c', {
        cause: 'assistant',
        actor: { kind: 'assistant', name: '' },
      }),
      item('d', { cause: 'restore' }),
    ]);
    const { wrapper } = await mountPanel(api);
    const options = wrapper.findAll('[role="option"]');
    expect(options).toHaveLength(5);
    expect(options[0]!.attributes('data-version-id')).toBe('current');
    expect(options[0]!.attributes('aria-selected')).toBe('true');
    expect(wrapper.get('[data-history-count]').text()).toBe('4 / 50');
    const text = options.map((option) => option.text()).join('|');
    expect(text).toContain(editorHistoryCopy.en.causeEdit);
    expect(text).toContain(editorHistoryCopy.en.causeAssistant('Claude'));
    expect(text).toContain(editorHistoryCopy.en.causeAssistantUnnamed);
    expect(text).toContain(editorHistoryCopy.en.causeRestore);
    expect(options[1]!.get('time').attributes('datetime')).toMatch(/Z$/);
    expect(options[1]!.text()).toContain('Jan 5, 2:05 PM');
    expect(wrapper.text()).toContain(editorHistoryCopy.en.retention);
    expect(wrapper.get('[data-action="open-clear"]').text()).toBe(
      editorHistoryCopy.en.clear,
    );
  });

  it('formats times in Vietnamese', async () => {
    setSiteLocale('vi');
    const { wrapper } = await mountPanel(fakeApi([item('a')]));
    expect(wrapper.findAll('[role="option"]')[1]!.text()).toContain(
      '14:05, 5 thg 1',
    );
    expect(wrapper.text()).toContain(editorHistoryCopy.vi.retention);
  });

  it('selects on click and Enter, not on arrow focus', async () => {
    const api = fakeApi([item('a'), item('b')]);
    const { wrapper, controller } = await mountPanel(api);
    const list = wrapper.get('[role="listbox"]');
    await list.get('[data-version-id="current"]').trigger('keydown', {
      key: 'ArrowDown',
    });
    expect(api.read).not.toHaveBeenCalled();
    expect(controller.selected.value).toBe('current');
    await list.get('[data-version-id="a"]').trigger('keydown', {
      key: 'Enter',
    });
    await flushPromises();
    expect(api.read).toHaveBeenCalledWith('resume-1', 'a');
    expect(controller.selected.value).toBe('a');
    expect(
      wrapper.get('[data-version-id="a"]').attributes('aria-selected'),
    ).toBe('true');
    expect(wrapper.get('[data-history-status]').text()).toContain(
      'Showing the version from',
    );
  });

  it('is one tab stop with roving tabindex', async () => {
    const { wrapper } = await mountPanel(fakeApi([item('a'), item('b')]));
    const tabbable = wrapper
      .findAll('[role="option"]')
      .filter((option) => option.attributes('tabindex') === '0');
    expect(tabbable).toHaveLength(1);
  });
});

describe('Restore confirmation', () => {
  it('names the public link for a published resume', async () => {
    const record = fakeRecord();
    record.accepted.metadata = {
      ...record.accepted.metadata,
      live: true,
      slug: 'ada-lovelace',
    };
    const { controller } = await mountPanel(fakeApi([item('a')]), record);
    controller.openRestore('a');
    await flushPromises();
    expect(dialog()!.textContent).toContain('This resume is public.');
    expect(dialog()!.textContent).toContain('ada-lovelace');
    expect(dialog()!.textContent).toContain(editorHistoryCopy.en.restoreKept);
    expect(dialog()!.textContent).toContain(
      editorHistoryCopy.en.restoreUndoNote,
    );
  });

  it('says only the owner sees the change when unpublished', async () => {
    const { controller } = await mountPanel(fakeApi([item('a')]));
    controller.openRestore('a');
    await flushPromises();
    expect(dialog()!.textContent).toContain(
      editorHistoryCopy.en.restorePrivate,
    );
  });

  it('lists a new title and language only when they differ', async () => {
    const api = fakeApi([item('a', { title: 'Older title' })]);
    api.read.mockResolvedValue({
      kind: 'ok',
      value: version('a', { title: 'Older title', lng: 'vi' }),
    });
    const { controller } = await mountPanel(api);
    await controller.select('a');
    controller.openRestore('a');
    await flushPromises();
    expect(dialog()!.textContent).toContain('Older title');
    expect(dialog()!.textContent).toContain(
      editorHistoryCopy.en.restoreNewLanguage('Tiếng Việt'),
    );
  });

  it('omits them when equal to the current resume', async () => {
    const record = fakeRecord();
    const api = fakeApi([item('a', { title: record.accepted.metadata.title })]);
    api.read.mockResolvedValue({
      kind: 'ok',
      value: version('a', {
        title: record.accepted.metadata.title,
        lng: record.accepted.metadata.lng,
      }),
    });
    const { controller } = await mountPanel(api, record);
    await controller.select('a');
    controller.openRestore('a');
    await flushPromises();
    expect(dialog()!.textContent).not.toContain('The resume title becomes');
    expect(dialog()!.textContent).not.toContain('The resume language becomes');
  });

  it('hides Restore and reads Close after an invalid result', async () => {
    const api = fakeApi([item('a')]);
    api.write.mockResolvedValue({ kind: 'invalid' } as never);
    const { controller } = await mountPanel(api);
    controller.openRestore('a');
    await flushPromises();
    document.body
      .querySelector<HTMLElement>('[data-action="restore-confirm"]')!
      .click();
    await flushPromises();
    expect(dialog()!.textContent).toContain(
      editorHistoryCopy.en.restoreInvalid,
    );
    expect(
      document.body.querySelector('[data-action="restore-confirm"]'),
    ).toBeNull();
    expect(
      document.body.querySelector('[data-action="restore-cancel"]')!
        .textContent,
    ).toContain('Close');
  });

  it('shows the success banner with Undo after a restore', async () => {
    const api = fakeApi([item('a')]);
    const record = fakeRecord();
    api.write.mockResolvedValue({
      kind: 'accepted',
      resume: record.accepted,
    } as never);
    const { wrapper, controller } = await mountPanel(api, record);
    api.list.mockResolvedValue({
      kind: 'ok',
      value: [
        item('r', { cause: 'restore', startedAt: new Date().toISOString() }),
        item('a'),
      ],
    });
    controller.openRestore('a');
    await controller.confirmRestore();
    await flushPromises();
    const banner = wrapper.get('[data-history-restored]');
    expect(banner.text()).toContain('Restored the version from');
    await banner.get('[data-action="history-undo"]').trigger('click');
    await flushPromises();
    expect(dialog()).not.toBeNull();
  });
});

describe('Blocked restore and failed reloads', () => {
  it('gates Undo on the same blocked state as the version bar', async () => {
    const { wrapper, controller } = await mountPanel(
      fakeApi([item('a')]),
      fakeRecord(),
      true,
    );
    controller.banner.value = {
      id: 'a',
      startedAt: item('a').startedAt,
      undoId: 'r',
    };
    await flushPromises();
    const undo = wrapper.get('[data-action="history-undo"]');
    expect(undo.attributes('aria-disabled')).toBe('true');
    await undo.trigger('click');
    expect(controller.restore.value).toBeNull();
  });

  it('keeps the rows and shows the banner on a later read error', async () => {
    const { wrapper, controller } = await mountPanel(fakeApi([item('a')]));
    controller.loadFailed.value = true;
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain(
      editorHistoryCopy.en.loadFailed,
    );
    expect(wrapper.find('[role="listbox"]').exists()).toBe(true);
  });

  it('reads Deleting while a delete is in flight', async () => {
    const { controller } = await mountPanel(fakeApi([item('a')]));
    controller.remove.value = { id: 'a', phase: 'busy' };
    await flushPromises();
    expect(
      document.body.querySelector('[data-action="delete-confirm"]')!
        .textContent,
    ).toContain(editorHistoryCopy.en.deleting);
  });
});

describe('Delete and clear confirmations', () => {
  it('confirms a delete with focus on Cancel', async () => {
    const api = fakeApi([item('a')]);
    const { controller } = await mountPanel(api);
    await controller.select('a');
    controller.openDelete();
    await flushPromises();
    expect(dialog()!.textContent).toContain(editorHistoryCopy.en.deleteBody);
    expect(
      document.body.querySelector('[data-action="delete-confirm"]')!
        .textContent,
    ).toContain(editorHistoryCopy.en.deleteConfirm);
    api.write.mockResolvedValueOnce({ kind: 'failed' } as never);
    document.body
      .querySelector<HTMLElement>('[data-action="delete-confirm"]')!
      .click();
    await flushPromises();
    expect(dialog()!.textContent).toContain(editorHistoryCopy.en.deleteFailed);
  });

  it('confirms clear with the version count', async () => {
    const api = fakeApi([item('a'), item('b')]);
    const { wrapper } = await mountPanel(api);
    await wrapper.get('[data-action="open-clear"]').trigger('click');
    await flushPromises();
    expect(dialog()!.textContent).toContain(editorHistoryCopy.en.clearTitle);
    expect(dialog()!.textContent).toContain('2 versions');
    api.write.mockResolvedValueOnce({ kind: 'done' } as never);
    api.list.mockResolvedValue({ kind: 'ok', value: [] });
    document.body
      .querySelector<HTMLElement>('[data-action="clear-confirm"]')!
      .click();
    await flushPromises();
    expect(wrapper.get('h3').text()).toBe(editorHistoryCopy.en.emptyTitle);
    expect(wrapper.get('[data-history-status]').text()).toBe(
      editorHistoryCopy.en.cleared,
    );
  });
});

describe('Version bar', () => {
  async function mountBar(
    restoreBlocked: boolean,
    versionOver: Partial<ReturnType<typeof version>> = {},
  ) {
    const api = fakeApi([item('a')]);
    api.read.mockResolvedValue({
      kind: 'ok',
      value: version('a', versionOver),
    });
    const record = fakeRecord();
    const { deps } = fakeDeps(api, record);
    const controller = createHistoryController(deps);
    controller.open();
    await flushPromises();
    await controller.select('a');
    const wrapper = await mountSuspended(HistoryPreview, {
      attachTo: document.body,
      props: { controller, record: record as never, restoreBlocked },
      global: {
        stubs: {
          EditorPreview: {
            name: 'EditorPreview',
            props: ['lng', 'document', 'label'],
            template: '<div />',
          },
        },
      },
    });
    await flushPromises();
    return { wrapper, controller };
  }

  it('shows the title, view-only line and actions', async () => {
    const { wrapper } = await mountBar(false);
    expect(wrapper.get('h2').text()).toContain('Version from');
    expect(wrapper.get('h2').attributes('tabindex')).toBe('-1');
    expect(wrapper.text()).toContain(editorHistoryCopy.en.viewOnly);
    expect(
      wrapper.get('[data-action="history-delete"]').attributes('aria-label'),
    ).toBe(editorHistoryCopy.en.deleteVersionLabel);
    expect(wrapper.get('[data-action="history-restore"]').text()).toBe(
      editorHistoryCopy.en.restore,
    );
  });

  it('keeps Restore focusable but inert until saved', async () => {
    const { wrapper, controller } = await mountBar(true);
    const restore = wrapper.get('[data-action="history-restore"]');
    expect(restore.attributes('aria-disabled')).toBe('true');
    expect(restore.attributes('disabled')).toBeUndefined();
    expect(wrapper.text()).toContain(editorHistoryCopy.en.restoreBlocked);
    await restore.trigger('click');
    expect(controller.restore.value).toBeNull();
  });

  it('opens the confirmation when Restore is allowed', async () => {
    const { wrapper, controller } = await mountBar(false);
    expect(wrapper.text()).not.toContain(editorHistoryCopy.en.restoreBlocked);
    await wrapper.get('[data-action="history-restore"]').trigger('click');
    expect(controller.restore.value?.phase).toBe('confirm');
  });

  it('renders the version in its own language, unset when null', async () => {
    const own = await mountBar(false, { lng: 'vi' });
    expect(own.wrapper.findComponent({ name: 'EditorPreview' }).props('lng'))
      .toBe('vi');
    const unset = await mountBar(false, { lng: null });
    expect(
      unset.wrapper.findComponent({ name: 'EditorPreview' }).props('lng'),
    ).toBe('und');
  });

  it('shows the loading, failed and gone states', async () => {
    const { wrapper, controller } = await mountBar(false);
    controller.preview.value = { kind: 'loading' };
    await flushPromises();
    expect(wrapper.text()).toContain(editorHistoryCopy.en.previewLoading);
    controller.preview.value = { kind: 'failed' };
    await flushPromises();
    expect(wrapper.text()).toContain(editorHistoryCopy.en.previewFailed);
    controller.preview.value = { kind: 'gone' };
    await flushPromises();
    expect(wrapper.text()).toContain(editorHistoryCopy.en.versionGone);
  });
});

void vi;
