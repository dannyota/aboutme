import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import HeldWriteAlert from '../../app/components/editor/HeldWriteAlert.vue';
import type { AtomicEditorCommand } from '../../app/editor/commands';
import { editorShellCopy } from '../../app/i18n/editor-shell';
import {
  createRecord,
  type AttemptState,
  type ResumeRecord,
} from '../../app/stores/resumeRecord';
import { setSiteLocale } from '../support/locale';
import { fullName, headline } from './fake-resume-server';
import { acceptedFixture } from './fixture';
import { openEditor, type OpenEditor } from './held-write-editor';

const command = {
  id: 'command-1',
  kind: 'personalField',
  path: 'headline',
} as unknown as AtomicEditorCommand;

function recordWith(
  attempt: AttemptState | null,
  conflicts: ResumeRecord['conflicts'] = [],
): ResumeRecord {
  return { ...createRecord(acceptedFixture()), attempt, conflicts };
}

function held(
  attempt: Partial<AttemptState> & Pick<AttemptState, 'kind'>,
): AttemptState {
  return {
    queueItem: command,
    command,
    attempt: {} as never,
    ...attempt,
  } as AttemptState;
}

function mountAlert(context: OpenEditor, record = context.record()) {
  return mount(HeldWriteAlert, { props: { actions: context.actions, record } });
}

async function settle(): Promise<void> {
  await vi.advanceTimersByTimeAsync(0);
  await nextTick();
}

async function unknownForAMinute(context: OpenEditor): Promise<string> {
  context.server.loseEveryResponse({ applied: false });
  const mine = context.edit(headline('Staff engineer'));
  await vi.advanceTimersByTimeAsync(61_000);
  expect(context.record().attempt).toMatchObject({ alert: true });
  context.server.loseEveryResponse(undefined);
  return mine;
}

beforeEach(() => {
  setSiteLocale('en');
  vi.useFakeTimers();
  vi.setSystemTime(0);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('the held-write alert', () => {
  it.each([
    ['no write held', recordWith(null)],
    ['an unknown write before its alert', recordWith(held({
      kind: 'unknown', reason: 'transport',
    }))],
    ['an unknown write past its retry window', recordWith(held({
      kind: 'unknown', reason: 'cutoff',
    }))],
    ['a write refused for another reason', recordWith(held({
      kind: 'failed', reason: 'request_invalid',
    }))],
    ['a held write with a conflict', recordWith(held({
      kind: 'unknown', reason: 'transport', alert: true,
    }), [{ id: 'command-1' } as ResumeRecord['conflicts'][number]])],
  ] as const)('is absent for %s', (_name, record) => {
    const wrapper = mountAlert(openEditor(), record);

    expect(wrapper.find('[data-held-write]').exists()).toBe(false);
  });

  it('explains an unknown write once it has raised its alert', async () => {
    const context = openEditor();
    await unknownForAMinute(context);
    const banner = mountAlert(context).get('[data-held-write="unknown"]');

    expect(banner.attributes('role')).toBe('alert');
    expect(banner.text()).toContain(editorShellCopy.en.unknownWriteTitle);
    expect(banner.text()).toContain(editorShellCopy.en.unknownWriteDescription);
    expect(banner.text()).toContain(editorShellCopy.en.retryNow);
  });
});

describe('Retry now', () => {
  it('sends the held write once on two quick clicks and stays disabled '
    + 'while it is on its way', async () => {
    const context = openEditor();
    const mine = await unknownForAMinute(context);
    const sends = context.server.sentFor(mine).length;
    const release = context.server.holdNextResponse();
    const wrapper = mountAlert(context);
    const button = wrapper.get('[data-action="retry-held-write"]');

    await button.trigger('click');
    await button.trigger('click');
    await settle();

    expect(context.server.sentFor(mine)).toEqual([
      ...Array(sends).fill('unknown'), 'complete',
    ]);
    expect(button.attributes('disabled')).toBeDefined();

    release();
    await settle();

    expect(context.server.sentFor(mine)).toHaveLength(sends + 1);
    expect(wrapper.find('[data-held-write]').exists()).toBe(false);
    expect(context.store.saveStateFor(context.id)).toBe('saved');
  });

  it('sends nothing more when the automatic retry is already on its way',
    async () => {
      const context = openEditor();
      const mine = await unknownForAMinute(context);
      const sends = context.server.sentFor(mine).length;
      const release = context.server.holdNextResponse();
      const wrapper = mountAlert(context);
      const button = wrapper.get('[data-action="retry-held-write"]');

      await vi.advanceTimersByTimeAsync(1_250);
      expect(context.server.sentFor(mine)).toHaveLength(sends + 1);
      await nextTick();
      await button.trigger('click');
      void context.actions.retry(mine);
      await settle();

      expect(context.server.sentFor(mine)).toHaveLength(sends + 1);
      expect(button.attributes('disabled')).toBeDefined();

      release();
      await settle();

      expect(context.server.sentFor(mine)).toHaveLength(sends + 1);
      expect(context.store.saveStateFor(context.id)).toBe('saved');
    });
});

describe('Try again', () => {
  it('starts one cycle of rebases on two quick clicks and stays disabled '
    + 'while it is on its way', async () => {
    const context = openEditor();
    const mine = context.edit(headline('Staff engineer'));
    const later = context.edit(fullName('Ada King'));
    for (let bump = 0; bump < 4; bump += 1) {
      context.server.beforeNextWrite(() => {
        context.server.otherWriter((document) => document);
      });
    }
    await context.idle();
    expect(context.record().attempt).toMatchObject({
      kind: 'failed', reason: 'stale-limit',
    });
    const sends = context.server.sentFor(mine).length;
    const release = context.server.holdNextResponse();
    const wrapper = mountAlert(context);
    const button = wrapper
      .get('[data-held-write="stale"]')
      .get('[data-action="retry-held-write"]');
    expect(button.text()).toBe(editorShellCopy.en.tryAgain);

    await button.trigger('click');
    await button.trigger('click');
    await settle();

    expect(context.server.sentFor(mine)).toHaveLength(sends + 1);
    expect(button.attributes('disabled')).toBeDefined();

    release();
    await context.idle();

    expect(context.server.sentFor(mine).slice(sends)).toEqual(['complete']);
    expect(context.server.sentFor(later)).toEqual(['complete']);
    expect(context.store.saveStateFor(context.id)).toBe('saved');
  });
});
