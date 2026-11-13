import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { AtomicCommandIntent } from '../../app/editor/commands';
import { fullName, headline } from './fake-resume-server';
import { openEditor, type OpenEditor } from './held-write-editor';

const clearHeadline: AtomicCommandIntent = {
  kind: 'personalField',
  path: 'headline',
  value: { present: false },
};

function sentHeadlines(context: OpenEditor): unknown[] {
  return context.server.sent
    .filter(({ attempt }) =>
      attempt.operation === 'updateResumePersonalDetails')
    .map(({ attempt }) => attempt.payload.kind === 'json'
      ? JSON.parse(attempt.payload.utf8).headline
      : undefined);
}

function slowRefresh(context: OpenEditor): () => void {
  let finish = (): void => {};
  context.token.value = null;
  context.auth.refresh.mockImplementation(() => new Promise<void>(
    (resolve) => {
      finish = () => {
        context.token.value = 'csrf-2';
        resolve();
      };
    },
  ));
  return () => finish();
}

async function expectAllSaved(context: OpenEditor): Promise<void> {
  await context.idle();
  expect(context.record()).toMatchObject({ attempt: null, pending: [] });
  expect(context.store.saveStateFor(context.id)).toBe('saved');
  expect(context.server.sent.map(({ result }) => result))
    .not.toContain('idempotency-reuse');
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(0);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('text typed while the queue chooses what to send', () => {
  it('is sent when it lands during the CSRF refresh', async () => {
    const context = openEditor();
    const finishRefresh = slowRefresh(context);
    context.edit(headline('Staff'));
    const flushing = context.coordinator.flush(context.id);
    context.edit(headline('Staff engineer'));
    finishRefresh();
    await flushing;

    expect(sentHeadlines(context)).toEqual(['Staff engineer']);
    await expectAllSaved(context);
    expect(context.server.stored.document.personalDetails.headline)
      .toBe('Staff engineer');
  });

  it('is sent when it lands while a cached token is read', async () => {
    const context = openEditor();
    context.edit(headline('Staff'));
    const flushing = context.coordinator.flush(context.id);
    context.edit(headline('Staff engineer'));
    await flushing;

    await expectAllSaved(context);
    expect(sentHeadlines(context).at(-1)).toBe('Staff engineer');
    expect(context.server.stored.document.personalDetails.headline)
      .toBe('Staff engineer');
  });

  it.each([
    ['the same field cleared', clearHeadline, undefined, 'Ada Lovelace'],
    ['another field set', fullName('Ada King'), 'Staff', 'Ada King'],
  ] as const)('keeps %s while the text waits for its token', async (
    _case,
    change,
    headlineAfter,
    fullNameAfter,
  ) => {
    const context = openEditor();
    const finishRefresh = slowRefresh(context);
    context.edit(headline('Staff'));
    const flushing = context.coordinator.flush(context.id);
    context.edit(change);
    finishRefresh();
    await flushing;

    await expectAllSaved(context);
    const stored = context.server.stored.document.personalDetails;
    expect(stored.headline).toBe(headlineAfter);
    expect(stored.fullName).toBe(fullNameAfter);
  });
});

function seeded(seed: number): () => number {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let mixed = Math.imul(state ^ (state >>> 15), state | 1);
    mixed ^= mixed + Math.imul(mixed ^ (mixed >>> 7), mixed | 61);
    return ((mixed ^ (mixed >>> 14)) >>> 0) / 4_294_967_296;
  };
}

describe('a field typed into at any step of the queue', () => {
  const values = ['S', 'St', 'Sta', 'Staf', 'Staff'];
  const cases = Array.from({ length: 36 }, (_, seed) => {
    const next = seeded(seed + 1);
    return [
      seed + 1,
      values.map(() => ({
        turns: Math.floor(next() * 24),
        refresh: next() < 0.5,
      })),
    ] as const;
  });

  it.each(cases)('ends with the last value typed (seed %i)', async (
    _seed,
    keystrokes,
  ) => {
    const context = openEditor();
    context.auth.refresh.mockImplementation(async () => {
      await Promise.resolve();
      context.token.value = 'csrf-2';
    });
    for (const [index, { turns, refresh }] of keystrokes.entries()) {
      if (refresh) context.token.value = null;
      context.edit(headline(values[index]!));
      void context.coordinator.flush(context.id);
      for (let turn = 0; turn < turns; turn += 1) await Promise.resolve();
    }

    await expectAllSaved(context);
    expect(context.server.stored.document.personalDetails.headline)
      .toBe('Staff');
  });
});
