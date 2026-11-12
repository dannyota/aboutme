import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { AtomicCommandIntent } from '../../app/editor/commands';
import { fullName, headline } from './fake-resume-server';
import { openEditor, type OpenEditor } from './held-write-editor';

// The write the queue chooses to send is the write it claims. An edit that
// lands while the queue waits for its CSRF token, or between any two of its
// steps, is never consumed by an older reference to the same command.

const clearHeadline: AtomicCommandIntent = {
  kind: 'personalField',
  path: 'headline',
  value: { present: false },
};

/** The headlines the server was sent, in order. */
function sentHeadlines(context: OpenEditor): unknown[] {
  return context.server.sent
    .filter(({ attempt }) =>
      attempt.operation === 'updateResumePersonalDetails')
    .map(({ attempt }) => attempt.payload.kind === 'json'
      ? JSON.parse(attempt.payload.utf8).headline
      : undefined);
}

/** A CSRF refresh that finishes only when the returned function is called. */
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

/** Lets the editor's timers run out, then checks nothing is left unsaved. */
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

describe('a field typed into at any step of the queue', () => {
  const values = ['S', 'St', 'Sta', 'Staf', 'Staff'];
  const cases = [false, true].flatMap((refresh) =>
    Array.from({ length: 24 }, (_, turns) => [refresh, turns] as const));

  it.each(cases)(
    'ends with the last value typed (token refreshed: %s, %i turns between '
    + 'edits)',
    async (refresh, turns) => {
      const context = openEditor();
      context.auth.refresh.mockImplementation(async () => {
        await Promise.resolve();
        context.token.value = 'csrf-2';
      });
      for (const value of values) {
        if (refresh) context.token.value = null;
        context.edit(headline(value));
        void context.coordinator.flush(context.id);
        for (let turn = 0; turn < turns; turn += 1) await Promise.resolve();
      }

      await expectAllSaved(context);
      expect(context.server.stored.document.personalDetails.headline)
        .toBe('Staff');
    },
  );
});
