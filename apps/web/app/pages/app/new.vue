<script setup lang="ts">
/**
 * /app/new — the confirm step before creating a resume from a gallery sample
 * or a blank resume with a template. A visit never creates anything; only the
 * button does, so a refresh or a replayed link cannot use up the resume
 * limit.
 */
import type { Resume } from '@aboutme/schema';
import { computed, ref, watch } from 'vue';

import LoadingState from '@/components/app/LoadingState.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import TextField from '@/components/app/TextField.vue';
import SheetThumbnail from '@/components/templates/SheetThumbnail.vue';
import { Button, buttonVariants } from '@/components/ui/button';
import { sampleRole } from '@/templates/catalog';
import {
  parseNewResumeQuery,
  startDocument,
  suggestedTitle,
} from '@/templates/startDocument';
import {
  createNotice,
  type CreateNotice,
  RESUME_CAP,
  useResumeList,
} from '../../composables/useResumeList';
import { workspaceTitles } from '@/i18n/meta';
import { resumeCreateCopy } from '@/i18n/resume-create';

const route = useRoute();
const { locale } = useLocale();
const openingLocale = locale.value;
const copy = computed(() => resumeCreateCopy[locale.value]);
useHead({ title: computed(() => workspaceTitles[locale.value].newResume) });
// A signed-out visitor here came from a public gallery page, not the app, so
// send them to create an account rather than sign in to one; register.vue
// carries `next` through Google sign-in and email verification back here.
const list = useResumeList({
  loginPath: `/register?next=${encodeURIComponent(route.fullPath)}`,
});
const request = computed(() => parseNewResumeQuery(route.query, openingLocale));
const document = ref<Resume>();
const title = ref('');
const busy = ref(false);
const notice = ref<CreateNotice>(null);
const uncertain = ref(false);
const titleInvalid = ref(false);

const message = computed(() => {
  if (titleInvalid.value) return copy.value.titleRequired;
  if (uncertain.value) return copy.value.uncertain;
  switch (notice.value) {
    case 'resume-cap': return copy.value.cap(RESUME_CAP);
    case 'create-failed': return copy.value.createFailed;
    case 'retry-later': return copy.value.retryLater;
    case 'session-lost': return copy.value.sessionLost;
    default: return null;
  }
});

watch(request, async (next) => {
  document.value = undefined;
  if (next.kind === 'invalid') return;
  const start = await startDocument(next);
  document.value = start;
  title.value = suggestedTitle(next);
}, { immediate: true });

const count = computed(() =>
  list.view.value.kind === 'ready' ? list.view.value.items.length : null);
const atCap = computed(() => count.value !== null && count.value >= RESUME_CAP);
const heading = computed(() => request.value.kind === 'template'
  ? copy.value.blankHeading
  : copy.value.sampleHeading);
const summary = computed(() => {
  const next = request.value;
  if (next.kind === 'invalid') return '';
  if (next.kind === 'template') {
    return copy.value.blankSummary(next.template.name);
  }
  const role = sampleRole(next.template, next.lng, locale.value);
  return copy.value.sampleSummary(
    next.template.name,
    role ?? '',
    copy.value.languageName(next.lng),
  );
});
const countLine = computed(() => {
  const n = count.value;
  if (n === null) return '';
  return copy.value.count(n, RESUME_CAP);
});
const resumeLanguage = computed(() => {
  const next = request.value;
  return next.kind === 'sample' ? next.lng : openingLocale;
});

async function create(): Promise<void> {
  if (document.value === undefined || busy.value || atCap.value) return;
  if (title.value.trim() === '') {
    titleInvalid.value = true;
    return;
  }
  busy.value = true;
  notice.value = null;
  uncertain.value = false;
  titleInvalid.value = false;
  const result = await list.create(
    title.value.trim(),
    resumeLanguage.value,
    document.value,
  );
  busy.value = false;
  uncertain.value = result.kind === 'opaque-create';
  notice.value = createNotice(result);
}
</script>

<template>
  <main
    class="mx-auto w-full max-w-xl px-4 py-10"
    data-testid="new-resume"
  >
    <section
      v-if="request.kind === 'invalid'"
      class="grid gap-4 rounded-lg border bg-background p-6"
      data-new-resume="invalid"
    >
      <h1 class="text-xl font-semibold">
        {{ copy.invalidTitle }}
      </h1>
      <p class="text-muted-foreground">
        {{ copy.invalidDescription }}
      </p>
      <NuxtLink
        :class="buttonVariants({ variant: 'outline' })"
        class="justify-self-start"
        to="/templates"
      >
        {{ copy.browseTemplates }}
      </NuxtLink>
    </section>
    <LoadingState
      v-else-if="count === null || document === undefined"
      :label="copy.loading"
    />
    <form
      v-else
      class="grid gap-6 rounded-lg border bg-background p-6"
      data-new-resume="confirm"
      novalidate
      @submit.prevent="create"
    >
      <div class="grid grid-cols-[120px_minmax(0,1fr)] items-start gap-5">
        <SheetThumbnail
          :document="document"
          :lng="request.kind === 'sample' ? request.lng : openingLocale"
          :width="120"
        />
        <div class="grid gap-2">
          <h1 class="text-xl font-semibold leading-tight">
            {{ heading }}
          </h1>
          <p
            class="text-muted-foreground"
            data-new-resume-summary
          >
            {{ summary }}.
            {{ request.kind === 'sample'
              ? copy.sampleDescription
              : copy.blankDescription }}
          </p>
        </div>
      </div>
      <template v-if="atCap">
        <StatusBanner
          data-new-resume-cap
          kind="info"
        >
          {{ copy.cap(RESUME_CAP) }}
        </StatusBanner>
        <NuxtLink
          :class="buttonVariants({ variant: 'outline' })"
          class="justify-self-end"
          to="/app/resumes"
        >
          {{ copy.returnToResumes }}
        </NuxtLink>
      </template>
      <template v-else>
        <TextField
          v-model="title"
          :control-attrs="{ 'data-action': 'new-resume-title' }"
          :disabled="busy"
          :label="copy.title"
          name="title"
          required
        />
        <StatusBanner kind="info">
          {{ countLine }}
        </StatusBanner>
        <StatusBanner
          v-if="message !== null"
          kind="error"
        >
          {{ message }}
        </StatusBanner>
        <div
          class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end"
        >
          <NuxtLink
            :class="buttonVariants({ variant: 'outline' })"
            to="/templates"
          >
            {{ copy.back }}
          </NuxtLink>
          <Button
            data-action="create-from-start"
            :disabled="busy"
            type="submit"
          >
            {{ busy ? copy.creating : copy.createAndOpen }}
          </Button>
        </div>
      </template>
    </form>
  </main>
</template>
