import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import ConflictPanel from '../../app/components/editor/ConflictPanel.vue';
import ErrorSummary from '../../app/components/editor/ErrorSummary.vue';
import SaveStatus from '../../app/components/editor/SaveStatus.vue';
import type {
  ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import type { ConflictRecord } from '../../app/editor/reconcile';
import type { SaveState } from '../../app/editor/types';
import { acceptedFixture } from './fixture';

describe('editor accessibility boundaries', () => {
  it.each([
    'idle',
    'dirty',
    'saving',
    'saved',
    'offline',
    'error',
    'conflict',
    'session-lost',
  ] as const)('announces %s with safe non-color text', (state: SaveState) => {
    const wrapper = mount(SaveStatus, { props: { state } });

    expect(wrapper.get('[role="status"]').text()).not.toBe('');
    expect(wrapper.text()).not.toContain('sentinel raw server text');
  });

  it('focuses new validation issues without exposing their raw path',
    async () => {
      const wrapper = mount(ErrorSummary, {
        attachTo: document.body,
        props: { issues: [] },
      });

      await wrapper.setProps({
        issues: [
          {
            path: 'personalDetails.fullName',
            code: 'format',
            message: 'sentinel raw server text',
          },
        ],
      });
      await nextTick();

      expect(document.activeElement).toBe(wrapper.element);
      expect(wrapper.text()).toContain('Check this value and try again.');
      expect(wrapper.text()).not.toContain('sentinel raw server text');
      expect(wrapper.text()).not.toContain('personalDetails.fullName');
      wrapper.unmount();
    });

  it('leaves an unmapped issue visible in the focused summary', () => {
    const wrapper = mount(ErrorSummary, {
      props: {
        issues: [{ path: 'unknown.branch', code: 'unknown', message: 'raw' }],
      },
    });

    expect(wrapper.get('button').text()).toBe('This value needs attention.');
  });

  it('offers only the matching dedicated control for each conflict family',
    async () => {
      const applyMine = vi.fn();
      const acceptLatest = vi.fn();
      const conflicts = [
        conflict('target-changed', 'personalField'),
        conflict('identity-missing', 'entryField'),
        conflict('membership-changed', 'entryReorder'),
        conflict('context-changed', 'structure'),
        conflict('photo-changed', 'photoCrop'),
        templateConflict(),
        conflict('destructive-reconfirmation', 'resumeDelete'),
      ];
      const wrapper = mount(ConflictPanel, {
        props: { actions: actionsFor(applyMine, acceptLatest), conflicts },
      });

      expect(
        wrapper.get('[data-conflict="target-changed:personalField"]').text(),
      ).toContain('Apply my value');
      expect(
        wrapper.get('[data-conflict="identity-missing:entryField"]').text(),
      ).toContain('Select another entry');
      expect(
        wrapper.get('[data-conflict="membership-changed:entryReorder"]').text(),
      ).toContain('Reopen entry order');
      expect(
        wrapper.get('[data-conflict="context-changed:structure"]').text(),
      ).toContain('Reopen placement');
      expect(
        wrapper.get('[data-conflict="photo-changed:photoCrop"]').text(),
      ).toContain('Reopen crop');
      expect(wrapper.get('[data-conflict="template"]').text()).toContain(
        'Review template changes',
      );
      expect(wrapper.get('[data-conflict="template"]').text()).not.toContain(
        'Accept latest',
      );
      expect(
        wrapper
          .get('[data-conflict="destructive-reconfirmation:resumeDelete"]')
          .text(),
      ).toContain('Confirm deletion again');
      expect(wrapper.findAll('[data-action="apply-mine"]')).toHaveLength(1);

      await wrapper.get('[data-action="reopen-entry-order"]').trigger('click');
      expect(applyMine).toHaveBeenCalledWith(
        'membership-changed-entryReorder',
        { kind: 'reorder', members: ['entry-a', 'entry-b'] },
      );
      expect(wrapper.emitted('openInspector')?.at(-1)?.[0]).toEqual({
        kind: 'section',
        key: 'work',
      });
    });
});

function actionsFor(
  applyMine: ReturnType<typeof vi.fn>,
  acceptLatest: ReturnType<typeof vi.fn>,
): ResumeEditorActions {
  return {
    createEntityId: vi.fn(() => '00000000-0000-4000-8000-000000000001'),
    edit: vi.fn(() => ({ kind: 'blocked', reason: 'not-loaded' })),
    applyTemplate: vi.fn(() => ({ kind: 'no-change' })),
    undoTemplate: vi.fn(() => ({
      kind: 'unavailable',
      reason: 'state-changed',
    })),
    recoverTemplate: vi.fn(() => ({
      kind: 'unavailable',
      reason: 'state-changed',
    })),
    resolveOpaquePhoto: vi.fn(),
    retry: vi.fn(),
    acceptLatest,
    applyMine,
    resumeAfterAuth: vi.fn(),
    discard: vi.fn(),
    record: { value: undefined } as ResumeEditorActions['record'],
  };
}

function conflict(kind: string, commandKind: string): ConflictRecord {
  const latest = acceptedFixture();
  latest.document.content.work = {
    sectionType: 'work',
    entries: [{ id: 'entry-a' }, { id: 'entry-b' }],
  } as never;
  return {
    id: `${kind}-${commandKind}`,
    subject: 'atomic',
    kind,
    command: { kind: commandKind, sectionKey: 'work' },
    latest,
    latestProjection: { target: { present: false }, context: {} },
  } as unknown as ConflictRecord;
}

function templateConflict(): ConflictRecord {
  const latest = acceptedFixture();
  return {
    id: 'template',
    subject: 'template',
    kind: 'context-changed',
    group: { id: 'template' },
    latest,
    latestProjection: { target: { present: false }, context: {} },
  } as unknown as ConflictRecord;
}
