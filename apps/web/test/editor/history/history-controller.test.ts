import { describe, expect, it, vi } from 'vitest';

import { createHistoryController } from '../../../app/editor/historyController';
import { acceptedFixture } from '../fixture';
import { fakeApi, fakeDeps, fakeRecord, item } from './support';

describe('history controller', () => {
  it('loads the list on open and exposes empty and failed states', async () => {
    const api = fakeApi([item('a'), item('b')]);
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.open();
    expect(controller.list.value.kind).toBe('loading');
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    expect(controller.selected.value).toBe('current');

    api.list.mockResolvedValueOnce({ kind: 'ok', value: [] });
    await controller.reload();
    expect(controller.list.value).toEqual({ kind: 'ready', items: [] });

    api.list.mockResolvedValueOnce({ kind: 'failed' } as never);
    await controller.reload();
    expect(controller.list.value.kind).toBe('ready');
    expect(controller.loadFailed.value).toBe(true);
    await controller.reload();
    expect(controller.loadFailed.value).toBe(false);
  });

  it('fails the first load without rows', async () => {
    const api = fakeApi();
    api.list.mockResolvedValue({ kind: 'failed' } as never);
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    await controller.reload();
    expect(controller.list.value.kind).toBe('failed');
  });

  it('keeps the last good rows on a later read error', async () => {
    const api = fakeApi([item('a')]);
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    api.list.mockResolvedValueOnce({ kind: 'failed' } as never);
    await controller.reload();
    expect(controller.list.value).toEqual({
      kind: 'ready',
      items: [item('a')],
    });
  });

  it('resolves a queued reload only after the re-read finished', async () => {
    const api = fakeApi([item('a')]);
    const releases: (() => void)[] = [];
    api.list.mockImplementation(
      () => new Promise((resolve) => {
        releases.push(() => resolve({ kind: 'ok', value: [item('a')] }));
      }),
    );
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    const first = controller.reload();
    let second = false;
    void controller.reload().then(() => {
      second = true;
    });
    releases[0]!();
    await vi.waitFor(() => expect(releases).toHaveLength(2));
    expect(second).toBe(false);
    releases[1]!();
    await first;
    await vi.waitFor(() => expect(second).toBe(true));
  });

  it('runs one list read at a time and rereads once afterwards', async () => {
    const api = fakeApi([item('a')]);
    let release: () => void = () => {};
    api.list.mockImplementationOnce(
      () => new Promise((resolve) => {
        release = () => resolve({ kind: 'ok', value: [item('a')] });
      }),
    );
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.open();
    void controller.reload();
    void controller.reload();
    expect(api.list).toHaveBeenCalledTimes(1);
    release();
    await vi.waitFor(() => expect(api.list).toHaveBeenCalledTimes(2));
  });

  it('marks the session lost when the list answers 401', async () => {
    const api = fakeApi();
    api.list.mockResolvedValue({ kind: 'session-lost' } as never);
    const { deps, store } = fakeDeps(api);
    const controller = createHistoryController(deps);
    await controller.reload();
    expect(store.markSessionLost).toHaveBeenCalledWith('resume-1');
  });

  it('selects a version, previews it, and shows gone on 404', async () => {
    const api = fakeApi([item('a')]);
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    await controller.select('a');
    expect(controller.selected.value).toBe('a');
    expect(controller.preview.value.kind).toBe('ready');
    expect(controller.status.value?.kind).toBe('showing');

    api.read.mockResolvedValueOnce({ kind: 'not-found' } as never);
    api.list.mockResolvedValueOnce({ kind: 'ok', value: [] });
    await controller.select('a');
    expect(controller.preview.value.kind).toBe('gone');
    expect(controller.focusRequest.value).toEqual({ kind: 'current' });
    await vi.waitFor(() => expect(controller.selected.value).toBe('current'));
  });

  it('restores with the accepted revision and adopts the result', async () => {
    const api = fakeApi([item('a')]);
    const accepted = acceptedFixture();
    api.write.mockResolvedValueOnce({
      kind: 'accepted',
      resume: accepted,
    } as never);
    const { deps, store } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    await controller.select('a');
    controller.openRestore('a');
    expect(controller.restore.value?.phase).toBe('confirm');
    await controller.confirmRestore();
    const write = api.write.mock.calls[0]![0] as never as {
      operation: string;
      revision: string;
      idempotencyKey: string;
    };
    expect(write.operation).toBe('restore');
    expect(write.revision).toBe('1');
    expect(store.adoptComplete).toHaveBeenCalledWith('resume-1', accepted);
    expect(store.setTemplateState).toHaveBeenCalledWith('resume-1', null);
    expect(controller.restore.value).toBeNull();
    expect(controller.selected.value).toBe('current');
    expect(controller.banner.value?.id).toBe('a');
  });

  it('resends the same key after a network failure', async () => {
    const api = fakeApi([item('a')]);
    api.write
      .mockResolvedValueOnce({ kind: 'network' } as never)
      .mockResolvedValueOnce({ kind: 'network' } as never);
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    expect(controller.restore.value?.phase).toBe('network');
    await controller.confirmRestore();
    const keys = api.write.mock.calls.map(
      (call) => (call[0] as never as { idempotencyKey: string })
        .idempotencyKey,
    );
    expect(keys[0]).toBe(keys[1]);
  });

  it('uses a new key after a 412 and reconciles first', async () => {
    const api = fakeApi([item('a')]);
    const winner = { document: acceptedFixture().document, revision: '9' };
    api.write.mockResolvedValueOnce({ kind: 'stale', winner } as never);
    const { deps, store, record } = fakeDeps(api);
    store.adoptStaleWinner.mockImplementation(
      (_id: string, accepted: { revision: string }) => {
        record.accepted = { ...record.accepted, revision: accepted.revision };
      },
    );
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    expect(store.adoptStaleWinner).toHaveBeenCalled();
    expect(deps.refresh).toHaveBeenCalled();
    expect(controller.restore.value?.phase).toBe('changed');
    await controller.confirmRestore();
    const revisions = api.write.mock.calls.map(
      (call) => (call[0] as never as { revision: string }).revision,
    );
    expect(revisions).toEqual(['1', '9']);
    const keys = api.write.mock.calls.map(
      (call) => (call[0] as never as { idempotencyKey: string })
        .idempotencyKey,
    );
    expect(keys[0]).not.toBe(keys[1]);
  });

  it.each([
    ['invalid', 'invalid'],
    ['not-found', 'gone'],
    ['failed', 'failed'],
  ])('maps restore %s to phase %s', async (kind, phase) => {
    const api = fakeApi([item('a')]);
    api.write.mockResolvedValueOnce({ kind } as never);
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    expect(controller.restore.value?.phase).toBe(phase);
  });

  it('holds Restore while a Retry-After runs', async () => {
    const api = fakeApi([item('a')]);
    api.write.mockResolvedValueOnce(
      { kind: 'busy', retryAfterMs: 5000 } as never,
    );
    const { deps } = fakeDeps(api);
    let wake: () => void = () => {};
    (deps.runtime as { delay: unknown }).delay = () =>
      new Promise<void>((resolve) => {
        wake = resolve;
      });
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    expect(controller.restore.value?.phase).toBe('later');
    wake();
    await vi.waitFor(() =>
      expect(controller.restore.value?.phase).toBe('retry'),
    );
  });

  it('closes the dialog and marks the session lost on 401', async () => {
    const api = fakeApi([item('a')]);
    api.write.mockResolvedValueOnce({ kind: 'session-lost' } as never);
    const { deps, store } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    expect(controller.restore.value).toBeNull();
    expect(store.markSessionLost).toHaveBeenCalled();
  });

  it('refreshes the CSRF token once on a rejection', async () => {
    const api = fakeApi([item('a')]);
    api.write
      .mockResolvedValueOnce({ kind: 'csrf-rejected' } as never)
      .mockResolvedValueOnce({ kind: 'done' } as never);
    const { deps, auth } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    await controller.select('a');
    controller.openDelete();
    await controller.confirmDelete();
    expect(auth.refresh).toHaveBeenCalledTimes(1);
    expect(api.write).toHaveBeenCalledTimes(2);
  });

  it('removes a version and focuses the next older one', async () => {
    const api = fakeApi([item('a'), item('b'), item('c')]);
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    await controller.select('b');
    api.list.mockResolvedValueOnce({
      kind: 'ok',
      value: [item('a'), item('c')],
    });
    controller.openDelete();
    await controller.confirmDelete();
    expect(controller.remove.value).toBeNull();
    expect(controller.selected.value).toBe('current');
    expect(controller.focusRequest.value).toEqual({ kind: 'option', id: 'c' });
    expect(controller.status.value?.kind).toBe('deleted');
  });

  it('keeps the delete dialog open on failure', async () => {
    const api = fakeApi([item('a')]);
    api.write.mockResolvedValueOnce({ kind: 'failed' } as never);
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    await controller.select('a');
    controller.openDelete();
    await controller.confirmDelete();
    expect(controller.remove.value?.phase).toBe('failed');
  });

  it('clears history and focuses the empty heading', async () => {
    const api = fakeApi([item('a')]);
    const { deps } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.open();
    await vi.waitFor(() => expect(controller.list.value.kind).toBe('ready'));
    api.list.mockResolvedValueOnce({ kind: 'ok', value: [] });
    controller.openClear();
    await controller.confirmClear();
    expect(controller.clear.value).toBeNull();
    expect(controller.list.value).toEqual({ kind: 'ready', items: [] });
    expect(controller.focusRequest.value).toEqual({ kind: 'empty' });
    expect(controller.status.value?.kind).toBe('cleared');
  });

  it('points Undo at the newest restore row after this restore', async () => {
    const api = fakeApi([item('a')]);
    const record = fakeRecord();
    api.write.mockResolvedValueOnce({
      kind: 'accepted',
      resume: record.accepted,
    } as never);
    api.list.mockResolvedValue({
      kind: 'ok',
      value: [
        item('new', {
          cause: 'restore',
          startedAt: new Date().toISOString(),
        }),
        item('a'),
        item('old', { cause: 'restore', startedAt: '1970-01-01T00:00:00Z' }),
      ],
    });
    const { deps } = fakeDeps(api, record);
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    expect(controller.banner.value?.undoId).toBe('new');
  });

  it('leaves Undo disabled when no restore row follows', async () => {
    const api = fakeApi([item('a')]);
    const record = fakeRecord();
    api.write.mockResolvedValueOnce({
      kind: 'accepted',
      resume: record.accepted,
    } as never);
    api.list.mockResolvedValue({
      kind: 'ok',
      value: [
        item('x', { cause: 'restore', startedAt: '1970-01-01T00:00:00Z' }),
      ],
    });
    const { deps } = fakeDeps(api, record);
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    expect(controller.banner.value?.undoId).toBeNull();
  });

  it('does not send a restore while blocked', async () => {
    const api = fakeApi([item('a')]);
    const { deps } = fakeDeps(api);
    (deps as { restoreBlocked?: () => boolean }).restoreBlocked = () => true;
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    expect(api.write).not.toHaveBeenCalled();
    expect(controller.restore.value?.phase).toBe('confirm');
  });

  it('fails without losing the session while auth loads', async () => {
    const api = fakeApi([item('a')]);
    const { deps, auth, store } = fakeDeps(api);
    auth.authState.value = 'loading';
    auth.csrfToken.value = null;
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    expect(api.write).not.toHaveBeenCalled();
    expect(store.markSessionLost).not.toHaveBeenCalled();
    expect(controller.restore.value?.phase).toBe('failed');
  });

  it('loses the session for an anonymous state', async () => {
    const api = fakeApi([item('a')]);
    const { deps, auth, store } = fakeDeps(api);
    auth.authState.value = 'anonymous';
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    expect(api.write).not.toHaveBeenCalled();
    expect(store.markSessionLost).toHaveBeenCalledWith('resume-1');
  });

  it('loses the session when the owner changes before a retry', async () => {
    const api = fakeApi([item('a')]);
    api.write.mockResolvedValueOnce({ kind: 'network' } as never);
    const { deps, auth, store } = fakeDeps(api);
    const controller = createHistoryController(deps);
    controller.openRestore('a');
    await controller.confirmRestore();
    auth.user.value = { id: 'someone-else' };
    await controller.confirmRestore();
    expect(api.write).toHaveBeenCalledTimes(1);
    expect(store.markSessionLost).toHaveBeenCalledWith('resume-1');
  });
});
