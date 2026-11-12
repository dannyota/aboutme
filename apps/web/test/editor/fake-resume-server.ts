import type { Resume } from '@aboutme/schema';
import { vi } from 'vitest';

import type {
  AttemptResult,
  FrozenAttempt,
  ResumeReadResult,
} from '../../app/editor/attempt';
import type { AtomicCommandIntent } from '../../app/editor/commands';
import { parentETag, parseRevision } from '../../app/editor/revision';
import type { AcceptedResume } from '../../app/editor/types';
import { acceptedFixture } from './fixture';

export type ReadFailure = Exclude<ResumeReadResult, { kind: 'complete' }>;

export const headline = (value: string): AtomicCommandIntent => ({
  kind: 'personalField',
  path: 'headline',
  value: { present: true, value },
});

export const fullName = (value: string): AtomicCommandIntent => ({
  kind: 'personalField',
  path: 'fullName',
  value: { present: true, value },
});

export function startingResume(): AcceptedResume {
  const base = acceptedFixture();
  return {
    ...base,
    document: {
      ...base.document,
      personalDetails: {
        ...base.document.personalDetails,
        photo: { key: 'photo-1' },
      },
      content: {
        work: {
          sectionType: 'work',
          entries: [
            { id: 'entry-1', jobTitle: 'Engineer' },
            { id: 'entry-2', jobTitle: 'Lead' },
          ],
        },
      },
      customization: {
        ...base.document.customization,
        layout: {
          ...base.document.customization.layout,
          sections: { main: ['work'], sidebar: [] },
        },
      },
    },
  };
}

/** How a write's response is lost on the way back to the editor. */
export interface LostResponse {
  /** Whether the server had already applied the write. */
  readonly applied: boolean;
}

/** Writes after this many never answer, so a client that loops cannot hang. */
const WRITE_LIMIT = 200;

/**
 * The owner API over one stored resume. It checks If-Match as the server
 * does, answers a stale write with the stored document and revision, and
 * applies the writes these scenarios send. A key it already applied replays
 * that result without running the write again when the request is the same,
 * and refuses it when the If-Match or body differ (ADR 0006).
 */
export function fakeServer(initial: AcceptedResume) {
  let stored = structuredClone(initial);
  const failedReads: ReadFailure[] = [];
  const heldResponses: Promise<void>[] = [];
  let readsHeld: Promise<void> | undefined;
  const lostResponses: LostResponse[] = [];
  let everyResponseLost: LostResponse | undefined;
  const beforeWrites: (() => void)[] = [];
  const failedWrites: AttemptResult[] = [];
  const replays = new Map<
    string,
    { readonly request: string; readonly result: AttemptResult }
  >();
  const sent: {
    attempt: FrozenAttempt;
    result: AttemptResult['kind'];
    at: number;
    /** What the server fingerprints for the request. */
    request: string;
  }[] = [];
  const readTimes: number[] = [];

  const advance = (document: Resume): void => {
    stored = {
      ...stored,
      document: structuredClone(document),
      revision: parseRevision(String(Number(stored.revision) + 1)),
    };
  };
  const apply = (attempt: FrozenAttempt): AttemptResult => {
    const document = stored.document;
    switch (attempt.operation) {
      case 'updateResumePersonalDetails': {
        if (attempt.payload.kind !== 'json') throw new Error('no body');
        const body = JSON.parse(attempt.payload.utf8);
        const photo = document.personalDetails.photo;
        advance({
          ...document,
          personalDetails: photo === undefined ? body : { ...body, photo },
        });
        return { kind: 'complete', status: 200, accepted: copy() };
      }
      case 'upsertResumeEntry': {
        if (attempt.payload.kind !== 'json') throw new Error('no body');
        const { entry } = JSON.parse(attempt.payload.utf8);
        const sectionKey = decodeURIComponent(attempt.url.split('/').at(-1)!);
        const section = document.content[sectionKey]!;
        const others = section.entries.filter(({ id }) => id !== entry.id);
        const at = section.entries.findIndex(({ id }) => id === entry.id);
        const entries = at === -1
          ? [...section.entries, entry]
          : [...others.slice(0, at), entry, ...others.slice(at)];
        advance({
          ...document,
          content: {
            ...document.content,
            [sectionKey]: { ...section, entries },
          },
        });
        return { kind: 'complete', status: 200, accepted: copy() };
      }
      case 'deleteResumeEntry': {
        const entryId = decodeURIComponent(attempt.url.split('/').at(-1)!);
        const work = document.content.work!;
        advance({
          ...document,
          content: {
            work: {
              ...work,
              entries: work.entries.filter((entry) => entry.id !== entryId),
            },
          },
        });
        return childAck('entry');
      }
      case 'linkResumeProject':
        return { kind: 'rejected', code: 'project_not_found' };
      case 'updateResumeMetadata': {
        if (attempt.payload.kind !== 'json') throw new Error('no body');
        advance(document);
        stored = {
          ...stored,
          metadata: { ...stored.metadata, ...JSON.parse(attempt.payload.utf8) },
        };
        return { kind: 'complete', status: 200, accepted: copy() };
      }
      case 'uploadResumePhoto': {
        advance({
          ...document,
          personalDetails: {
            ...document.personalDetails,
            photo: { key: `photo-upload-${Number(stored.revision) + 1}` },
          },
        });
        return { kind: 'complete', status: 201, accepted: copy() };
      }
      case 'deleteResume':
        return { kind: 'resume-deleted', status: 204 };
      case 'deleteResumePhoto': {
        const { photo: _photo, ...personalDetails } = document.personalDetails;
        advance({ ...document, personalDetails });
        return childAck('photo');
      }
      default:
        throw new Error(`unexpected operation ${attempt.operation}`);
    }
  };
  const childAck = (scope: 'entry' | 'photo'): AttemptResult => ({
    kind: 'child-ack',
    status: 204,
    scope,
    etag: parentETag(stored.revision),
  });
  const copy = (): AcceptedResume => structuredClone(stored);
  /** What the server fingerprints: the target, If-Match and body. */
  const requestOf = (attempt: FrozenAttempt, body: string): string =>
    JSON.stringify([
      attempt.method,
      attempt.url,
      attempt.ifMatch ?? 'absent',
      body,
    ]);
  /** A photo's raw bytes are its body. */
  const photoBody = async (file: File): Promise<string> =>
    `photo:${Buffer.from(await file.arrayBuffer()).toString('base64')}`;
  const answer = (attempt: FrozenAttempt, request: string): AttemptResult => {
    const replay = replays.get(attempt.idempotencyKey);
    if (replay !== undefined) {
      return replay.request === request
        ? replay.result
        : { kind: 'idempotency-reuse' };
    }
    if (attempt.ifMatch !== parentETag(stored.revision)) {
      return {
        kind: 'stale',
        status: 412,
        winner: {
          document: structuredClone(stored.document),
          revision: stored.revision,
        },
      };
    }
    const result = apply(attempt);
    if (
      result.kind === 'complete'
      || result.kind === 'child-ack'
      || result.kind === 'resume-deleted'
    ) {
      replays.set(attempt.idempotencyKey, { request, result });
    }
    return result;
  };

  const api = {
    dispatch: vi.fn(async (attempt: FrozenAttempt): Promise<AttemptResult> => {
      if (sent.length >= WRITE_LIMIT) return new Promise<never>(() => {});
      const { payload } = attempt;
      let body: string = payload.kind;
      if (payload.kind === 'json') body = payload.utf8;
      // Only a photo's bytes are read asynchronously.
      if (payload.kind === 'photo') body = await photoBody(payload.file);
      const request = requestOf(attempt, body);
      const record = (result: AttemptResult['kind']): void => {
        sent.push({ attempt, result, at: Date.now(), request });
      };
      beforeWrites.shift()?.();
      const lost = lostResponses.shift() ?? everyResponseLost;
      if (lost?.applied === false) {
        record('unknown');
        return { kind: 'unknown', reason: 'transport' };
      }
      const failed = lost === undefined ? failedWrites.shift() : undefined;
      if (failed !== undefined) {
        record(failed.kind);
        return failed;
      }
      const result = answer(attempt, request);
      if (lost !== undefined) {
        record('unknown');
        return { kind: 'unknown', reason: 'transport' };
      }
      record(result.kind);
      const held = heldResponses.shift();
      if (held !== undefined) await held;
      return result;
    }),
    list: vi.fn(async () => ({
      kind: 'ready' as const,
      items: [{ ...stored.metadata, revision: stored.revision }],
    })),
    read: vi.fn(async (): Promise<ResumeReadResult> => {
      readTimes.push(Date.now());
      if (readsHeld !== undefined) await readsHeld;
      return failedReads.shift() ?? { kind: 'complete', accepted: copy() };
    }),
  };

  return {
    api,
    sent,
    get stored() {
      return copy();
    },
    get reads() {
      return readTimes.length;
    },
    /** Milliseconds between consecutive reads. */
    get readGaps() {
      return readTimes.slice(1).map((time, index) => time - readTimes[index]!);
    },
    failNextRead(failure: ReadFailure): void {
      failedReads.push(failure);
    },
    /** Holds every read until the returned function is called. */
    holdReads(): () => void {
      let release = (): void => {};
      readsHeld = new Promise<void>((resolve) => {
        release = () => {
          readsHeld = undefined;
          resolve();
        };
      });
      return () => release();
    },
    /**
     * Applies the next write at once but holds its response until the
     * returned function is called.
     */
    holdNextResponse(): () => void {
      let release = (): void => {};
      heldResponses.push(new Promise<void>((resolve) => {
        release = resolve;
      }));
      return release;
    },
    /** The next write's response never reaches the editor. */
    loseNextResponse(lost: LostResponse): void {
      lostResponses.push(lost);
    },
    /**
     * The next write that reaches the server is refused with `result`
     * before it runs: a rate limit or a rejected CSRF token.
     */
    failNextWrite(result: AttemptResult): void {
      failedWrites.push(result);
    },
    /** Every write's response is lost until this is called again. */
    loseEveryResponse(lost: LostResponse | undefined): void {
      everyResponseLost = lost;
    },
    /** Milliseconds between the attempts sent for one command. */
    sendGapsFor(commandId: string): number[] {
      const times = sent
        .filter(({ attempt }) => attempt.id === commandId)
        .map(({ at }) => at);
      return times.slice(1).map((time, index) => time - times[index]!);
    },
    /** Runs `change` as the next write arrives, before it is checked. */
    beforeNextWrite(change: () => void): void {
      beforeWrites.push(change);
    },
    /** Another tab or agent changes the resume. */
    otherWriter(change: (document: Resume) => Resume): void {
      advance(change(structuredClone(stored.document)));
    },
    /** Attempts sent for one command, with what each got back. */
    sentFor(commandId: string): AttemptResult['kind'][] {
      return sent
        .filter(({ attempt }) => attempt.id === commandId)
        .map(({ result }) => result);
    },
  };
}

export type FakeResumeServer = ReturnType<typeof fakeServer>;
