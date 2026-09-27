<script setup lang="ts">
/**
 * /app/import/linkedin: create a resume from a LinkedIn "Save to PDF" file,
 * read entirely in the browser (docs/design/linkedin-import.md and
 * docs/design/linkedin-import-ui.md; ADR 0064). This page wires the states
 * the design names; the pieces live under components/import.
 */
import type { Resume } from '@aboutme/schema';
import { ArrowLeft } from '@lucide/vue';
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import LoadingState from '@/components/app/LoadingState.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import ImportActionPanel from '@/components/import/ImportActionPanel.vue';
import ImportPick from '@/components/import/ImportPick.vue';
import ImportReview from '@/components/import/ImportReview.vue';
import { Button, buttonVariants } from '@/components/ui/button';
import {
  createNotice, RESUME_CAP, useResumeList,
} from '@/composables/useResumeList';
import { importCopy } from '@/i18n/import';
import { pageTitle } from '@/i18n/meta';
import { resumeCreateCopy } from '@/i18n/resume-create';
import { resumeListCopy } from '@/i18n/resume-list';
import {
  buildDocument,
  checkDocument,
  importLinkedInPdf,
  requestBytes,
  REQUEST_MAX_BYTES,
  type ImportFailure,
  type ImportReview as ImportReviewData,
  type SchemaCheck,
} from '@/import/linkedin/build';
import {
  initialChoiceSets,
  selectedSummary,
} from '@/import/linkedin/pageState';
import {
  loadPdfReader,
  supportsImport,
  type PdfReader,
  type PdfWorkerHandle,
} from '@/import/linkedin/pdfWorker';

const { locale } = useLocale();
const copy = computed(() => importCopy[locale.value]);
const createCopy = computed(() => resumeCreateCopy[locale.value]);
useHead({ title: computed(() => pageTitle(copy.value.documentTitle)) });

const list = useResumeList();

type Phase = 'loading' | 'old-browser' | 'unavailable' | 'cap' | 'pick';

const browserOk = ref(true);
const readerFailed = ref(false);
const reader = ref<PdfReader>();
const worker = ref<PdfWorkerHandle>();

const resumeCount = computed(() =>
  list.view.value.kind === 'ready' ? list.view.value.items.length : null);
const atCap = computed(() =>
  resumeCount.value !== null && resumeCount.value >= RESUME_CAP);
const listUnavailable = computed(() => list.view.value.kind === 'unavailable');

const phase = computed<Phase>(() => {
  if (!browserOk.value) return 'old-browser';
  if (listUnavailable.value || readerFailed.value) return 'unavailable';
  if (resumeCount.value === null || reader.value === undefined) {
    return 'loading';
  }
  if (atCap.value) return 'cap';
  return 'pick';
});

function endWorker(): void {
  worker.value?.terminate();
  worker.value = undefined;
}

/** A fresh worker for the next pick, started only on the Pick state, never
 * while the review is shown (docs/design/linkedin-import.md, "Reading the
 * file"). */
function armWorker(): void {
  endWorker();
  if (reader.value !== undefined) worker.value = reader.value.startWorker();
}

onMounted(async () => {
  if (!supportsImport()) {
    browserOk.value = false;
    return;
  }
  try {
    reader.value = await loadPdfReader();
    armWorker();
  } catch {
    readerFailed.value = true;
  }
});
onBeforeUnmount(() => {
  endWorker();
  controller.value?.abort();
});

// --- Pick, Reading, Failed -----------------------------------------------

const pickError = ref<ImportFailure | 'dropOne' | null>(null);
const stoppedNotice = ref(false);
const reading = ref(false);
const fileName = ref<string | null>(null);
const pagesRead = ref(0);
const pageCount = ref<number | null>(null);
const controller = ref<AbortController>();
const pickPanel = ref<InstanceType<typeof ImportPick>>();

const review = ref<ImportReviewData>();
const title = ref('');
const fullName = ref('');
const headline = ref('');
const detailIds = ref<Set<string>>(new Set());
const entryIds = ref<Set<string>>(new Set());
const titleInvalid = ref(false);
const creating = ref(false);
const createErrorMessage = ref<string | null>(null);
const uncertain = ref(false);
const reviewHeading = ref<HTMLHeadingElement>();
const actionPanel = ref<InstanceType<typeof ImportActionPanel>>();

function resetReview(): void {
  review.value = undefined;
  title.value = '';
  fullName.value = '';
  headline.value = '';
  detailIds.value = new Set();
  entryIds.value = new Set();
  titleInvalid.value = false;
  createErrorMessage.value = null;
  uncertain.value = false;
}

async function pick(file: File): Promise<void> {
  if (reader.value === undefined || worker.value === undefined) return;
  pickError.value = null;
  stoppedNotice.value = false;
  reading.value = true;
  fileName.value = file.name;
  pagesRead.value = 0;
  pageCount.value = null;
  const usedWorker = worker.value;
  void nextTick(() => pickPanel.value?.focusStop());
  // No request follows a pick: this worker was already started, and each
  // pick gets its own, ended whatever the outcome (docs/design/
  // linkedin-import.md, "Reading the file").
  worker.value = undefined;
  controller.value = new AbortController();

  const outcome = await importLinkedInPdf(
    file,
    {
      pdfjs: reader.value.pdfjs,
      worker: usedWorker,
      signal: controller.value.signal,
      onProgress: (read, count) => {
        pagesRead.value = read;
        pageCount.value = count;
      },
    },
    () => crypto.randomUUID(),
  );

  reading.value = false;
  if (!outcome.ok) {
    if (outcome.reason === 'stopped') stoppedNotice.value = true;
    else pickError.value = outcome.reason;
    // The next pick gets a fresh worker; review stays empty, so this is
    // still the Pick state.
    armWorker();
    await nextTick();
    pickPanel.value?.focusChoose();
    return;
  }

  review.value = outcome.review;
  title.value = copy.value.defaultTitle;
  fullName.value = outcome.review.fullName;
  headline.value = outcome.review.headline;
  const choices = initialChoiceSets(outcome.review);
  detailIds.value = choices.detailIds;
  entryIds.value = choices.entryIds;
  // No worker starts while the review is shown; chooseAnother arms one when
  // the page returns to Pick.
  await nextTick();
  reviewHeading.value?.focus();
}

function onDropMultiple(): void {
  pickError.value = 'dropOne';
  stoppedNotice.value = false;
}

function stop(): void {
  controller.value?.abort();
}

function chooseAnother(): void {
  resetReview();
  pickError.value = null;
  stoppedNotice.value = false;
  armWorker();
}

// --- Review, size, and schema ----------------------------------------------

const resumeDocument = computed<Resume | undefined>(() => {
  if (review.value === undefined) return undefined;
  return buildDocument(review.value, {
    title: title.value.trim(),
    fullName: fullName.value,
    headline: headline.value,
    detailIds: detailIds.value,
    entryIds: entryIds.value,
  });
});
const bytes = computed(() => (
  resumeDocument.value === undefined
    ? 0
    : requestBytes(title.value.trim(), resumeDocument.value)
));
const sizeOver = computed(() => bytes.value > REQUEST_MAX_BYTES);
const schemaCheck = computed<SchemaCheck>(() => {
  if (review.value === undefined || resumeDocument.value === undefined) {
    return { ok: true };
  }
  return checkDocument(resumeDocument.value, review.value, {
    title: title.value.trim(),
    fullName: fullName.value,
    headline: headline.value,
    detailIds: detailIds.value,
    entryIds: entryIds.value,
  });
});
const summary = computed(() => (
  review.value === undefined
    ? { n: 0, s: 0 }
    : selectedSummary(review.value, entryIds.value)
));

async function submit(): Promise<void> {
  if (review.value === undefined || resumeDocument.value === undefined) return;
  if (creating.value) return;
  if (title.value.trim() === '') {
    titleInvalid.value = true;
    await nextTick();
    document.querySelector<HTMLInputElement>('[data-action="import-title"]')
      ?.focus();
    return;
  }
  if (sizeOver.value) return;
  const check = schemaCheck.value;
  if (!check.ok) {
    if (check.entryIds.length === 0) {
      // A failure outside any entry or detail: no row to focus, so the
      // panel's reason text explains it and takes focus (docs/design/
      // linkedin-import-ui.md, "Action panel").
      actionPanel.value?.focusReason();
      return;
    }
    // Focus the first marked detail, then the first marked entry, in review
    // order (spec "Action panel").
    const firstDetail = review.value.details
      .find((detail) => check.entryIds.includes(detail.id));
    const first = firstDetail ?? review.value.sections
      .flatMap((section) => section.entries)
      .find((entry) => check.entryIds.includes(entry.id));
    if (first !== undefined) {
      document.querySelector<HTMLElement>(
        `[data-import-entry="${first.id}"]`,
      )?.focus();
    }
    return;
  }

  titleInvalid.value = false;
  creating.value = true;
  createErrorMessage.value = null;
  uncertain.value = false;
  const result = await list.create(
    title.value.trim(), 'en', resumeDocument.value,
  );
  creating.value = false;
  if (result.kind === 'opaque-create') {
    uncertain.value = true;
    return;
  }
  switch (createNotice(result)) {
    case 'resume-cap':
      createErrorMessage.value = createCopy.value.cap(RESUME_CAP);
      break;
    case 'create-failed':
      createErrorMessage.value = createCopy.value.createFailed;
      break;
    case 'retry-later':
      createErrorMessage.value = createCopy.value.retryLater;
      break;
    case 'session-lost':
      createErrorMessage.value = createCopy.value.sessionLost;
      break;
    default:
      break;
  }
}
</script>

<template>
  <main
    class="mx-auto w-full px-4 py-8 sm:px-6 sm:py-10"
    :class="review === undefined
      ? 'max-w-3xl'
      : 'max-w-3xl min-[900px]:max-w-6xl'"
    data-testid="import-linkedin"
  >
    <NuxtLink
      class="mb-4 inline-flex items-center gap-1 text-sm text-link
        underline-offset-4 hover:underline"
      to="/app/resumes"
    >
      <ArrowLeft
        aria-hidden="true"
        class="size-4"
      />
      {{ copy.back }}
    </NuxtLink>

    <LoadingState
      v-if="phase === 'loading'"
      class="mt-4"
      :label="createCopy.loading"
    />

    <template v-else-if="phase === 'old-browser'">
      <PageHeader
        class="mb-6"
        :title="copy.heading"
      />
      <StatusBanner
        data-import-old-browser
        kind="info"
      >
        {{ copy.oldBrowser }}
      </StatusBanner>
    </template>

    <template v-else-if="phase === 'unavailable'">
      <PageHeader
        class="mb-6"
        :title="copy.heading"
      />
      <StatusBanner
        data-import-unavailable
        kind="error"
      >
        {{ readerFailed
          ? copy.readerFailed
          : resumeListCopy[locale].unavailable }}
      </StatusBanner>
    </template>

    <template v-else-if="phase === 'cap'">
      <PageHeader
        class="mb-6"
        :title="copy.heading"
      />
      <StatusBanner
        data-import-cap
        kind="info"
      >
        {{ createCopy.cap(RESUME_CAP) }}
      </StatusBanner>
      <NuxtLink
        :class="[buttonVariants({ variant: 'outline' }), 'mt-4 inline-flex']"
        to="/app/resumes"
      >
        {{ createCopy.returnToResumes }}
      </NuxtLink>
    </template>

    <template v-else-if="review === undefined">
      <PageHeader
        class="mb-6"
        :description="copy.lead"
        :title="copy.heading"
      />
      <ImportPick
        ref="pickPanel"
        :copy="copy"
        :error="pickError"
        :file-name="fileName"
        :page-count="pageCount"
        :pages-read="pagesRead"
        :reading="reading"
        :stopped="stoppedNotice"
        @drop-multiple="onDropMultiple"
        @pick="pick"
        @stop="stop"
      />
    </template>

    <form
      v-else
      :aria-busy="creating || undefined"
      aria-labelledby="import-review-heading"
      class="grid gap-6 min-[900px]:grid-cols-[minmax(0,1fr)_20rem]
        min-[900px]:gap-8"
      novalidate
      @submit.prevent="submit"
    >
      <div>
        <div
          class="mb-6 flex flex-col gap-2 sm:flex-row sm:items-start
            sm:justify-between"
        >
          <div>
            <h1
              id="import-review-heading"
              ref="reviewHeading"
              class="text-2xl font-bold tracking-tight outline-none"
              tabindex="-1"
            >
              {{ copy.reviewHeading }}
            </h1>
            <p class="mt-2 text-muted-foreground">
              {{ copy.reviewLead }}
            </p>
          </div>
          <Button
            class="self-start max-sm:-ml-4"
            data-action="import-choose-another"
            :disabled="creating"
            type="button"
            variant="ghost"
            @click="chooseAnother"
          >
            {{ copy.chooseAnother }}
          </Button>
        </div>
        <ImportReview
          :copy="copy"
          :create-copy="createCopy"
          :date-format="resumeDocument?.customization.dateFormat ?? 'Mon YYYY'"
          :detail-ids="detailIds"
          :disabled="creating"
          :entry-ids="entryIds"
          :full-name="fullName"
          :headline="headline"
          :locale="locale"
          :review="review"
          :schema-check="schemaCheck"
          :title="title"
          :title-invalid="titleInvalid"
          @update:detail-ids="detailIds = $event"
          @update:entry-ids="entryIds = $event"
          @update:full-name="fullName = $event"
          @update:headline="headline = $event"
          @update:title="title = $event"
        />
      </div>
      <ImportActionPanel
        ref="actionPanel"
        :bytes="bytes"
        :copy="copy"
        :create-copy="createCopy"
        :create-error-message="createErrorMessage"
        :creating="creating"
        :n="summary.n"
        :s="summary.s"
        :schema-check="schemaCheck"
        :size-over="sizeOver"
        :uncertain="uncertain"
      />
    </form>
  </main>
</template>
