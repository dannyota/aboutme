import { mount } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import HeldWriteAlert from '../../app/components/editor/HeldWriteAlert.vue';
// eslint-disable-next-line max-len
import type { ResumeEditorActions } from '../../app/composables/useResumeEditor';
import type { AtomicEditorCommand } from '../../app/editor/commands';
import { editorShellCopy } from '../../app/i18n/editor-shell';
import {
  createRecord,
  type AttemptState,
  type ResumeRecord,
} from '../../app/stores/resumeRecord';
import { setSiteLocale } from '../support/locale';
import { acceptedFixture } from './fixture';

const command = {
  id: 'command-1',
  kind: 'personalField',
  path: 'headline',
} as unknown as AtomicEditorCommand;

function recordWith(attempt: AttemptState | null): ResumeRecord {
  return { ...createRecord(acceptedFixture()), attempt };
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

function mountAlert(record: ResumeRecord) {
  const retry = vi.fn();
  const actions = { retry } as unknown as ResumeEditorActions;
  const wrapper = mount(HeldWriteAlert, { props: { actions, record } });
  return { retry, wrapper };
}

beforeEach(() => {
  setSiteLocale('en');
});

describe('the held-write alert', () => {
  it.each([
    ['no write held', null],
    ['an unknown write before its alert', held({
      kind: 'unknown', reason: 'transport',
    })],
    ['an unknown write past its retry window', held({
      kind: 'unknown', reason: 'cutoff',
    })],
    ['a write refused for another reason', held({
      kind: 'failed', reason: 'request_invalid',
    })],
  ] as const)('is absent for %s', (_name, attempt) => {
    const { wrapper } = mountAlert(recordWith(attempt));

    expect(wrapper.find('[data-held-write]').exists()).toBe(false);
  });

  it('asks to retry now once an unknown write has raised its alert',
    async () => {
      const { retry, wrapper } = mountAlert(recordWith(held({
        kind: 'unknown', reason: 'transport', alert: true,
      })));
      const banner = wrapper.get('[data-held-write="unknown"]');

      expect(banner.attributes('role')).toBe('alert');
      expect(banner.text()).toContain(editorShellCopy.en.unknownWriteTitle);
      expect(banner.text()).toContain(
        editorShellCopy.en.unknownWriteDescription,
      );
      await banner.get('[data-action="retry-held-write"]').trigger('click');
      expect(banner.text()).toContain(editorShellCopy.en.retryNow);
      expect(retry).toHaveBeenCalledWith('command-1');
    });

  it('offers Try again once a write has used every rebase', async () => {
    const { retry, wrapper } = mountAlert(recordWith(held({
      kind: 'failed', reason: 'stale-limit',
    })));
    const banner = wrapper.get('[data-held-write="stale"]');

    expect(banner.text()).toContain(editorShellCopy.en.staleWriteTitle);
    expect(banner.text()).toContain(editorShellCopy.en.staleWriteDescription);
    expect(banner.text()).toContain(editorShellCopy.en.tryAgain);
    await banner.get('[data-action="retry-held-write"]').trigger('click');
    expect(retry).toHaveBeenCalledWith('command-1');
  });
});
