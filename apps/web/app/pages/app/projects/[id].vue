<script setup lang="ts">
import { ChevronLeft, Ellipsis, Plus } from '@lucide/vue';
import { nextTick, provide } from 'vue';

import EmptyState from '@/components/app/EmptyState.vue';
import IconButton from '@/components/app/IconButton.vue';
import LoadingState from '@/components/app/LoadingState.vue';
import StateMark from '@/components/app/StateMark.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import TextField from '@/components/app/TextField.vue';
import ErrorSummary from '@/components/editor/ErrorSummary.vue';
import DateRangeField from '@/components/editor/forms/DateRangeField.vue';
import type { FieldIntent } from '@/components/editor/forms/fieldIntent';
import ProjectLanguageDialog
  from '@/components/projects/ProjectLanguageDialog.vue';
import ProjectLeaveDialog from '@/components/projects/ProjectLeaveDialog.vue';
import ProjectReleaseDialog
  from '@/components/projects/ProjectReleaseDialog.vue';
import ProjectResumes from '@/components/projects/ProjectResumes.vue';
import ProjectTechnologies
  from '@/components/projects/ProjectTechnologies.vue';
import ProjectTextFields from '@/components/projects/ProjectTextFields.vue';
import {
  projectName,
  type ProjectDates,
  type ProjectTextDraft,
} from '@/components/projects/projectDocument';
import { issueMessage, issueTarget } from '@/components/projects/projectIssues';
import { takeUnlinked } from '@/components/projects/projectLinks';
import { useProjectResumes } from '@/components/projects/projectResumesState';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
  createProjectsApi,
  createReleaseAction,
  type ReleaseKind,
} from '@/composables/projectsApi';
import {
  createFieldDrafts,
  FieldDraftsKey,
} from '@/composables/useFieldDrafts';
import { useProjectEditor } from '@/composables/useProjects';
import { useLeaveGuard } from '@/composables/useUnsavedNavigationGuard';
import { workspaceTitles } from '@/i18n/meta';
import { projectsCopy } from '@/i18n/projects';

const route = useRoute();
const id = String(route.params.id);
const locale = useRouteLocale();
const copy = computed(() => projectsCopy[locale.value]);
useHead({ title: computed(() => workspaceTitles[locale.value].projects) });

const auth = useAuth();
const { providerLogin } = useCapabilities();
const api = createProjectsApi();
const editor = useProjectEditor(id, { api, auth });
const drafts = createFieldDrafts();
provide(FieldDraftsKey, drafts);

let leaving = false;
const leaveChoice = ref<((leave: boolean) => void) | null>(null);

function confirmLeave(): Promise<boolean> {
  leaveChoice.value?.(false);
  return new Promise((resolve) => {
    leaveChoice.value = (leave) => {
      leaveChoice.value = null;
      resolve(leave);
    };
  });
}

useLeaveGuard(() => !leaving && (drafts.flushAll()
  || editor.dirty.value || editor.saving.value), confirmLeave);

const active = ref('');
const languageOpen = ref(false);
const releaseKind = ref<ReleaseKind | null>(null);
const notice = ref<{ focus?: () => void } | null>(null);

const draft = computed(() => editor.draft.value);
const loaded = computed(() => editor.loaded.value);
const title = computed(() =>
  loaded.value === null
    ? ''
    : projectName(loaded.value).trim() || copy.value.untitled);
const languages = computed(() => Object.keys(draft.value?.text ?? {}));
const errors = computed<Record<string, string>>(() => Object.fromEntries(
  editor.issues.value.map((issue) =>
    [issue.path, issueMessage(issue, copy.value)])));
const changed = computed(() =>
  editor.dirty.value || drafts.count.value > 0);
const resumes = useProjectResumes(editor.loaded, changed, { api, auth });
const notLinked = takeUnlinked(id);
const saveMark = computed(() => {
  if (editor.saving.value) return 'saving';
  if (!changed.value) return 'saved';
  return editor.saveFailed.value ? 'failed' : 'unsaved';
});
const noticeText = computed(() => {
  switch (editor.notice.value) {
    case 'changed-elsewhere': return copy.value.changedElsewhere;
    case 'try-again': return copy.value.tryAgain;
    case 'invalid': return copy.value.saveInvalid;
    case 'session-lost': return copy.value.sessionLost;
    default: return null;
  }
});

watch(() => editor.generation.value, () => {
  active.value = draft.value?.defaultLanguage ?? '';
}, { immediate: true });

watch(() => editor.notice.value, async (next) => {
  if (next === null) return;
  await nextTick();
  notice.value?.focus?.();
});

const action = createReleaseAction({
  api,
  auth,
  uuid: () => crypto.randomUUID(),
  providerLogin,
  onDone: async (kind, project) => {
    releaseKind.value = null;
    if (kind === 'address' && project !== null) {
      editor.adopt(project);
      focusById('project-change-address');
      return;
    }
    leaving = true;
    await navigateTo('/app/projects');
  },
  onStale: (project) => editor.markStale(project),
});

function focusById(target: string): void {
  void nextTick(() => {
    const element = document.getElementById(target);
    const focusable = element?.matches('input, textarea, button')
      ? element
      : element?.querySelector<HTMLElement>(
          '[contenteditable="true"], input, textarea, button',
        );
    focusable?.focus();
  });
}

function focusIssue(path: string): void {
  const target = issueTarget(path);
  if (target.language !== null) active.value = target.language;
  focusById(target.id);
}

function updateText(
  language: string,
  field: keyof ProjectTextDraft,
  value: string,
): void {
  const text = draft.value?.text[language];
  if (text !== undefined) text[field] = value;
}

function updateDates(intent: FieldIntent<ProjectDates>): void {
  if (draft.value !== null) {
    draft.value.dates = intent.kind === 'set' ? intent.value : null;
  }
}

function addLanguage(key: string): void {
  languageOpen.value = false;
  if (editor.addLanguage(key)) active.value = key;
  focusById('project-add-language');
}

function removeLanguage(): void {
  editor.removeLanguage(active.value);
  active.value = draft.value?.defaultLanguage ?? '';
}

async function save(): Promise<void> {
  const held = drafts.flushAll();
  await editor.save(held ? [{ path: 'dates', code: 'date-range-order' }] : []);
}

function closeRelease(): void {
  const kind = releaseKind.value;
  releaseKind.value = null;
  focusById(kind === 'delete' ? 'project-delete' : 'project-change-address');
}
</script>

<template>
  <main
    class="mx-auto w-full max-w-[50rem] px-4 pt-6 sm:pt-10"
    data-testid="project-editor"
  >
    <nav
      :aria-label="copy.breadcrumb"
      class="mb-4"
    >
      <NuxtLink
        class="inline-flex items-center gap-1 rounded-[var(--radius)] text-sm
          text-muted-foreground hover:text-foreground focus-visible:ring-2
          focus-visible:ring-ring focus-visible:outline-none"
        data-testid="project-breadcrumb"
        to="/app/projects"
      >
        <ChevronLeft
          aria-hidden="true"
          :size="16"
        />
        {{ copy.title }}
      </NuxtLink>
    </nav>
    <LoadingState
      v-if="editor.view.value === 'waiting-auth'
        || editor.view.value === 'loading'"
      :label="copy.loadingProject"
      testid="project-loading"
    />
    <EmptyState
      v-else-if="editor.view.value === 'not-found'"
      :title="copy.notFoundTitle"
      :description="copy.notFoundDescription"
      data-testid="project-not-found"
    >
      <template #action>
        <Button
          as-child
          variant="outline"
        >
          <NuxtLink to="/app/projects">
            {{ copy.backToProjects }}
          </NuxtLink>
        </Button>
      </template>
    </EmptyState>
    <EmptyState
      v-else-if="editor.view.value === 'unavailable' || draft === null"
      :title="copy.projectUnavailable"
      data-testid="project-unavailable"
    >
      <template #action>
        <Button
          type="button"
          @click="editor.retry()"
        >
          {{ copy.retry }}
        </Button>
      </template>
    </EmptyState>
    <form
      v-else
      :key="editor.generation.value"
      novalidate
      data-testid="project-form"
      @submit.prevent="save"
    >
      <h1
        class="mb-6 text-2xl font-bold tracking-tight break-words"
        data-testid="project-title"
      >
        {{ title }}
      </h1>
      <ErrorSummary
        class="mb-6"
        :issues="editor.issues.value"
        @focus-issue="focusIssue"
      />
      <StatusBanner
        v-if="notLinked.length > 0"
        class="mb-6"
        kind="error"
        testid="project-not-linked"
      >
        {{ copy.notLinked(notLinked) }}
      </StatusBanner>
      <StatusBanner
        v-if="noticeText !== null"
        ref="notice"
        class="mb-6"
        kind="error"
        testid="project-notice"
      >
        {{ noticeText }}
        <Button
          v-if="editor.notice.value === 'changed-elsewhere'"
          class="mt-2"
          data-action="project-reload"
          size="sm"
          type="button"
          variant="outline"
          @click="editor.reload()"
        >
          {{ copy.reload }}
        </Button>
      </StatusBanner>

      <section
        aria-labelledby="project-text-title"
        class="border-t py-8"
      >
        <h2
          id="project-text-title"
          class="mb-4 text-lg font-semibold"
        >
          {{ copy.textSection }}
        </h2>
        <Tabs v-model="active">
          <div class="mb-4 flex flex-wrap items-center gap-2">
            <TabsList
              :aria-label="copy.languageTabs"
              class="h-auto flex-wrap"
            >
              <TabsTrigger
                v-for="key in languages"
                :key="key"
                :value="key"
                :data-language="key"
                class="flex-none"
              >
                {{ key === draft.defaultLanguage
                  ? copy.defaultTab(copy.languageName(key))
                  : copy.languageName(key) }}
              </TabsTrigger>
            </TabsList>
            <DropdownMenu>
              <DropdownMenuTrigger as-child>
                <IconButton
                  :label="copy.languageActions(copy.languageName(active))"
                  data-testid="project-language-actions"
                  size="icon-sm"
                >
                  <Ellipsis aria-hidden="true" />
                </IconButton>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                <DropdownMenuItem
                  :disabled="active === draft.defaultLanguage"
                  data-action="project-make-default"
                  @select="editor.makeDefault(active)"
                >
                  {{ copy.makeDefault }}
                </DropdownMenuItem>
                <DropdownMenuItem
                  :disabled="active === draft.defaultLanguage"
                  data-action="project-remove-language"
                  variant="destructive"
                  @select="removeLanguage"
                >
                  {{ copy.removeLanguage }}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button
              v-if="editor.canAddLanguage.value"
              id="project-add-language"
              size="sm"
              type="button"
              variant="outline"
              @click="languageOpen = true"
            >
              <Plus aria-hidden="true" />
              {{ copy.addLanguage }}
            </Button>
          </div>
          <TabsContent
            v-for="key in languages"
            :key="key"
            :value="key"
          >
            <ProjectTextFields
              :language="key"
              :text="draft.text[key]!"
              :is-default="key === draft.defaultLanguage"
              :errors="errors"
              :copy="copy"
              @update="(field, value) => updateText(key, field, value)"
            />
          </TabsContent>
        </Tabs>
      </section>

      <section
        aria-labelledby="project-details-title"
        class="grid gap-4 border-t py-8"
      >
        <h2
          id="project-details-title"
          class="text-lg font-semibold"
        >
          {{ copy.detailsSection }}
        </h2>
        <DateRangeField
          id="project-dates"
          field-id="project-dates"
          :model-value="draft.dates ?? undefined"
          class="grid gap-3"
          @intent="updateDates"
        />
        <TextField
          id="project-repository-url"
          v-model="draft.repositoryUrl"
          :label="copy.repositoryUrl"
          :hint="copy.linkHint"
          :error="errors.repositoryUrl"
          autocomplete="url"
          inputmode="url"
          name="repositoryUrl"
          type="url"
        />
        <TextField
          id="project-demo-url"
          v-model="draft.demoUrl"
          :label="copy.demoUrl"
          :hint="copy.linkHint"
          :error="errors.demoUrl"
          autocomplete="url"
          inputmode="url"
          name="demoUrl"
          type="url"
        />
        <ProjectTechnologies
          id="project-technologies"
          v-model="draft.technologies"
          :copy="copy"
          :error="errors.technologies"
        />
      </section>

      <ProjectResumes
        :controller="resumes"
        :project="loaded"
        :dirty="changed"
        :copy="copy"
      />

      <section
        aria-labelledby="project-address-title"
        class="grid gap-3 border-t py-8"
      >
        <h2
          id="project-address-title"
          class="text-lg font-semibold"
        >
          {{ copy.addressSection }}
        </h2>
        <dl class="grid gap-1">
          <dt class="text-sm font-medium">
            {{ copy.addressLabel }}
          </dt>
          <dd
            class="font-mono text-sm break-all"
            data-testid="project-slug"
          >
            {{ loaded?.slug }}
          </dd>
        </dl>
        <div>
          <Button
            id="project-change-address"
            type="button"
            variant="outline"
            @click="releaseKind = 'address'"
          >
            {{ copy.changeAddress }}
          </Button>
        </div>
      </section>

      <section
        aria-labelledby="project-delete-title"
        class="grid gap-3 border-t py-8"
      >
        <h2
          id="project-delete-title"
          class="text-lg font-semibold"
        >
          {{ copy.deleteSection }}
        </h2>
        <p class="text-sm text-muted-foreground">
          {{ copy.deleteHelp }}
        </p>
        <div>
          <Button
            id="project-delete"
            type="button"
            variant="destructive"
            @click="releaseKind = 'delete'"
          >
            {{ copy.deleteProject }}
          </Button>
        </div>
      </section>

      <div
        :aria-label="copy.saveBar"
        class="sticky bottom-0 z-10 -mx-4 flex items-center justify-between
          gap-4 border-t border-border bg-background px-4 py-3"
        data-testid="project-save-bar"
        role="region"
      >
        <StateMark :state="saveMark" />
        <Button
          :disabled="!changed || editor.saving.value"
          data-action="project-save"
          type="submit"
        >
          {{ copy.save }}
        </Button>
      </div>
    </form>

    <ProjectLanguageDialog
      :open="languageOpen"
      :existing="languages"
      :copy="copy"
      @close="languageOpen = false; focusById('project-add-language')"
      @submit="addLanguage"
    />
    <ProjectLeaveDialog
      :open="leaveChoice !== null"
      :copy="copy"
      @stay="leaveChoice?.(false)"
      @leave="leaveChoice?.(true)"
    />
    <ProjectReleaseDialog
      :open="releaseKind !== null"
      :kind="releaseKind ?? 'address'"
      :project="loaded"
      :linked-count="resumes.linked.value.size"
      :action="action"
      :copy="copy"
      @close="closeRelease"
    />
  </main>
</template>
