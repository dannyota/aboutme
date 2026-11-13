import { computed, type ComputedRef, inject, type InjectionKey } from 'vue';

export type ProjectLinkMap = ReadonlyMap<string, string>;

export const NO_PROJECT_LINKS: ProjectLinkMap = new Map();

export const ProjectLinksKey: InjectionKey<ComputedRef<ProjectLinkMap>>
  = Symbol('projectLinks');

export function useProjectLinks(): ComputedRef<ProjectLinkMap> {
  return inject(ProjectLinksKey, computed(() => NO_PROJECT_LINKS));
}
