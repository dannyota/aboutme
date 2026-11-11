<script setup lang="ts">
import { Plus } from '@lucide/vue';
import { nextTick } from 'vue';

import StatusBanner from '@/components/app/StatusBanner.vue';
import ProjectCreateDialog
  from '@/components/projects/ProjectCreateDialog.vue';
import ProjectList from '@/components/projects/ProjectList.vue';
import ProjectReleaseDialog
  from '@/components/projects/ProjectReleaseDialog.vue';
import {
  PROJECT_LIMIT,
  type Project,
} from '@/components/projects/projectDocument';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import {
  createProjectsApi,
  createReleaseAction,
  type ReleaseKind,
} from '@/composables/projectsApi';
import { useProjectList } from '@/composables/useProjects';
import { workspaceTitles } from '@/i18n/meta';
import { projectsCopy } from '@/i18n/projects';

const locale = useRouteLocale();
const copy = computed(() => projectsCopy[locale.value]);
useHead({ title: computed(() => workspaceTitles[locale.value].projects) });

const auth = useAuth();
const { providerLogin } = useCapabilities();
const now = useNow();
const api = createProjectsApi();
const list = useProjectList(locale, { api, auth });
const createOpen = ref(false);
const release = ref<{ kind: ReleaseKind; project: Project } | null>(null);
const action = createReleaseAction({
  api,
  auth,
  uuid: () => crypto.randomUUID(),
  providerLogin,
  onDone: (kind, project) => {
    const current = release.value;
    release.value = null;
    if (kind === 'address' && project !== null) {
      list.adopt(project);
      focusActions(project.id);
    } else if (current !== null) {
      list.forget(current.project.id);
      focusNewProject();
    }
  },
  onStale: (project) => {
    list.adopt(project);
    if (release.value !== null) release.value = { ...release.value, project };
  },
});

const view = computed(() => list.view.value);
const empty = computed(() =>
  view.value.kind === 'ready' && list.items.value.length === 0);

function focusSelector(selector: string): void {
  void nextTick(() => document.querySelector<HTMLElement>(selector)?.focus());
}

function focusActions(id: string): void {
  focusSelector(`[data-project-actions="${CSS.escape(id)}"]`);
}

function focusNewProject(): void {
  focusSelector('[data-testid="new-project"]');
}

function openCreate(): void {
  list.resetCreate();
  createOpen.value = true;
}

function closeCreate(): void {
  createOpen.value = false;
  focusNewProject();
}

function openRelease(kind: ReleaseKind, project: Project): void {
  release.value = { kind, project };
}

function closeRelease(): void {
  const current = release.value;
  release.value = null;
  if (current !== null) focusActions(current.project.id);
}
</script>

<template>
  <main
    class="mx-auto w-full max-w-[78rem] space-y-6 px-4 py-10 sm:py-12"
    data-testid="projects-page"
  >
    <div class="flex flex-wrap items-end justify-between gap-4">
      <div class="min-w-0">
        <h1
          id="projects-title"
          class="text-2xl font-bold tracking-tight"
        >
          {{ copy.title }}
        </h1>
        <p
          v-if="!empty"
          class="mt-2 text-muted-foreground"
          data-testid="projects-lead"
        >
          {{ copy.lead }}
        </p>
      </div>
      <div
        v-if="view.kind === 'ready' && !empty"
        class="flex items-center gap-3"
      >
        <span
          class="text-sm text-muted-foreground tabular-nums"
          data-testid="project-count"
        >{{ copy.count(list.items.value.length, PROJECT_LIMIT) }}</span>
        <Button
          data-testid="new-project"
          :disabled="list.atLimit.value"
          type="button"
          @click="openCreate"
        >
          <Plus aria-hidden="true" />
          {{ copy.newProject }}
        </Button>
      </div>
    </div>
    <p
      v-if="list.atLimit.value"
      class="text-sm text-muted-foreground"
      data-testid="project-limit"
      role="status"
    >
      {{ copy.limitReached(PROJECT_LIMIT) }}
    </p>

    <div
      v-if="view.kind === 'waiting-auth' || view.kind === 'loading'"
      class="divide-y divide-border rounded-lg border border-border bg-card"
      data-testid="projects-loading"
      role="status"
    >
      <span class="sr-only">{{ copy.loading }}</span>
      <div
        v-for="row in 3"
        :key="row"
        class="grid gap-2 px-4 py-4"
      >
        <Skeleton class="h-4 w-2/5" />
        <Skeleton class="h-3 w-3/5" />
      </div>
    </div>
    <StatusBanner
      v-else-if="view.kind === 'unavailable'"
      kind="error"
      testid="projects-unavailable"
    >
      {{ copy.unavailable }}
      <Button
        class="mt-2"
        data-action="projects-retry"
        size="sm"
        type="button"
        variant="outline"
        @click="list.retry()"
      >
        {{ copy.retry }}
      </Button>
    </StatusBanner>
    <div
      v-else-if="empty"
      class="rounded-lg border border-border bg-card p-8 text-center"
      data-testid="projects-empty"
    >
      <p
        class="font-medium"
        role="status"
      >
        {{ copy.empty }}
      </p>
      <p class="mt-1 text-muted-foreground">
        {{ copy.lead }}
      </p>
      <Button
        class="mt-4"
        data-testid="new-project"
        type="button"
        @click="openCreate"
      >
        <Plus aria-hidden="true" />
        {{ copy.newProject }}
      </Button>
    </div>
    <ProjectList
      v-else
      :items="list.items.value"
      :now="now"
      :copy="copy"
      :locale="locale"
      @change-address="openRelease('address', $event)"
      @remove="openRelease('delete', $event)"
    />

    <ProjectCreateDialog
      :open="createOpen"
      :state="list.createState.value"
      :copy="copy"
      @close="closeCreate"
      @submit="list.create"
    />
    <ProjectReleaseDialog
      :open="release !== null"
      :kind="release?.kind ?? 'address'"
      :project="release?.project ?? null"
      :action="action"
      :copy="copy"
      @close="closeRelease"
    />
  </main>
</template>
