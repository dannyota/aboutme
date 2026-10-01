import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  JOIN_INVITE_STORAGE_KEY,
  isJoinInviteClosed,
  JOIN_INVITE_BAR_HEIGHT,
  JOIN_INVITE_BAR_TEXT_MIN_WIDTH,
  JOIN_INVITE_ENTRANCE,
  joinInviteContainerStyle,
  joinInviteIsDark,
  joinInvitePalette,
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
      querySelector: () => null,
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

describe('joinInvitePlacement measures the resume box', () => {
  // The public page root spans the viewport, so its right edge leaves no
  // margin; the resume's own box (.public-measure) is what sits inside it.
  function page(measureRight: number, viewport: number): Element {
    const measure = {
      getBoundingClientRect: () => ({ right: measureRight }),
    };
    return {
      getBoundingClientRect: () => ({ right: viewport }),
      querySelector: (selector: string) =>
        selector === '.public-measure' ? measure : null,
    } as unknown as Element;
  }

  it('uses the card when the resume box leaves 360 px on the right', () => {
    const win = { innerWidth: 1_920 } as unknown as Window;
    expect(joinInvitePlacement(page(1_920 - 360, 1_920), win)).toBe('card');
  });

  it('uses the bar when the resume box leaves under 360 px', () => {
    const win = { innerWidth: 1_920 } as unknown as Window;
    expect(joinInvitePlacement(page(1_920 - 359, 1_920), win)).toBe('bar');
  });

  it('falls back to the root when it holds no resume box', () => {
    const win = { innerWidth: 1_440 } as unknown as Window;
    const bare = {
      getBoundingClientRect: () => ({ right: 1_440 - 360 }),
      querySelector: () => null,
    } as unknown as Element;
    expect(joinInvitePlacement(bare, win)).toBe('card');
  });
});

describe('joinInviteContainerStyle', () => {
  // DESIGN.md, "Join invite": light column of the page bar's tokens.
  it('draws the card 320 px wide, 16 px from the bottom right', () => {
    const style = joinInviteContainerStyle('card');
    expect(style).toMatchObject({
      position: 'fixed',
      right: '16px',
      bottom: '16px',
      boxSizing: 'border-box',
      width: '320px',
      padding: '16px',
      borderRadius: '14px',
      border: '1px solid #E5E1D6',
      background: '#FFFFFF',
      color: '#5C6178',
      boxShadow: '0 1px 2px rgba(16, 27, 63, 0.06)',
    });
  });

  it('draws the bar as a 56 px band plus the bottom safe area', () => {
    expect(JOIN_INVITE_BAR_HEIGHT)
      .toBe('calc(56px + env(safe-area-inset-bottom))');
    const style = joinInviteContainerStyle('bar');
    expect(style).toMatchObject({
      position: 'fixed',
      left: '0',
      right: '0',
      bottom: '0',
      boxSizing: 'border-box',
      height: JOIN_INVITE_BAR_HEIGHT,
      display: 'flex',
      alignItems: 'center',
      gap: '8px',
      borderTop: '1px solid #E5E1D6',
      background: '#FFFFFF',
      color: '#5C6178',
    });
    expect(String(style.padding)).toBe(
      '0 calc(12px + env(safe-area-inset-right)) '
      + 'env(safe-area-inset-bottom) calc(12px + env(safe-area-inset-left))',
    );
    expect(style).not.toHaveProperty('boxShadow');
  });

  it('hides the bar text below 360 px and eases in over 200 ms', () => {
    expect(JOIN_INVITE_BAR_TEXT_MIN_WIDTH).toBe(360);
    expect(JOIN_INVITE_ENTRANCE.options.duration).toBe(200);
    expect(JOIN_INVITE_ENTRANCE.keyframes[0]).toEqual({
      opacity: 0,
      transform: 'translateY(8px)',
    });
  });
});

describe('join invite color scheme', () => {
  // docs/design/public-page-theme.md, "Page bar > Tokens": the invite takes
  // the bar's dark column on a dark page.
  it('takes the dark column of the bar tokens', () => {
    expect(joinInvitePalette(true)).toEqual({
      surface: '#141A2E',
      border: 'rgba(230, 225, 210, 0.13)',
      text: '#A5ABBF',
      shadow: 'none',
      action: '#72A0FF',
      onAction: '#071126',
    });
    expect(joinInvitePalette(false)).toEqual({
      surface: '#FFFFFF',
      border: '#E5E1D6',
      text: '#5C6178',
      shadow: '0 1px 2px rgba(16, 27, 63, 0.06)',
      action: '#1A5CEB',
      onAction: '#FFFFFF',
    });
  });

  it('draws the dark card and bar with the dark tokens', () => {
    expect(joinInviteContainerStyle('card', true)).toMatchObject({
      border: '1px solid rgba(230, 225, 210, 0.13)',
      background: '#141A2E',
      color: '#A5ABBF',
      boxShadow: 'none',
    });
    expect(joinInviteContainerStyle('bar', true)).toMatchObject({
      borderTop: '1px solid rgba(230, 225, 210, 0.13)',
      background: '#141A2E',
      color: '#A5ABBF',
    });
  });

  it('keeps the light style when no scheme is passed', () => {
    expect(joinInviteContainerStyle('card')).toEqual(
      joinInviteContainerStyle('card', false),
    );
    expect(joinInviteContainerStyle('bar')).toEqual(
      joinInviteContainerStyle('bar', false),
    );
  });

  describe('reading the page scheme', () => {
    const page = (scheme: string | null): Element => {
      const root = document.createElement('main');
      const inner = document.createElement('div');
      inner.className = 'public-resume-page';
      if (scheme !== null) inner.setAttribute('data-color-scheme', scheme);
      root.append(inner);
      return root;
    };
    const win = (matches: boolean) => ({
      matchMedia: (query: string) => ({
        matches: matches && query === '(prefers-color-scheme: dark)',
      }),
    }) as unknown as Window;

    it('is dark for a dark page whatever the device prefers', () => {
      expect(joinInviteIsDark(page('dark'), win(false))).toBe(true);
    });

    it('follows the device for a Match device page', () => {
      expect(joinInviteIsDark(page('system'), win(true))).toBe(true);
      expect(joinInviteIsDark(page('system'), win(false))).toBe(false);
    });

    it('is light for a light page, a missing page, or another value', () => {
      expect(joinInviteIsDark(page(null), win(true))).toBe(false);
      expect(joinInviteIsDark(page('light'), win(true))).toBe(false);
      expect(joinInviteIsDark(page('bogus'), win(true))).toBe(false);
      expect(joinInviteIsDark(document.createElement('main'), win(true)))
        .toBe(false);
    });
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
