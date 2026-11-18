import {
  computed, getCurrentScope, onScopeDispose, ref, watch,
  type ComputedRef, type Ref,
} from 'vue';

import type {
  AttachBody,
  EvidenceFailure,
  EvidenceResult,
  EvidenceWritten,
  ProjectEvidenceApi,
} from '../../composables/projectEvidenceApi';
import {
  sendWithCsrf,
  type ProjectAuth,
  type ProjectsApi,
} from '../../composables/projectsApi';
import type { ProjectEditorController } from '../../composables/useProjects';
import {
  canonicalJSON,
  EVIDENCE_LIMIT,
  requestFromProject,
  type Project,
  type ProjectEvidence,
} from './projectDocument';

export type EvidenceNotice
  = | { readonly kind: 'rate-limited'; readonly until: number | null }
    | {
      readonly kind:
        | 'not-found'
        | 'duplicate'
        | 'limit'
        | 'size'
        | 'upstream'
        | 'not-confirmed'
        | 'unsupported-link'
        | 'try-again'
        | 'evidence-unavailable'
        | 'csrf-rejected'
        | 'session-lost'
        | 'failed';
    };

export type AttachOutcome
  = | { readonly kind: 'attached' | 'refused' | 'session-ended' | 'closed' }
    | { readonly kind: 'failed'; readonly notice: EvidenceNotice };

export interface ProjectEvidenceDeps {
  readonly api: ProjectEvidenceApi;
  readonly projects: Pick<ProjectsApi, 'read'>;
  readonly auth: ProjectAuth;
  readonly uuid?: () => string;
  readonly now?: () => number;
  readonly onSessionEnded?: () => void;
}

export type EvidenceEditor = Pick<
  ProjectEditorController,
  'view' | 'loaded' | 'saving' | 'notice' | 'adoptEvidence' | 'markStale'
>;

export interface ProjectEvidenceController {
  readonly available: ComputedRef<boolean>;
  readonly items: ComputedRef<readonly ProjectEvidence[]>;
  readonly locked: ComputedRef<boolean>;
  readonly atLimit: ComputedRef<boolean>;
  readonly pending: Ref<string | null>;
  readonly status: Ref<EvidenceNotice | null>;
  attach(body: AttachBody): Promise<AttachOutcome>;
  move(id: string, step: -1 | 1): Promise<void>;
  recheck(id: string): Promise<void>;
  remove(id: string): Promise<void>;
}

type Send = (
  revision: string,
  key: string,
  csrfToken: string,
) => Promise<EvidenceResult<EvidenceWritten>>;

type Intent
  = | { readonly op: 'attach'; readonly body: AttachBody }
    | { readonly op: 'order'; readonly order: readonly string[] }
    | { readonly op: 'recheck' | 'remove'; readonly id: string };

const RETRYABLE = new Set<EvidenceFailure>([
  'csrf-rejected', 'rate-limited', 'unavailable', 'upstream', 'busy',
  'session-lost',
]);

const PLAIN_NOTICES = new Set<EvidenceFailure>([
  'limit', 'size', 'upstream', 'not-confirmed', 'unsupported-link',
  'evidence-unavailable', 'csrf-rejected',
]);

const CLOSED = { kind: 'closed' } as const;

function sameStored(left: Project, right: Project): boolean {
  return canonicalJSON(requestFromProject(left))
    === canonicalJSON(requestFromProject(right));
}

function failedWith(notice: EvidenceNotice): AttachOutcome {
  return { kind: 'failed', notice };
}

export function useProjectEvidence(
  editor: EvidenceEditor,
  changed: Readonly<Ref<boolean>>,
  deps: ProjectEvidenceDeps,
): ProjectEvidenceController {
  const uuid = deps.uuid ?? (() => crypto.randomUUID());
  const now = deps.now ?? (() => Date.now());
  const pending = ref<string | null>(null);
  const status = ref<EvidenceNotice | null>(null);
  const keys = new Map<string, string>();
  let ownerId: string | null = null;
  let disposed = false;
  if (getCurrentScope() !== undefined) {
    onScopeDispose(() => {
      disposed = true;
    });
  }

  const available = computed(() =>
    editor.loaded.value?.evidence !== undefined);
  const items = computed<readonly ProjectEvidence[]>(() =>
    editor.loaded.value?.evidence ?? []);
  const locked = computed(() => changed.value
    || editor.saving.value
    || editor.notice.value === 'changed-elsewhere');
  const atLimit = computed(() => items.value.length >= EVIDENCE_LIMIT);

  watch(() => editor.loaded.value?.id, (id) => {
    if (id !== undefined && ownerId === null) {
      ownerId = deps.auth.user.value?.id ?? null;
    }
  }, { immediate: true });

  async function reread(notice: EvidenceNotice): Promise<AttachOutcome> {
    const project = editor.loaded.value;
    if (project === null) return CLOSED;
    const read = await deps.projects.read(project.id);
    const current = editor.loaded.value;
    if (disposed || current === null) return CLOSED;
    if (read.kind === 'ok') {
      if (!sameStored(read.value, current)) {
        editor.markStale(read.value);
        return CLOSED;
      }
      editor.adoptEvidence(read.value.evidence ?? [], read.value.revision);
      return failedWith(notice);
    }
    if (read.kind === 'failed' && read.failure === 'not-found') {
      editor.view.value = 'not-found';
      return CLOSED;
    }
    return failedWith({ kind: 'try-again' });
  }

  async function settle(
    result: EvidenceResult<EvidenceWritten>
      | { readonly kind: 'failed'; readonly failure: 'session-lost' },
    intent: Intent,
  ): Promise<AttachOutcome> {
    if (result.kind === 'ok') {
      editor.adoptEvidence(result.value.evidence, result.value.projectRevision);
      return { kind: 'attached' };
    }
    if (result.kind === 'stale') {
      editor.markStale(result.project);
      return CLOSED;
    }
    const failure = result.failure;
    switch (failure) {
      case 'project-not-found':
        editor.view.value = 'not-found';
        return CLOSED;
      case 'session-lost':
        editor.notice.value = 'session-lost';
        return CLOSED;
      case 'session-required':
        return { kind: 'session-ended' };
      case 'evidence-not-found':
        return reread({ kind: 'not-found' });
      case 'duplicate':
        return reread({ kind: 'duplicate' });
      case 'invalid':
        return intent.op === 'order'
          ? reread({ kind: 'try-again' })
          : failedWith({ kind: 'failed' });
      case 'rate-limited': {
        const seconds = 'retryAfter' in result ? result.retryAfter : null;
        return failedWith({
          kind: 'rate-limited',
          until: seconds === null ? null : now() + seconds * 1000,
        });
      }
      case 'busy':
      case 'unavailable':
        return failedWith({ kind: 'try-again' });
      default:
        return failedWith(PLAIN_NOTICES.has(failure)
          ? { kind: failure } as EvidenceNotice
          : { kind: 'failed' });
    }
  }

  async function write(
    target: string,
    intent: Intent,
    send: (projectId: string) => Send,
  ): Promise<AttachOutcome> {
    const project = editor.loaded.value;
    if (
      project === null
      || !available.value
      || locked.value
      || pending.value !== null
    ) return { kind: 'refused' };
    if (ownerId === null) {
      editor.notice.value = 'session-lost';
      return CLOSED;
    }
    const revision = project.revision;
    const fingerprint = canonicalJSON({
      ownerId, projectId: project.id, revision, intent,
    });
    const key = keys.get(fingerprint) ?? uuid();
    keys.set(fingerprint, key);
    const sendOnce = send(project.id);
    pending.value = target;
    status.value = null;
    try {
      const result = await sendWithCsrf(deps.auth, ownerId, (csrfToken) =>
        sendOnce(revision, key, csrfToken));
      if (result.kind !== 'failed' || !RETRYABLE.has(result.failure)) {
        keys.delete(fingerprint);
      }
      if (disposed) return CLOSED;
      return await settle(result, intent);
    } finally {
      pending.value = null;
    }
  }

  async function sectionWrite(
    target: string,
    intent: Intent,
    send: (projectId: string) => Send,
  ): Promise<void> {
    const outcome = await write(target, intent, send);
    if (outcome.kind === 'failed') status.value = outcome.notice;
    if (outcome.kind === 'session-ended') deps.onSessionEnded?.();
  }

  return {
    available,
    items,
    locked,
    atLimit,
    pending,
    status,
    attach: (body) => write('attach', { op: 'attach', body }, (projectId) =>
      (revision, key, csrfToken) =>
        deps.api.attach(projectId, revision, body, key, csrfToken)),
    move: async (id, step) => {
      const order = items.value.map((item) => item.id);
      const index = order.indexOf(id);
      const next = index + step;
      if (index === -1 || next < 0 || next >= order.length) return;
      order[index] = order[next]!;
      order[next] = id;
      await sectionWrite(id, { op: 'order', order }, (projectId) =>
        (revision, key, csrfToken) =>
          deps.api.reorder(projectId, revision, order, key, csrfToken));
    },
    recheck: (id) => sectionWrite(id, { op: 'recheck', id }, (projectId) =>
      (revision, key, csrfToken) =>
        deps.api.recheck(projectId, id, revision, key, csrfToken)),
    remove: (id) => sectionWrite(id, { op: 'remove', id }, (projectId) =>
      (revision, key, csrfToken) =>
        deps.api.remove(projectId, id, revision, key, csrfToken)),
  };
}
