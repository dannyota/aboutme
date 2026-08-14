import { CURRENT_VERSION } from '@aboutme/schema/released';
import { describe, expect, it } from 'vitest';

import { captureCommand } from '../../app/editor/commands';
import {
  freezeAttempt,
  freezeCreateAttempt,
  requestFromAttempt,
} from '../../app/editor/resumeApi';
import type { AtomicEditorCommand } from '../../app/editor/commands';
import type { AcceptedResume, EditorRuntime } from '../../app/editor/types';
import { acceptedFixture } from './fixture';

describe('frozen resume attempts', () => {
  const runtime: EditorRuntime = {
    nowEpochMs: () => 100,
    uuid: (() => {
      let id = 0;
      return () => `id-${++id}`;
    })(),
    delay: async () => {},
  };

  it('freezes metadata bytes while replay changes only CSRF', async () => {
    const accepted = acceptedFixture();
    const command = captureCommand(
      accepted,
      {
        resumeId: accepted.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        dependencyIds: [],
        intent: { kind: 'metadataField', field: 'title', value: 'Ada' },
      },
      runtime,
    );

    const attempt = freezeAttempt(command, accepted, runtime);
    const first = requestFromAttempt(attempt, 'csrf-a');
    const second = requestFromAttempt(attempt, 'csrf-b');

    expect(attempt).toMatchObject({
      operation: 'updateResumeMetadata',
      url: `/api/v1/resumes/${accepted.metadata.id}`,
      method: 'PATCH',
      ifMatch: '"r1"',
      schemaVersion: CURRENT_VERSION,
      firstDispatchAt: 100,
      retryCutoff: 82_800_100,
      automaticReplays: 0,
      staleRebases: 0,
    });
    expect(await first.text()).toBe('{"title":"Ada"}');
    expect(await second.text()).toBe('{"title":"Ada"}');
    expect(first.headers.get('Content-Type')).toBe('application/json');
    expect(first.headers.get('Idempotency-Key')).toBe(
      second.headers.get('Idempotency-Key'),
    );
    expect(first.headers.get('If-Match')).toBe('"r1"');
    expect(first.headers.get('X-Resume-Schema-Version')).toBe(
      String(CURRENT_VERSION),
    );
    expect(first.headers.get('X-CSRF-Token')).toBe('csrf-a');
    expect(second.headers.get('X-CSRF-Token')).toBe('csrf-b');
  });

  it('freezes a create JSON payload without a precondition', async () => {
    const attempt = freezeCreateAttempt(
      {
        kind: 'resumeCreate',
        id: 'create-1',
        ownerId: 'owner-1',
        sequence: 0,
        title: 'Fixture',
        lng: 'en',
      },
      runtime,
    );
    const request = requestFromAttempt(attempt, 'csrf');

    expect(attempt).toMatchObject({
      operation: 'createResume',
      url: '/api/v1/resumes',
      method: 'POST',
      payload: { kind: 'json', utf8: '{"title":"Fixture","lng":"en"}' },
    });
    expect(request.headers.get('If-Match')).toBeNull();
    expect(await request.text()).toBe('{"title":"Fixture","lng":"en"}');
  });

  it.each([
    [
      'personal details',
      {
        kind: 'personalField',
        path: 'fullName',
        value: { present: true, value: 'Grace' },
      },
      'updateResumePersonalDetails',
      '/personal-details',
      'PATCH',
      '{"fullName":"Grace","details":[]}',
    ],
    [
      'entry field',
      {
        kind: 'entryField',
        sectionKey: 'work',
        entryId: 'entry-1',
        path: 'jobTitle',
        value: { present: true, value: 'Staff' },
      },
      'upsertResumeEntry',
      '/entries/work',
      'PATCH',
      '{"entry":{"id":"entry-1","jobTitle":"Staff"}}',
    ],
    [
      'entry upsert',
      {
        kind: 'entryUpsert',
        sectionKey: 'work',
        entry: { id: 'entry-2', jobTitle: 'Principal' },
      },
      'upsertResumeEntry',
      '/entries/work',
      'PATCH',
      '{"entry":{"id":"entry-2","jobTitle":"Principal"}}',
    ],
    [
      'entry delete',
      { kind: 'entryDelete', sectionKey: 'work', entryId: 'entry-1' },
      'deleteResumeEntry',
      '/entries/work/entry-1',
      'DELETE',
      null,
    ],
    [
      'entry reorder',
      { kind: 'entryReorder', sectionKey: 'work', entryIds: ['entry-1'] },
      'updateResumeSection',
      '/sections/work',
      'PATCH',
      '{"entryOrder":["entry-1"]}',
    ],
    [
      'section metadata',
      {
        kind: 'sectionMetadata',
        sectionKey: 'work',
        change: { field: 'displayName', value: 'Work' },
      },
      'updateResumeSection',
      '/sections/work',
      'PATCH',
      '{"displayName":"Work"}',
    ],
    [
      'structure',
      {
        kind: 'structure',
        commands: [
          {
            op: 'createSection',
            key: 'projects',
            sectionType: 'project',
            column: 'main',
            index: 0,
          },
        ],
      },
      'updateResumeStructure',
      '/structure',
      'PATCH',
      [
        '{"commands":[{"op":"createSection","key":"projects",',
        '"sectionType":"project","column":"main","index":0}]}',
      ].join(''),
    ],
    [
      'customization',
      {
        kind: 'customization',
        deltas: [{ op: 'set', path: 'font.family', value: 'roboto' }],
      },
      'updateResumeCustomization',
      '/customization',
      'PATCH',
      '{"deltas":[{"op":"set","path":"font.family","value":"roboto"}]}',
    ],
    [
      'photo crop',
      { kind: 'photoCrop', crop: null },
      'updateResumePhotoCrop',
      '/photo',
      'PATCH',
      '{"crop":null}',
    ],
    [
      'photo delete',
      { kind: 'photoDelete' },
      'deleteResumePhoto',
      '/photo',
      'DELETE',
      null,
    ],
    [
      'resume delete',
      { kind: 'resumeDelete', confirmedTitle: 'Fixture' },
      'deleteResume',
      '',
      'DELETE',
      null,
    ],
  ] as const)(
    'freezes %s as the registered wire operation',
    async (_name, intent, operation, suffix, method, payload) => {
      const accepted = resumeWithWork();
      const attempt = freezeAttempt(commandFor(intent), accepted, runtime);
      const request = requestFromAttempt(attempt, 'csrf');

      expect(attempt.operation).toBe(operation);
      expect(attempt.url).toBe(
        `/api/v1/resumes/${accepted.metadata.id}${suffix}`,
      );
      expect(attempt.method).toBe(method);
      expect(request.headers.get('If-Match')).toBe('"r1"');
      expect(request.headers.get('X-Resume-Schema-Version')).toBe(
        String(CURRENT_VERSION),
      );
      expect(request.headers.get('Content-Type')).toBe(
        payload === null ? null : 'application/json',
      );
      if (payload === null) {
        expect(attempt.payload).toEqual({ kind: 'empty' });
      } else {
        expect(attempt.payload).toEqual({ kind: 'json', utf8: payload });
        expect(await request.text()).toBe(payload);
      }
    },
  );

  it('recreates multipart framing around the frozen file bytes', async () => {
    const file = new File(['raw-photo-bytes'], 'photo.png', {
      type: 'image/png',
    });
    const attempt = freezeAttempt(
      commandFor({ kind: 'photoUpload', file }),
      resumeWithWork(),
      runtime,
    );
    const first = requestFromAttempt(attempt, 'csrf-a');
    const second = requestFromAttempt(attempt, 'csrf-b');

    expect(attempt).toMatchObject({
      operation: 'uploadResumePhoto',
      method: 'POST',
    });
    expect(first.headers.get('Content-Type')).toMatch(
      /^multipart\/form-data; boundary=/,
    );
    expect(second.headers.get('Content-Type')).toMatch(
      /^multipart\/form-data; boundary=/,
    );
    expect(await (await first.formData()).get('file')?.text()).toBe(
      'raw-photo-bytes',
    );
    expect(await (await second.formData()).get('file')?.text()).toBe(
      'raw-photo-bytes',
    );
  });
});

function commandFor(intent: Record<string, unknown>): AtomicEditorCommand {
  return {
    id: 'command-1',
    resumeId: 'resume-1',
    ownerId: 'owner-1',
    sequence: 1,
    targetKey: 'target',
    base: { target: { present: false }, context: {} },
    intended: { target: { present: false }, context: {} },
    dependencyIds: [],
    ...intent,
  } as AtomicEditorCommand;
}

function resumeWithWork(): AcceptedResume {
  const accepted = acceptedFixture();
  return {
    ...accepted,
    document: {
      ...accepted.document,
      content: {
        ...accepted.document.content,
        work: {
          sectionType: 'work',
          entries: [{ id: 'entry-1' }],
        },
      },
    },
  } as AcceptedResume;
}
