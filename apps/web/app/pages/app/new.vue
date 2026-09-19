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
import { appTitles } from '@/i18n/meta';
import { sampleRole } from '@/templates/catalog';
import {
  parseNewResumeQuery,
  startDocument,
  suggestedTitle,
} from '@/templates/startDocument';
import {
  createStatusMessage,
  RESUME_CAP,
  useResumeList,
} from '../../composables/useResumeList';

useHead({ title: appTitles.newResume });

const route = useRoute();
const { locale } = useLocale();
// A signed-out visitor here came from a public gallery page, not the app, so
// send them to create an account rather than sign in to one; register.vue
// carries `next` through Google sign-in and email verification back here.
const list = useResumeList({
  loginPath: `/register?next=${encodeURIComponent(route.fullPath)}`,
});
const request = computed(() => parseNewResumeQuery(route.query, locale.value));
const document = ref<Resume>();
const title = ref('');
const busy = ref(false);
const message = ref<string | null>(null);

const LANGUAGE_NAMES = { vi: 'Vietnamese', en: 'English' } as const;
const ORDINALS = ['first', 'second', 'third'] as const;

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
  ? 'Create a blank resume with this template'
  : 'Create a resume from this sample');
const summary = computed(() => {
  const next = request.value;
  if (next.kind === 'invalid') return '';
  if (next.kind === 'template') return `${next.template.name} · Blank resume`;
  const role = sampleRole(next.template, next.lng, 'en');
  return [next.template.name, role, LANGUAGE_NAMES[next.lng]]
    .filter(Boolean)
    .join(' · ');
});
const countLine = computed(() => {
  const n = count.value;
  if (n === null) return '';
  const ordinal = ORDINALS[n];
  return `You have ${n} of ${RESUME_CAP} resumes.`
    + (ordinal === undefined ? '' : ` This creates your ${ordinal}.`);
});
const resumeLanguage = computed(() => {
  const next = request.value;
  return next.kind === 'sample' ? next.lng : locale.value;
});

async function create(): Promise<void> {
  if (document.value === undefined || busy.value || atCap.value) return;
  if (title.value.trim() === '') {
    message.value = 'Enter a title.';
    return;
  }
  busy.value = true;
  message.value = null;
  const result = await list.create(
    title.value.trim(),
    resumeLanguage.value,
    document.value,
  );
  busy.value = false;
  message.value = result.kind === 'opaque-create'
    ? 'We could not confirm whether the resume was created. Check your '
    + 'resumes before trying again.'
    : createStatusMessage(result);
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
        Template not found
      </h1>
      <p class="text-muted-foreground">
        This link does not name a template or sample we have.
      </p>
      <NuxtLink
        :class="buttonVariants({ variant: 'outline' })"
        class="justify-self-start"
        to="/templates"
      >
        Browse templates
      </NuxtLink>
    </section>
    <LoadingState
      v-else-if="count === null || document === undefined"
      label="Loading"
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
          :lng="request.kind === 'sample' ? request.lng : locale"
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
              ? 'You get a private copy to replace with your own content.'
              : 'Your new resume starts empty and wears this template.' }}
          </p>
        </div>
      </div>
      <template v-if="atCap">
        <StatusBanner
          data-new-resume-cap
          kind="info"
        >
          You have {{ RESUME_CAP }} resumes. Delete one to create another.
        </StatusBanner>
        <NuxtLink
          :class="buttonVariants({ variant: 'outline' })"
          class="justify-self-end"
          to="/app/resumes"
        >
          Go to your resumes
        </NuxtLink>
      </template>
      <template v-else>
        <TextField
          v-model="title"
          :control-attrs="{ 'data-action': 'new-resume-title' }"
          :disabled="busy"
          label="Title"
          name="title"
          required
        />
        <StatusBanner kind="info">
          {{ countLine }}
        </StatusBanner>
        <StatusBanner
          v-if="message"
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
            Back to templates
          </NuxtLink>
          <Button
            data-action="create-from-start"
            :disabled="busy"
            type="submit"
          >
            {{ busy ? 'Creating…' : 'Create and open editor' }}
          </Button>
        </div>
      </template>
    </form>
  </main>
</template>
