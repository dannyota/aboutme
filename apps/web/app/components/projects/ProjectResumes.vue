<script setup lang="ts">
import { computed } from 'vue';

import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { projectPageLanguage } from '@/editor/projectLinks';
import type { ResumeSummary } from '@/editor/resumeApi';
import type { ProjectsCopy } from '@/i18n/projects';
import type { Project } from './projectDocument';
import type { ProjectResumesController } from './projectResumes';

// The project editor's Resumes section (docs/design/ui/projects.md "In the
// project screens"): every resume of the account, Shown or Add to this
// resume. Removing a project happens in the resume, where its entry lives.
const props = defineProps<{
  readonly controller: ProjectResumesController;
  readonly project: Project | null;
  readonly dirty: boolean;
  readonly copy: ProjectsCopy;
}>();

const view = computed(() => props.controller.view.value);
const linked = computed(() => props.controller.linked.value);
const pending = computed(() => props.controller.pending.value);
const status = computed(() => props.controller.status.value);
const statusText = computed(() => {
  const current = status.value;
  switch (current?.kind) {
    case 'linked': return props.copy.linkedTo(current.resume.title);
    case 'document-invalid': return props.copy.cannotTake(current.resume.title);
    case 'busy': return props.copy.resumeBusy(current.resume.title);
    case 'try-again': return props.copy.tryAgain;
    case 'session-lost': return props.copy.sessionLost;
    case 'project-not-found': return props.copy.notFound;
    default: return '';
  }
});

/** The default language's name when this resume's project page uses it. */
function fallbackFor(resume: ResumeSummary): string | null {
  if (props.project === null) return null;
  const page = projectPageLanguage(resume.lng, props.project);
  return page.fallback ? props.copy.languageName(page.key) : null;
}

function resumePath(id: string): string {
  return `/app/resumes/${encodeURIComponent(id)}`;
}
</script>

<template>
  <section
    aria-labelledby="project-resumes-title"
    class="grid gap-3 border-t py-8"
    data-testid="project-resumes"
  >
    <h2
      id="project-resumes-title"
      class="text-lg font-semibold"
    >
      {{ copy.resumesSection }}
    </h2>
    <p class="text-sm text-muted-foreground">
      {{ copy.removeInResume }}
    </p>
    <p
      v-if="dirty"
      id="project-resumes-save-first"
      class="text-sm"
    >
      {{ copy.saveFirst }}
    </p>
    <div
      v-if="view.kind === 'loading'"
      class="grid gap-2 rounded-lg border border-border bg-card px-4 py-4"
      role="status"
    >
      <span class="sr-only">{{ copy.resumesLoading }}</span>
      <Skeleton class="h-4 w-2/5" />
      <Skeleton class="h-3 w-1/5" />
    </div>
    <StatusBanner
      v-else-if="view.kind === 'unavailable'"
      kind="error"
      testid="project-resumes-unavailable"
    >
      {{ copy.resumesUnavailable }}
      <Button
        class="mt-2"
        data-action="project-resumes-retry"
        size="sm"
        type="button"
        variant="outline"
        @click="controller.retry()"
      >
        {{ copy.retry }}
      </Button>
    </StatusBanner>
    <p
      v-else-if="view.items.length === 0"
      class="text-sm text-muted-foreground"
    >
      {{ copy.noResumes }}
    </p>
    <ul
      v-else
      class="divide-y divide-border rounded-lg border border-border bg-card"
    >
      <li
        v-for="resume in view.items"
        :key="resume.id"
        class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2
          px-4 py-3"
        :data-resume-row="resume.id"
      >
        <div class="grid min-w-0 gap-0.5">
          <span
            :id="`project-resume-${resume.id}`"
            class="font-medium break-words"
          >{{ resume.title }}</span>
          <span class="text-sm text-muted-foreground">
            {{ resume.live ? copy.publicMark : copy.draftMark }}
          </span>
          <span
            v-if="fallbackFor(resume) !== null"
            class="text-sm text-muted-foreground"
            data-project-fallback
          >{{ copy.pageUses(fallbackFor(resume)!) }}</span>
        </div>
        <div
          v-if="linked.has(resume.id)"
          class="flex flex-wrap items-center gap-3"
        >
          <span class="text-sm">{{ copy.shown }}</span>
          <Button
            as-child
            size="sm"
            variant="outline"
          >
            <NuxtLink
              :aria-describedby="`project-resume-${resume.id}`"
              data-action="project-open-resume"
              :to="{
                path: resumePath(resume.id),
                query: project === null ? {} : { project: project.id },
              }"
            >
              {{ copy.openInEditor }}
            </NuxtLink>
          </Button>
        </div>
        <Button
          v-else
          :aria-describedby="dirty
            ? `project-resume-${resume.id} project-resumes-save-first`
            : `project-resume-${resume.id}`"
          data-action="project-add-to-resume"
          :disabled="dirty || pending !== null"
          size="sm"
          type="button"
          variant="outline"
          @click="controller.add(resume.id)"
        >
          {{ copy.addToResume }}
        </Button>
      </li>
    </ul>
    <p
      class="text-sm empty:hidden"
      data-testid="project-resumes-status"
      role="status"
    >
      <template v-if="statusText !== ''">
        {{ statusText }}
        <NuxtLink
          v-if="status?.kind === 'document-invalid'"
          class="underline"
          :to="resumePath(status.resume.id)"
        >
          {{ copy.openResume }}
        </NuxtLink>
      </template>
    </p>
    <!-- docs/design/projects.md "Privacy": Cancel takes the first focus. -->
    <ConfirmDialog
      v-if="controller.asking.value !== null"
      :open="true"
      :title="copy.sharedPublic"
      :description="copy.sharedReader"
      :confirm-label="copy.addAnyway"
      :cancel-label="copy.cancel"
      destructive
      confirm-action="confirm-project-privacy"
      cancel-action="cancel-project-privacy"
      @confirm="controller.addAnyway()"
      @cancel="controller.cancelAsk()"
    />
  </section>
</template>
