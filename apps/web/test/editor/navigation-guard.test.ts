import { describe, expect, it } from 'vitest';

import { hasUnsafeWork } from '../../app/composables/useUnsavedNavigationGuard';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

describe('editor navigation guard', () => {
  it('allows a fully accepted resume to leave', () => {
    expect(hasUnsafeWork(record())).toBe(false);
  });

  it.each([
    'pending',
    'attempt',
    'conflict',
    'issues',
    'partial',
    'read-required',
    'session-lost',
    'opaque-photo',
  ] as const)('retains in-memory work for %s state', (state) => {
    const value = record();
    switch (state) {
      case 'pending':
        value.pending = [{ id: 'pending-1' }] as never;
        break;
      case 'attempt':
        value.attempt = { kind: 'failed' } as never;
        break;
      case 'conflict':
        value.conflicts = [{ id: 'conflict-1' }] as never;
        break;
      case 'issues':
        value.issues = { command: [{ path: 'x', code: 'invalid' }] };
        break;
      case 'partial':
        value.templateState = { kind: 'partial' } as never;
        break;
      case 'read-required':
        value.completeReadRequired = true;
        break;
      case 'session-lost':
        value.sessionLost = true;
        break;
      case 'opaque-photo':
        value.opaquePhotoOutcome = { kind: 'photo-cutoff' } as never;
        break;
    }

    expect(hasUnsafeWork(value)).toBe(true);
  });
});

function record(): ResumeRecord {
  const accepted = acceptedFixture();
  return {
    accepted,
    current: {
      document: structuredClone(accepted.document),
      metadata: structuredClone(accepted.metadata),
    },
    pending: [],
    attempt: null,
    conflicts: [],
    issues: {},
    templateState: null,
    photoRead: { kind: 'none' },
    completeReadRequired: false,
    sessionLost: false,
    opaquePhotoOutcome: null,
  };
}
