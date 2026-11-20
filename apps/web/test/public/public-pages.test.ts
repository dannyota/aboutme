import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  clampPageIndex,
  createPagesController,
  PAGES_TIMEOUT_MS,
  pageProgress,
  pageScale,
  type PagesState,
} from '../../app/public/publicPages';

const frames: FrameRequestCallback[] = [];
const timers = {
  setTimeout: (callback: () => void, ms: number) =>
    setTimeout(callback, ms) as unknown as number,
  clearTimeout: (id: number) => clearTimeout(id),
  requestAnimationFrame: (callback: FrameRequestCallback) => {
    frames.push(callback);
    return frames.length;
  },
  cancelAnimationFrame: (id: number) => {
    frames[id - 1] = () => undefined;
  },
};

const runFrame = (): void => {
  const pending = frames.splice(0);
  for (const callback of pending) callback(0);
};

interface Harness {
  readonly states: PagesState[];
  readonly pages: [number, boolean][];
  within: boolean;
  readonly controller: ReturnType<typeof createPagesController>;
}

const harness = (): Harness => {
  const value = {
    states: [] as PagesState[],
    pages: [] as [number, boolean][],
    within: true,
  } as Harness & { within: boolean };
  const controller = createPagesController({
    withinFirstPage: () => value.within,
    onState: (state) => value.states.push(state),
    onPages: (count, update) => value.pages.push([count, update]),
    timers,
  });
  return Object.assign(value, { controller });
};

const fonts = () => {
  let resolve!: () => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<void>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
};

beforeEach(() => {
  vi.useFakeTimers();
  frames.splice(0);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('page scale', () => {
  it('fits the page inside the content width minus 32 px, never above 1',
    () => {
      expect(pageScale(1_440, 794)).toBe(1);
      expect(pageScale(826, 794)).toBe(1);
      expect(pageScale(700, 794)).toBeCloseTo(668 / 794, 10);
      expect(pageScale(390, 794)).toBeCloseTo(358 / 794, 10);
      expect(pageScale(390, 816)).toBeCloseTo(358 / 816, 10);
      expect(pageScale(848, 816)).toBe(1);
    });

  it('never returns a negative or non-finite scale', () => {
    expect(pageScale(20, 794)).toBe(0);
    expect(pageScale(Number.NaN, 794)).toBe(1);
    expect(pageScale(390, 0)).toBe(1);
  });
});

describe('page index', () => {
  it('clamps an index to the page count', () => {
    expect(clampPageIndex(2, 2)).toBe(1);
    expect(clampPageIndex(-1, 3)).toBe(0);
    expect(clampPageIndex(1, 3)).toBe(1);
    expect(clampPageIndex(4, 0)).toBe(0);
  });

  it('measures progress as the pages reached', () => {
    expect(pageProgress(0, 2)).toBe(0.5);
    expect(pageProgress(1, 2)).toBe(1);
    expect(pageProgress(2, 5)).toBe(0.6);
    expect(pageProgress(1, 5)).toBe(0.4);
    expect(pageProgress(0, 0)).toBe(0);
  });
});

describe('pages controller', () => {
  it('waits for fonts, stages, and swaps one frame after settled', async () => {
    const run = harness();
    const font = fonts();
    run.controller.load(font.promise);
    expect(run.controller.state()).toBe('loading');
    font.resolve();
    await vi.advanceTimersByTimeAsync(0);
    expect(run.controller.state()).toBe('staging');
    run.controller.settled(2);
    expect(run.controller.state()).toBe('staging');
    runFrame();
    expect(run.controller.state()).toBe('shown');
    expect(run.states).toEqual(['staging', 'shown']);
    expect(run.pages).toEqual([[2, false]]);
  });

  it('keeps the continuous article for a reader past the first page',
    async () => {
      const run = harness();
      run.controller.load(Promise.resolve());
      await vi.advanceTimersByTimeAsync(0);
      run.controller.settled(3);
      run.within = false;
      runFrame();
      expect(run.controller.state()).toBe('continuous');
      expect(run.pages).toEqual([]);
    });

  it('times out 3 s after mount and fonts and never swaps late', async () => {
    const run = harness();
    expect(PAGES_TIMEOUT_MS).toBe(3_000);
    run.controller.load(Promise.resolve());
    await vi.advanceTimersByTimeAsync(PAGES_TIMEOUT_MS - 1);
    expect(run.controller.state()).toBe('staging');
    await vi.advanceTimersByTimeAsync(1);
    expect(run.controller.state()).toBe('continuous');
    run.controller.settled(2);
    runFrame();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(run.controller.state()).toBe('continuous');
    expect(run.pages).toEqual([]);
  });

  it('starts no timer before the fonts load', async () => {
    const run = harness();
    const font = fonts();
    run.controller.load(font.promise);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(run.controller.state()).toBe('loading');
    font.resolve();
    await vi.advanceTimersByTimeAsync(PAGES_TIMEOUT_MS - 1);
    expect(run.controller.state()).toBe('staging');
    await vi.advanceTimersByTimeAsync(1);
    expect(run.controller.state()).toBe('continuous');
  });

  it('falls back on a failure or a font error', async () => {
    const run = harness();
    run.controller.load(Promise.resolve());
    await vi.advanceTimersByTimeAsync(0);
    run.controller.failed();
    expect(run.controller.state()).toBe('continuous');
    runFrame();
    expect(run.controller.state()).toBe('continuous');

    const other = harness();
    other.controller.load(Promise.reject(new Error('font')));
    await vi.advanceTimersByTimeAsync(0);
    expect(other.controller.state()).toBe('continuous');
  });

  it('cancels a pending swap when the run fails before the frame',
    async () => {
      const run = harness();
      run.controller.load(Promise.resolve());
      await vi.advanceTimersByTimeAsync(0);
      run.controller.settled(2);
      run.controller.failed();
      runFrame();
      expect(run.controller.state()).toBe('continuous');
    });

  it('keeps the last pages during a live update and reports the update',
    async () => {
      const run = harness();
      run.controller.load(Promise.resolve());
      await vi.advanceTimersByTimeAsync(0);
      run.controller.settled(2);
      runFrame();
      run.controller.load(Promise.resolve());
      expect(run.controller.state()).toBe('updating');
      await vi.advanceTimersByTimeAsync(PAGES_TIMEOUT_MS - 1);
      expect(run.controller.state()).toBe('updating');
      run.controller.settled(3);
      expect(run.controller.state()).toBe('shown');
      expect(run.pages).toEqual([[2, false], [3, true]]);
      await vi.advanceTimersByTimeAsync(10_000);
      expect(run.controller.state()).toBe('shown');
    });

  it('shows the new continuous article when a live update times out',
    async () => {
      const run = harness();
      run.controller.load(Promise.resolve());
      await vi.advanceTimersByTimeAsync(0);
      run.controller.settled(2);
      runFrame();
      run.controller.load(Promise.resolve());
      await vi.advanceTimersByTimeAsync(PAGES_TIMEOUT_MS);
      expect(run.controller.state()).toBe('continuous');
      run.controller.settled(2);
      expect(run.controller.state()).toBe('continuous');
    });

  it('times out a live update from its start when the fonts never load',
    async () => {
      const run = harness();
      run.controller.load(Promise.resolve());
      await vi.advanceTimersByTimeAsync(0);
      run.controller.settled(2);
      runFrame();
      const font = fonts();
      run.controller.load(font.promise);
      expect(run.controller.state()).toBe('updating');
      await vi.advanceTimersByTimeAsync(PAGES_TIMEOUT_MS - 1);
      expect(run.controller.state()).toBe('updating');
      await vi.advanceTimersByTimeAsync(1);
      expect(run.controller.state()).toBe('continuous');
      font.resolve();
      await vi.advanceTimersByTimeAsync(PAGES_TIMEOUT_MS);
      expect(run.controller.state()).toBe('continuous');
    });

  it('keeps the live update timer from its start when the fonts load late',
    async () => {
      const run = harness();
      run.controller.load(Promise.resolve());
      await vi.advanceTimersByTimeAsync(0);
      run.controller.settled(2);
      runFrame();
      const font = fonts();
      run.controller.load(font.promise);
      await vi.advanceTimersByTimeAsync(PAGES_TIMEOUT_MS - 1_000);
      font.resolve();
      await vi.advanceTimersByTimeAsync(1_000);
      expect(run.controller.state()).toBe('continuous');
    });

  it('shows the new continuous article when a live update fails',
    async () => {
      const run = harness();
      run.controller.load(Promise.resolve());
      await vi.advanceTimersByTimeAsync(0);
      run.controller.settled(2);
      runFrame();
      run.controller.load(Promise.resolve());
      run.controller.failed();
      expect(run.controller.state()).toBe('continuous');
    });

  it('tries pagination again on a later live update', async () => {
    const run = harness();
    run.controller.load(Promise.resolve());
    await vi.advanceTimersByTimeAsync(0);
    run.controller.failed();
    run.controller.load(Promise.resolve());
    expect(run.controller.state()).toBe('loading');
    await vi.advanceTimersByTimeAsync(0);
    expect(run.controller.state()).toBe('staging');
    run.controller.settled(1);
    runFrame();
    expect(run.controller.state()).toBe('shown');
  });

  it('ignores a font load from a superseded update', async () => {
    const run = harness();
    const first = fonts();
    run.controller.load(first.promise);
    run.controller.load(Promise.resolve());
    await vi.advanceTimersByTimeAsync(0);
    expect(run.controller.state()).toBe('staging');
    run.controller.settled(1);
    runFrame();
    first.reject(new Error('stale'));
    await vi.advanceTimersByTimeAsync(PAGES_TIMEOUT_MS);
    expect(run.controller.state()).toBe('shown');
  });

  it('arms no timer when the run settled before the fonts promise',
    async () => {
      const run = harness();
      run.controller.load(Promise.resolve());
      await vi.advanceTimersByTimeAsync(0);
      run.controller.settled(2);
      runFrame();
      const font = fonts();
      run.controller.load(font.promise);
      run.controller.settled(2);
      font.resolve();
      await vi.advanceTimersByTimeAsync(PAGES_TIMEOUT_MS * 2);
      expect(run.controller.state()).toBe('shown');
    });

  it('stops its timers when disposed', async () => {
    const run = harness();
    run.controller.load(Promise.resolve());
    await vi.advanceTimersByTimeAsync(0);
    run.controller.dispose();
    await vi.advanceTimersByTimeAsync(PAGES_TIMEOUT_MS);
    runFrame();
    expect(run.controller.state()).toBe('staging');
  });
});
