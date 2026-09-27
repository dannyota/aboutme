import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  JOIN_INVITE_STORAGE_KEY,
  isJoinInviteClosed,
  joinInvitePlacement,
  recordJoinInviteClosed,
  startJoinInviteTiming,
} from '../../app/public/joinInvite';

// Timing and placement (docs/design/viewer-analytics/sign-in-to-view.md
// #join-invite; docs/design/public-page-theme.md, "Join invite").

class FakeDoc extends EventTarget {
  visibilityState: 'visible' | 'hidden' = 'visible';
  documentElement = { scrollHeight: 10_000 };
}

class FakeWindow extends EventTarget {
  innerWidth = 1_440;
  innerHeight = 800;
  scrollY = 0;
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('startJoinInviteTiming', () => {
  it('never shows before the 5 s floor, even at full scroll', async () => {
    const doc = new FakeDoc();
    const win = new FakeWindow();
    win.scrollY = 9_200; // scrollable = 10000 - 800 = 9200, fraction 1
    const onShow = vi.fn();
    startJoinInviteTiming(onShow, {
      window: win as unknown as Window,
      document: doc as unknown as Document,
    });
    win.dispatchEvent(new Event('scroll'));
    await vi.advanceTimersByTimeAsync(4_999);
    expect(onShow).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(onShow).toHaveBeenCalledTimes(1);
  });

  it('shows at 60 percent scroll after the floor', async () => {
    const doc = new FakeDoc();
    const win = new FakeWindow();
    const onShow = vi.fn();
    startJoinInviteTiming(onShow, {
      window: win as unknown as Window,
      document: doc as unknown as Document,
    });
    await vi.advanceTimersByTimeAsync(5_000);
    win.scrollY = 9_200 * 0.59;
    win.dispatchEvent(new Event('scroll'));
    expect(onShow).not.toHaveBeenCalled();
    win.scrollY = 9_200 * 0.6;
    win.dispatchEvent(new Event('scroll'));
    expect(onShow).toHaveBeenCalledTimes(1);
  });

  it('shows after 20 s of visible time without scrolling', async () => {
    const doc = new FakeDoc();
    const win = new FakeWindow();
    const onShow = vi.fn();
    startJoinInviteTiming(onShow, {
      window: win as unknown as Window,
      document: doc as unknown as Document,
    });
    await vi.advanceTimersByTimeAsync(19_999);
    expect(onShow).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(onShow).toHaveBeenCalledTimes(1);
  });

  it('does not count hidden time toward the 20 s dwell', async () => {
    const doc = new FakeDoc();
    const win = new FakeWindow();
    const onShow = vi.fn();
    startJoinInviteTiming(onShow, {
      window: win as unknown as Window,
      document: doc as unknown as Document,
    });
    await vi.advanceTimersByTimeAsync(5_000);
    doc.visibilityState = 'hidden';
    doc.dispatchEvent(new Event('visibilitychange'));
    await vi.advanceTimersByTimeAsync(30_000);
    expect(onShow).not.toHaveBeenCalled();
    doc.visibilityState = 'visible';
    doc.dispatchEvent(new Event('visibilitychange'));
    await vi.advanceTimersByTimeAsync(14_999);
    expect(onShow).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(onShow).toHaveBeenCalledTimes(1);
  });

  it('shows only once and stops listening after it fires', async () => {
    const doc = new FakeDoc();
    const win = new FakeWindow();
    const onShow = vi.fn();
    startJoinInviteTiming(onShow, {
      window: win as unknown as Window,
      document: doc as unknown as Document,
    });
    await vi.advanceTimersByTimeAsync(20_000);
    expect(onShow).toHaveBeenCalledTimes(1);
    win.scrollY = 9_200;
    win.dispatchEvent(new Event('scroll'));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(onShow).toHaveBeenCalledTimes(1);
  });

  it('stop() cancels timers and listeners before it fires', async () => {
    const doc = new FakeDoc();
    const win = new FakeWindow();
    const onShow = vi.fn();
    const stop = startJoinInviteTiming(onShow, {
      window: win as unknown as Window,
      document: doc as unknown as Document,
    });
    stop();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(onShow).not.toHaveBeenCalled();
  });
});

describe('joinInvitePlacement', () => {
  function root(right: number): Element {
    return {
      getBoundingClientRect: () => ({ right }),
    } as unknown as Element;
  }

  it('uses the bar below the 1024 px card floor', () => {
    const win = { innerWidth: 1_023 } as unknown as Window;
    expect(joinInvitePlacement(root(500), win)).toBe('bar');
  });

  it('uses the card at 1024 px or wider with 360 px of right margin', () => {
    const win = { innerWidth: 1_440 } as unknown as Window;
    expect(joinInvitePlacement(root(1_440 - 360), win)).toBe('card');
  });

  it('uses the bar when the right margin is under 360 px', () => {
    const win = { innerWidth: 1_440 } as unknown as Window;
    expect(joinInvitePlacement(root(1_440 - 359), win)).toBe('bar');
  });
});

describe('close persistence', () => {
  function storage(initial: Record<string, string> = {}) {
    const map = new Map(Object.entries(initial));
    return {
      getItem: (key: string) => map.get(key) ?? null,
      setItem: (key: string, value: string) => {
        map.set(key, value);
      },
      map,
    };
  }

  it('is not closed with no stored value', () => {
    expect(isJoinInviteClosed(storage())).toBe(false);
  });

  it('records the close time and reads it back as closed', () => {
    const store = storage();
    const now = () => 1_000;
    recordJoinInviteClosed(store, now);
    expect(store.map.get(JOIN_INVITE_STORAGE_KEY)).toBe('1000');
    expect(isJoinInviteClosed(store, now)).toBe(true);
  });

  it('expires after 90 days', () => {
    const store = storage({ [JOIN_INVITE_STORAGE_KEY]: '0' });
    const ninetyDaysMs = 90 * 24 * 60 * 60 * 1_000;
    expect(isJoinInviteClosed(store, () => ninetyDaysMs - 1)).toBe(true);
    expect(isJoinInviteClosed(store, () => ninetyDaysMs)).toBe(false);
  });

  it('treats a malformed stored value as not closed', () => {
    const store = storage({ [JOIN_INVITE_STORAGE_KEY]: 'not-a-number' });
    expect(isJoinInviteClosed(store)).toBe(false);
  });

  it('never throws when storage access fails', () => {
    const throwing = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    expect(isJoinInviteClosed(throwing)).toBe(false);
    expect(() => recordJoinInviteClosed(throwing)).not.toThrow();
  });
});
