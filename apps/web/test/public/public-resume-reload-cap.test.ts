// @vitest-environment happy-dom

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  createPublicResumeRealtime,
  type PublicResumeReadResult,
} from '../../app/public/public-resume.client';
import type { RealtimeEventSource } from '../../app/realtime/controller';

// A public-page 404 reload runs at most once per slug per window, and never
// when session storage is unavailable (docs/design/realtime.md#refresh-ladder).

class FakeEventSource implements RealtimeEventSource {
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  addEventListener(): void {}
  removeEventListener(): void {}
  close(): void {}
  emitOpen(): void {
    this.onopen?.();
  }
}

async function notFoundOnce(slug: string, reload: () => void): Promise<void> {
  document.body.innerHTML =
    '<main id="public-resume" data-revision="1"><h1>Ada</h1></main>';
  const source = new FakeEventSource();
  const read = async (): Promise<PublicResumeReadResult> => ({
    kind: 'not-found',
  });
  const realtime = createPublicResumeRealtime({
    root: document.querySelector<HTMLElement>('#public-resume')!,
    slug,
    revision: '1',
    read,
    reload,
    eventSourceFactory: () => source,
  });
  realtime.start();
  source.emitOpen();
  for (let i = 0; i < 10; i += 1) await Promise.resolve();
  realtime.stop();
}

beforeEach(() => {
  window.sessionStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  window.sessionStorage.clear();
  document.body.innerHTML = '';
});

describe('public not-found reload cap', () => {
  it('reloads on the first not-found and not again within the window',
    async () => {
      const reload = vi.fn();
      await notFoundOnce('ada1', reload);
      await notFoundOnce('ada1', reload);
      expect(reload).toHaveBeenCalledTimes(1);
    });

  it('caps each slug separately', async () => {
    const reload = vi.fn();
    await notFoundOnce('ada1', reload);
    await notFoundOnce('bob2', reload);
    expect(reload).toHaveBeenCalledTimes(2);
  });

  it('reloads again after the window passes', async () => {
    const reload = vi.fn();
    const now = vi.spyOn(Date, 'now').mockReturnValue(1_000_000);
    await notFoundOnce('ada1', reload);
    now.mockReturnValue(1_000_000 + 61_000);
    await notFoundOnce('ada1', reload);
    expect(reload).toHaveBeenCalledTimes(2);
  });

  it('does not reload when session storage throws', async () => {
    const reload = vi.fn();
    const blocked = (): never => {
      throw new Error('blocked');
    };
    vi.stubGlobal('sessionStorage', { getItem: blocked, setItem: blocked });
    await notFoundOnce('ada1', reload);
    expect(reload).not.toHaveBeenCalled();
  });
});
