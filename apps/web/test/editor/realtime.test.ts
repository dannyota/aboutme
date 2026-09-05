import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

import {
  createRealtimeController,
  type RealtimeEventSource,
  type RealtimeReadResult,
  type RevisionDecision,
} from '../../app/realtime/controller';
import { ownerRevisionDecision } from '../../app/realtime/owner';
import { parseRevision } from '../../app/editor/revision';

class FakeEventSource implements RealtimeEventSource {
  readonly listeners = new Map<
    string,
    Set<(event: MessageEvent<string>) => void>
  >();

  onerror: (() => void) | null = null;
  onopen: (() => void) | null = null;
  closed = false;

  addEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ): void {
    const listeners = this.listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ): void {
    this.listeners.get(type)?.delete(listener);
  }

  close(): void {
    this.closed = true;
  }

  emitOpen(): void {
    this.onopen?.();
  }

  emitError(): void {
    this.onerror?.();
  }

  emit(type: string, data: unknown): void {
    const event = new MessageEvent(type, { data: JSON.stringify(data) });
    this.listeners.get(type)?.forEach((listener) => listener(event));
  }
}

function settled(value: RealtimeReadResult = { kind: 'updated' }) {
  return Promise.resolve(value);
}

describe('realtime refresh controller', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('accepts max revision deletion for the active owner resume', () => {
    expect(
      ownerRevisionDecision(
        {
          version: 1,
          resume_id: 'resume-1',
          revision: '9223372036854775807',
          deleted: true,
        },
        'resume-1',
        parseRevision('9223372036854775807'),
      ),
    ).toBe('accept');
  });

  function setup(
    options: {
      refetch?: (
        mode: 'unconditional' | 'conditional',
      ) => Promise<RealtimeReadResult>;
      revision?: (value: unknown) => RevisionDecision;
    } = {},
  ) {
    const source = new FakeEventSource();
    const refetch = vi.fn(options.refetch ?? (() => settled()));
    const onRevision = vi.fn(options.revision ?? (() => 'accept' as const));
    const reload = vi.fn();
    const onNotFound = vi.fn();
    const controller = createRealtimeController({
      url: '/api/v1/events',
      eventSourceFactory: () => source,
      refetch,
      onRevision,
      onUnknownVersion: reload,
      onNotFound,
    });
    return { controller, source, refetch, onRevision, reload, onNotFound };
  }

  // eslint-disable-next-line max-len -- exact regression name.
  it('repairs on initial open and every reconnect while filtering', async () => {
    const { controller, source, refetch, onRevision } = setup({
      revision: (value) => (value === 'wrong-resume' ? 'ignore' : 'accept'),
    });
    controller.start();

    source.emitOpen();
    await Promise.resolve();
    source.emit('revision', 'wrong-resume');
    source.emit('revision', { version: 1, revision: '2' });
    source.emitOpen();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    expect(onRevision).toHaveBeenCalledTimes(1);
    expect(refetch).toHaveBeenCalledTimes(2);
    expect(refetch).toHaveBeenNthCalledWith(1, 'unconditional');
    expect(refetch).toHaveBeenNthCalledWith(2, 'unconditional');
  });

  // eslint-disable-next-line max-len -- exact regression name.
  it('coalesces a burst and preserves a queued invalidation after a failed read', async () => {
    let resolveFirst!: (result: RealtimeReadResult) => void;
    const first = new Promise<RealtimeReadResult>((resolve) => {
      resolveFirst = resolve;
    });
    const { controller, source, refetch } = setup({
      refetch: vi
        .fn()
        .mockReturnValueOnce(first)
        .mockImplementation(() => settled()),
    });
    controller.start();
    source.emitOpen();
    source.emit('revision', { version: 1, revision: '2' });
    source.emit('revision', { version: 1, revision: '3' });
    expect(refetch).toHaveBeenCalledTimes(1);

    resolveFirst({ kind: 'failed' });
    await vi.runOnlyPendingTimersAsync();
    expect(refetch).toHaveBeenCalledTimes(2);
    expect(refetch).toHaveBeenLastCalledWith('unconditional');
  });

  // eslint-disable-next-line max-len -- exact regression name.
  it('retries a failed revision read while heartbeats keep the stream healthy', async () => {
    const refetch = vi
      .fn()
      .mockResolvedValueOnce({ kind: 'failed' } as const)
      .mockResolvedValue({ kind: 'updated' } as const);
    const { controller, source } = setup({ refetch });
    controller.start();
    source.emit('revision', { version: 1, revision: '2' });
    await Promise.resolve();
    await Promise.resolve();
    source.emit('heartbeat', { version: 1 });
    await vi.advanceTimersByTimeAsync(5_000);
    await Promise.resolve();
    await Promise.resolve();

    expect(refetch).toHaveBeenCalledTimes(2);
    expect(refetch).toHaveBeenLastCalledWith('unconditional');
  });

  // eslint-disable-next-line max-len -- exact regression name.
  it('retries a failed conditional read after fallback is disabled', async () => {
    const refetch = vi.fn((mode: 'unconditional' | 'conditional') =>
      settled(
        mode === 'conditional' ? { kind: 'failed' } : { kind: 'updated' },
      ),
    );
    const { controller, source } = setup({ refetch });
    controller.start();
    source.emitError();
    source.emitError();
    source.emitError();
    await Promise.resolve();
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(30_000);
    source.emit('heartbeat', { version: 1 });
    await vi.advanceTimersByTimeAsync(5_000);

    expect(refetch).toHaveBeenLastCalledWith('unconditional');
    expect(
      refetch.mock.calls.filter(([mode]) => mode === 'conditional'),
    ).toHaveLength(1);
  });

  it('falls back when EventSource construction fails', async () => {
    const refetch = vi.fn(() => settled());
    const controller = createRealtimeController({
      url: '/api/v1/events',
      eventSourceFactory: () => {
        throw new Error('unavailable');
      },
      refetch,
      onRevision: () => 'accept',
      onUnknownVersion: vi.fn(),
    });

    expect(() => controller.start()).not.toThrow();
    await Promise.resolve();
    expect(refetch).toHaveBeenCalledWith('unconditional');
    await vi.advanceTimersByTimeAsync(30_000);
    expect(refetch).toHaveBeenCalledWith('conditional');
  });

  it('closes a partly initialized EventSource before falling back', () => {
    const source = new FakeEventSource();
    source.addEventListener = () => {
      throw new Error('listener setup failed');
    };
    const controller = createRealtimeController({
      url: '/api/v1/events',
      eventSourceFactory: () => source,
      refetch: () => settled(),
      onRevision: () => 'accept',
      onUnknownVersion: vi.fn(),
    });

    controller.start();

    expect(source.closed).toBe(true);
  });

  it('resets the failure threshold when restarted', async () => {
    const first = new FakeEventSource();
    const second = new FakeEventSource();
    const sources = [first, second];
    const refetch = vi.fn(() => settled());
    const controller = createRealtimeController({
      url: '/api/v1/events',
      eventSourceFactory: () => sources.shift()!,
      refetch,
      onRevision: () => 'accept',
      onUnknownVersion: vi.fn(),
    });
    controller.start();
    first.emitError();
    first.emitError();
    first.emitError();
    controller.stop();
    controller.start();
    second.emitError();
    await vi.advanceTimersByTimeAsync(30_000);

    expect(refetch.mock.calls.some(([mode]) => mode === 'conditional')).toBe(
      false,
    );
  });

  it('falls back after errors and recovers on heartbeat', async () => {
    const { controller, source, refetch } = setup();
    controller.start();
    source.emitError();
    source.emitError();
    source.emitError();
    await vi.advanceTimersByTimeAsync(30_000);
    await Promise.resolve();
    await Promise.resolve();
    expect(refetch).toHaveBeenLastCalledWith('conditional');
    const callsBeforeHeartbeat = refetch.mock.calls.length;
    source.emit('heartbeat', { version: 1 });
    await vi.advanceTimersByTimeAsync(30_000);
    expect(refetch.mock.calls.length).toBe(callsBeforeHeartbeat);
  });

  it('starts fallback after 60 seconds of silence', async () => {
    const { controller, source, refetch } = setup();
    controller.start();
    source.emitOpen();
    await vi.runOnlyPendingTimersAsync();
    await vi.advanceTimersByTimeAsync(60_000);
    await vi.advanceTimersByTimeAsync(30_000);

    expect(refetch).toHaveBeenLastCalledWith('conditional');
  });

  // eslint-disable-next-line max-len -- exact regression name.
  it('reloads once for unknown versions and ignores ordinary read failures', async () => {
    const { controller, source, reload } = setup({
      revision: () => 'unknown',
    });
    controller.start();
    source.emit('revision', { version: 99, revision: '2' });
    source.emit('revision', { version: 99, revision: '3' });
    await vi.runOnlyPendingTimersAsync();

    expect(reload).toHaveBeenCalledTimes(1);
  });

  // eslint-disable-next-line max-len -- exact regression name.
  it('closes source, clears timers, and ignores stale results after cleanup', async () => {
    let resolveRead!: (result: RealtimeReadResult) => void;
    const pending = new Promise<RealtimeReadResult>((resolve) => {
      resolveRead = resolve;
    });
    const { controller, source, refetch, onNotFound } = setup({
      refetch: vi.fn(() => pending),
    });
    controller.start();
    source.emitOpen();
    controller.stop();
    resolveRead({ kind: 'not-found' });
    await vi.runOnlyPendingTimersAsync();
    await vi.advanceTimersByTimeAsync(90_000);

    expect(source.closed).toBe(true);
    expect(refetch).toHaveBeenCalledTimes(1);
    expect(onNotFound).not.toHaveBeenCalled();
  });
});
