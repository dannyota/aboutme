import {
  computed, getCurrentScope, onScopeDispose, ref, watch,
  type ComputedRef, type Ref,
} from 'vue';

import type { ProjectAuth, ProjectsApi } from '../../composables/projectsApi';
import { projectPrivacyRequired } from '../../editor/projectLinks';
import type { ResumeSummary } from '../../editor/resumeApi';
import type { Project } from './projectDocument';
import { createProjectLinker, type ProjectLinker } from './projectLinks';

export type ProjectResumesView
  = | { readonly kind: 'loading' }
    | { readonly kind: 'ready'; readonly items: readonly ResumeSummary[] }
    | { readonly kind: 'unavailable' };

/** The last Add to this resume, by the design's outcome table. */
export type LinkStatus
  = | {
    readonly kind: 'linked' | 'document-invalid' | 'busy';
    readonly resume: ResumeSummary;
  }
  | { readonly kind: 'try-again' | 'session-lost' | 'project-not-found' };

export interface ProjectResumesDeps {
  readonly api: Pick<ProjectsApi, 'read'>;
  readonly auth: ProjectAuth;
  readonly linker?: ProjectLinker;
}

export interface ProjectResumesController {
  readonly view: Ref<ProjectResumesView>;
  readonly linked: ComputedRef<ReadonlySet<string>>;
  /** The resume a link is being sent to. */
  readonly pending: Ref<string | null>;
  readonly status: Ref<LinkStatus | null>;
  /** The published resume waiting for the owner's privacy answer. */
  readonly asking: Ref<string | null>;
  retry(): Promise<void>;
  add(resumeId: string): Promise<void>;
  addAnyway(): Promise<void>;
  cancelAsk(): void;
}

/**
 * The project editor's Resumes section. Adding links at once, as a resume
 * write; it waits while the project has unsaved changes so the prefill uses
 * the text on screen. Links go to the account that loaded the project.
 */
export function useProjectResumes(
  loaded: Readonly<Ref<Project | null>>,
  dirty: Readonly<Ref<boolean>>,
  deps: ProjectResumesDeps,
): ProjectResumesController {
  const linker = deps.linker ?? createProjectLinker({ auth: deps.auth });
  const view = ref<ProjectResumesView>({ kind: 'loading' });
  const linkedIds = ref<readonly string[]>([]);
  const pending = ref<string | null>(null);
  const status = ref<LinkStatus | null>(null);
  const asking = ref<string | null>(null);
  const linked = computed<ReadonlySet<string>>(() => new Set(linkedIds.value));
  let disposed = false;
  let ownerId: string | null = null;
  if (getCurrentScope() !== undefined) {
    onScopeDispose(() => {
      disposed = true;
    });
  }

  async function load(): Promise<void> {
    if (view.value.kind !== 'ready') view.value = { kind: 'loading' };
    const result = await linker.resumes();
    if (disposed) return;
    view.value = result.kind === 'ready'
      ? { kind: 'ready', items: result.items }
      : { kind: 'unavailable' };
  }

  watch(() => loaded.value?.linkedResumeIds, (ids) => {
    linkedIds.value = ids ?? [];
  }, { immediate: true });

  watch(() => loaded.value?.id, (id) => {
    if (id === undefined || ownerId !== null) return;
    ownerId = deps.auth.user.value?.id ?? null;
    void load();
  }, { immediate: true });

  async function reload(projectId: string): Promise<void> {
    const [read] = await Promise.all([deps.api.read(projectId), load()]);
    if (!disposed && read.kind === 'ok') {
      linkedIds.value = read.value.linkedResumeIds ?? [];
    }
  }

  function addable(resumeId: string): ResumeSummary | undefined {
    const resume = view.value.kind === 'ready'
      ? view.value.items.find((item) => item.id === resumeId)
      : undefined;
    return dirty.value
      || pending.value !== null
      || loaded.value === null
      || linked.value.has(resumeId)
      ? undefined
      : resume;
  }

  async function add(resumeId: string): Promise<void> {
    const resume = addable(resumeId);
    if (resume === undefined || asking.value !== null) return;
    status.value = null;
    pending.value = resumeId;
    const project = loaded.value!;
    const [resumes, fresh] = await Promise.all([
      linker.resumes(), deps.api.read(project.id),
    ]);
    pending.value = null;
    if (disposed || dirty.value || loaded.value?.id !== project.id) return;
    if (fresh.kind === 'ok') {
      linkedIds.value = fresh.value.linkedResumeIds ?? [];
    }
    if (resumes.kind === 'ready') {
      view.value = { kind: 'ready', items: resumes.items };
    }
    if (linked.value.has(resumeId)) return;
    if (projectPrivacyRequired(
      resumeId,
      fresh.kind === 'ok' ? fresh.value.linkedResumeIds ?? [] : null,
      resumes.kind === 'ready' ? resumes.items : null,
    )) {
      asking.value = resumeId;
      return;
    }
    await send(resume);
  }

  async function addAnyway(): Promise<void> {
    const resumeId = asking.value;
    asking.value = null;
    const resume = resumeId === null ? undefined : addable(resumeId);
    if (resume !== undefined) await send(resume, true);
  }

  async function send(
    resume: ResumeSummary, privacyConfirmed = false,
  ): Promise<void> {
    const project = loaded.value!;
    const resumeId = resume.id;
    if (ownerId === null) {
      status.value = { kind: 'session-lost' };
      return;
    }
    pending.value = resumeId;
    const result = privacyConfirmed
      ? await linker.link(ownerId, resumeId, project.id, true)
      : await linker.link(ownerId, resumeId, project.id);
    pending.value = null;
    if (disposed) return;
    if (result.kind === 'ok') {
      linkedIds.value = [...linkedIds.value, resumeId];
      status.value = { kind: 'linked', resume };
      return;
    }
    switch (result.failure) {
      case 'privacy-required':
        asking.value = resumeId;
        return;
      case 'already-linked':
        await reload(project.id);
        return;
      case 'document-invalid':
      case 'busy':
        status.value = { kind: result.failure, resume };
        return;
      case 'session-lost':
      case 'project-not-found':
        status.value = { kind: result.failure };
        return;
      case 'resume-not-found':
        await load();
        status.value = { kind: 'try-again' };
        return;
      default:
        status.value = { kind: 'try-again' };
    }
  }

  return {
    view,
    linked,
    pending,
    status,
    asking,
    retry: load,
    add,
    addAnyway,
    cancelAsk: () => {
      asking.value = null;
    },
  };
}
