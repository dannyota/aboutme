import { ref, type Ref } from 'vue';

import type { Project } from '../../projects/projectDocument';
import {
  createProjectsApi,
  type ProjectsApi,
} from '../../../composables/projectsApi';
import { projectPrivacyRequired } from '../../../editor/projectLinks';
import { createResumeApi, type ResumeApi } from '../../../editor/resumeApi';

export type ProjectLibraryView
  = | { readonly kind: 'idle' }
    | { readonly kind: 'loading' }
    | { readonly kind: 'ready'; readonly items: readonly Project[] }
    | { readonly kind: 'failed' };

export interface ProjectLibrary {
  readonly view: Ref<ProjectLibraryView>;
  load(): Promise<void>;
  find(id: string): Project | undefined;
  /** Rereads both privacy inputs before the owner chooses to link. */
  privacyRequired(projectId: string, resumeId: string): Promise<boolean>;
}

/**
 * The account's library as a project section reads it: once when the
 * section opens and again after a link. Editing never waits for it, and a
 * reread keeps the last list on screen until the new one arrives.
 */
export function createProjectLibrary(
  api: Pick<ProjectsApi, 'list'> = createProjectsApi(),
  resumes: Pick<ResumeApi, 'list'> = createResumeApi(),
): ProjectLibrary {
  const view = ref<ProjectLibraryView>({ kind: 'idle' });
  let generation = 0;

  async function load(): Promise<void> {
    generation += 1;
    const current = generation;
    if (view.value.kind !== 'ready') view.value = { kind: 'loading' };
    const result = await api.list();
    if (current !== generation) return;
    view.value = result.kind === 'ok'
      ? { kind: 'ready', items: result.value }
      : { kind: 'failed' };
  }

  return {
    view,
    load,
    find: (id) => view.value.kind === 'ready'
      ? view.value.items.find((item) => item.id === id)
      : undefined,
    async privacyRequired(projectId, resumeId) {
      const [projects, published] = await Promise.all([
        api.list(), resumes.list(),
      ]);
      const project = projects.kind === 'ok'
        ? projects.value.find((item) => item.id === projectId)
        : undefined;
      return projectPrivacyRequired(
        resumeId,
        project === undefined ? null : project.linkedResumeIds ?? [],
        published.kind === 'ready' ? published.items : null,
      );
    },
  };
}
