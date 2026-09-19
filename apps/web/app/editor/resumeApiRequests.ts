import { CURRENT_VERSION } from '@aboutme/schema/released';

import type { operations } from '../api/generated/openapi';
import type { AtomicEditorCommand, CreateResumeIntent } from './commands';
import { applyIntent } from './commands';
import { parentETag } from './revision';
import type { FrozenAttempt } from './attempt';
import type { AcceptedResume, EditorRuntime, ParentETag } from './types';

type JsonBody<Operation extends keyof operations>
  = operations[Operation] extends {
    requestBody: { content: { 'application/json': infer Body } };
  }
    ? Body
    : never;

type Wire = Readonly<{
  operation: FrozenAttempt['operation'];
  url: string;
  method: FrozenAttempt['method'];
  body?: unknown;
  file?: File;
}>;

const RETRY_WINDOW_MS = 23 * 60 * 60 * 1000;

export function freezeAttempt(
  command: AtomicEditorCommand,
  accepted: AcceptedResume,
  runtime: EditorRuntime,
): FrozenAttempt {
  const wire = wireForCommand(command, accepted);
  return freezeWire(command.id, wire, parentETag(accepted.revision), runtime);
}

export function freezeCreateAttempt(
  intent: CreateResumeIntent,
  runtime: EditorRuntime,
): FrozenAttempt {
  const body: JsonBody<'createResume'> = {
    title: intent.title,
    ...(intent.lng === undefined ? {} : { lng: intent.lng }),
    ...(intent.document === undefined
      ? {}
      : { document: intent.document as JsonBody<'createResume'>['document'] }),
  };
  return freezeWire(
    intent.id,
    {
      operation: 'createResume',
      url: '/api/v1/resumes',
      method: 'POST',
      body,
    },
    undefined,
    runtime,
  );
}

export function requestFromAttempt(
  attempt: FrozenAttempt,
  csrfToken: string,
): Request {
  const headers = new Headers({
    'Idempotency-Key': attempt.idempotencyKey,
    'X-CSRF-Token': csrfToken,
    'X-Resume-Schema-Version': String(attempt.schemaVersion),
  });
  if (attempt.ifMatch !== undefined) headers.set('If-Match', attempt.ifMatch);

  let body: BodyInit | undefined;
  if (attempt.payload.kind === 'json') {
    body = attempt.payload.utf8;
  } else if (attempt.payload.kind === 'photo') {
    const form = new FormData();
    form.append('file', attempt.payload.file);
    body = form;
  }

  const request = new Request(attempt.url, {
    method: attempt.method,
    body,
    cache: 'no-store',
    credentials: 'include',
  });
  for (const [name, value] of headers) request.headers.set(name, value);
  if (attempt.payload.kind === 'json') {
    request.headers.set('Content-Type', 'application/json');
  }
  return request;
}

function freezeWire(
  id: string,
  wire: Wire,
  ifMatch: ParentETag | undefined,
  runtime: EditorRuntime,
): FrozenAttempt {
  const firstDispatchAt = runtime.nowEpochMs();
  const payload
    = wire.file === undefined
      ? wire.body === undefined
        ? Object.freeze({ kind: 'empty' as const })
        : Object.freeze({
            kind: 'json' as const,
            utf8: JSON.stringify(wire.body),
          })
      : Object.freeze({ kind: 'photo' as const, file: wire.file });
  return Object.freeze({
    id,
    operation: wire.operation,
    url: wire.url,
    method: wire.method,
    schemaVersion: CURRENT_VERSION,
    ...(ifMatch === undefined ? {} : { ifMatch }),
    idempotencyKey: runtime.uuid(),
    payload,
    firstDispatchAt,
    retryCutoff: firstDispatchAt + RETRY_WINDOW_MS,
    automaticReplays: 0 as const,
    staleRebases: 0 as const,
  });
}

function wireForCommand(
  command: AtomicEditorCommand,
  accepted: AcceptedResume,
): Wire {
  const base = `/api/v1/resumes/${encodeURIComponent(command.resumeId)}`;
  const updated = applyIntent(accepted, command);
  switch (command.kind) {
    case 'metadataField':
      return {
        operation: 'updateResumeMetadata',
        url: base,
        method: 'PATCH',
        body: {
          [command.field]: command.value,
        } as JsonBody<'updateResumeMetadata'>,
      };
    case 'personalField': {
      const { photo: _photo, ...personalDetails }
        = updated.document.personalDetails;
      return {
        operation: 'updateResumePersonalDetails',
        url: `${base}/personal-details`,
        method: 'PATCH',
        body: personalDetails as JsonBody<'updateResumePersonalDetails'>,
      };
    }
    case 'entryField':
    case 'entryUpsert': {
      const section = updated.document.content[command.sectionKey];
      const entryId
        = command.kind === 'entryField' ? command.entryId : command.entry.id;
      const entry = section?.entries.find(
        (candidate) => candidate.id === entryId,
      );
      if (entry === undefined) throw new Error('entry missing after command');
      return {
        operation: 'upsertResumeEntry',
        url: `${base}/entries/${encodeURIComponent(command.sectionKey)}`,
        method: 'PATCH',
        body: { entry } as unknown as JsonBody<'upsertResumeEntry'>,
      };
    }
    case 'entryDelete': {
      const sectionKey = encodeURIComponent(command.sectionKey);
      const entryId = encodeURIComponent(command.entryId);
      return {
        operation: 'deleteResumeEntry',
        url: `${base}/entries/${sectionKey}/${entryId}`,
        method: 'DELETE',
      };
    }
    case 'entryReorder':
      return {
        operation: 'updateResumeSection',
        url: `${base}/sections/${encodeURIComponent(command.sectionKey)}`,
        method: 'PATCH',
        body: {
          entryOrder: command.entryIds,
        } as JsonBody<'updateResumeSection'>,
      };
    case 'sectionMetadata':
      return {
        operation: 'updateResumeSection',
        url: `${base}/sections/${encodeURIComponent(command.sectionKey)}`,
        method: 'PATCH',
        body: {
          [command.change.field]: command.change.value,
        } as JsonBody<'updateResumeSection'>,
      };
    case 'structure':
      return {
        operation: 'updateResumeStructure',
        url: `${base}/structure`,
        method: 'PATCH',
        body: {
          commands: command.commands,
        } as JsonBody<'updateResumeStructure'>,
      };
    case 'customization':
      return {
        operation: 'updateResumeCustomization',
        url: `${base}/customization`,
        method: 'PATCH',
        body: {
          deltas: command.deltas,
        } as JsonBody<'updateResumeCustomization'>,
      };
    case 'photoUpload':
      return {
        operation: 'uploadResumePhoto',
        url: `${base}/photo`,
        method: 'POST',
        file: command.file,
      };
    case 'photoCrop':
      return {
        operation: 'updateResumePhotoCrop',
        url: `${base}/photo`,
        method: 'PATCH',
        body: { crop: command.crop } as JsonBody<'updateResumePhotoCrop'>,
      };
    case 'photoDelete':
      return {
        operation: 'deleteResumePhoto',
        url: `${base}/photo`,
        method: 'DELETE',
      };
    case 'resumeDelete':
      return { operation: 'deleteResume', url: base, method: 'DELETE' };
    default:
      return assertNever(command);
  }
}

function assertNever(value: never): never {
  throw new Error(`unhandled command: ${String(value)}`);
}
