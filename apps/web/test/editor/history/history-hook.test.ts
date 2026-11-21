import { effectScope, nextTick, ref } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useEditorHistory } from '../../../app/composables/useEditorHistory';
import { createHistoryController } from '../../../app/editor/historyController';
import type { ResumeRecord } from '../../../app/stores/resumes';
import { fakeApi, fakeDeps, fakeRecord, item } from './support';

type Panel = { kind: string; key?: string };

function setup(start: Panel = { kind: 'section', key: 'work' }, wide = false) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn(() => ({ matches: wide })),
  );
  window.matchMedia = vi.fn(() => ({ matches: wide })) as never;
  const api = fakeApi([item('a'), item('b')]);
  const record = fakeRecord();
  const { deps } = fakeDeps(api, record);
  const controller = createHistoryController(deps);
  const inspector = ref<Panel>(start);
  const narrowRegion = ref<'editor' | 'preview'>('editor');
  const hook = effectScope().run(() =>
    useEditorHistory({
      inspector,
      narrowRegion,
      saveState: ref('saved'),
      record: () => record as unknown as ResumeRecord,
      sessionCheckFailing: () => false,
      actions: { refresh: vi.fn() } as never,
      controller,
    }),
  )!;
  return { api, record, controller, inspector, narrowRegion, hook };
}

function rail(action: string): HTMLButtonElement {
  const button = document.createElement('button');
  button.dataset.action = action;
  document.body.append(button);
  return button;
}

beforeEach(() => {
  document.body.innerHTML = '';
});
afterEach(() => vi.unstubAllGlobals());

describe('useEditorHistory', () => {
  it('opens History and remembers the tool before it', async () => {
    const { hook, inspector } = setup();
    hook.openHistory();
    expect(inspector.value.kind).toBe('history');
  });

  it('shows Preview and focuses the bar title when narrow', async () => {
    const { hook, inspector, controller, narrowRegion } = setup();
    const focusTitle = vi.fn();
    hook.preview.value = { focusTitle };
    hook.openHistory();
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    await controller.select('a');
    await nextTick();
    await nextTick();
    expect(inspector.value.kind).toBe('history');
    expect(narrowRegion.value).toBe('preview');
    expect(focusTitle).toHaveBeenCalled();
  });

  it('keeps the layout on a wide selection', async () => {
    const { hook, controller, narrowRegion } = setup(undefined, true);
    const focusTitle = vi.fn();
    hook.preview.value = { focusTitle };
    hook.openHistory();
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    await controller.select('a');
    await nextTick();
    expect(narrowRegion.value).toBe('editor');
    expect(focusTitle).not.toHaveBeenCalled();
  });

  it('returns to the earlier tool and focuses its rail button', async () => {
    const { hook, inspector, narrowRegion } = setup({ kind: 'structure' });
    const button = rail('open-structure');
    hook.openHistory();
    narrowRegion.value = 'preview';
    await hook.leaveHistory();
    expect(inspector.value).toEqual({ kind: 'structure' });
    expect(narrowRegion.value).toBe('editor');
    expect(document.activeElement).toBe(button);
  });

  it('falls back to personal details when the section is gone', async () => {
    const { hook, inspector, record } = setup({ kind: 'section', key: 'gone' });
    const button = rail('open-document');
    expect(record.current.document.content.gone).toBeUndefined();
    hook.openHistory();
    await hook.leaveHistory();
    expect(inspector.value).toEqual({ kind: 'personal' });
    expect(document.activeElement).toBe(button);
  });

  it('asks the list to focus the shown option on Escape', async () => {
    const { hook, controller, narrowRegion } = setup();
    hook.openHistory();
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    await controller.select('b');
    narrowRegion.value = 'preview';
    await hook.escapeHistory();
    expect(controller.selected.value).toBe('current');
    expect(narrowRegion.value).toBe('editor');
    expect(controller.focusRequest.value).toEqual({
      kind: 'option',
      id: 'b',
    });
  });

  it('switches to Edit after a restore', async () => {
    const { api, hook, controller, record, narrowRegion } = setup();
    api.write.mockResolvedValueOnce({
      kind: 'accepted',
      resume: record.accepted,
    } as never);
    hook.openHistory();
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    narrowRegion.value = 'preview';
    controller.openRestore('a');
    await controller.confirmRestore();
    await nextTick();
    expect(narrowRegion.value).toBe('editor');
  });

  it('switches to Edit when a 404 resets the selection', async () => {
    const { api, hook, controller, narrowRegion } = setup();
    hook.openHistory();
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    api.read.mockImplementationOnce(async () => {
      await nextTick();
      await nextTick();
      return { kind: 'not-found' } as never;
    });
    await controller.select('a');
    await vi.waitFor(() => expect(controller.selected.value).toBe('current'));
    await nextTick();
    expect(narrowRegion.value).toBe('editor');
  });

  it('switches to Edit when a delete resets the selection', async () => {
    const { hook, controller, narrowRegion } = setup();
    hook.openHistory();
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    await controller.select('a');
    await nextTick();
    expect(narrowRegion.value).toBe('preview');
    controller.openDelete();
    await controller.confirmDelete();
    await nextTick();
    expect(narrowRegion.value).toBe('editor');
  });
});
