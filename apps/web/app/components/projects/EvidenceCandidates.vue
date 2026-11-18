<script setup lang="ts">
import { computed, nextTick, useId } from 'vue';

import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import type { EvidenceCandidate } from '@/composables/projectEvidenceApi';
import type { Locale } from '@/i18n/locale';
import { projectEvidenceCopy } from '@/i18n/project-evidence';
import type { EvidencePicker } from './evidencePickerState';
import { evidenceNoticeText, formatEvidenceDate } from './evidenceText';
import type { EvidenceKind } from './projectDocument';

const props = defineProps<{
  readonly picker: EvidencePicker;
  readonly kind: EvidenceKind;
  readonly locale: Locale;
}>();

const uid = useId();
const copy = computed(() => projectEvidenceCopy[props.locale]);
const listing = computed(() => props.picker.listing(props.kind));
const choosing = computed(() =>
  props.kind === 'release' && props.picker.repository.value === null);
const disabled = computed(() => props.picker.locked.value
  || props.picker.atLimit.value
  || props.picker.attaching.value !== null);
const failure = computed(() => listing.value.failure === null
  ? ''
  : evidenceNoticeText(listing.value.failure, props.locale));

function rowId(candidate: EvidenceCandidate, part: string): string {
  return `${uid}-${part}-${candidate.candidateId}`;
}

async function attach(candidate: EvidenceCandidate): Promise<void> {
  if (!await props.picker.attachCandidate(candidate)) return;
  await nextTick();
  document.getElementById(rowId(candidate, 'attached'))?.focus();
}
</script>

<template>
  <div class="grid gap-3">
    <p
      v-if="listing.incomplete"
      class="text-sm text-muted-foreground"
      data-testid="evidence-incomplete"
    >
      {{ copy.incomplete }}
    </p>
    <ul
      v-if="listing.items.length > 0"
      class="divide-y divide-hairline rounded-[var(--radius-card)] border
        border-border bg-card"
    >
      <li
        v-for="candidate in listing.items"
        :key="candidate.candidateId"
        class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2
          px-4 py-3"
        :data-evidence-candidate="candidate.candidateId"
      >
        <Button
          v-if="choosing"
          class="h-auto w-full min-w-0 shrink justify-start p-0 text-left
            whitespace-normal"
          data-action="evidence-choose-repository"
          type="button"
          variant="link"
          @click="picker.choose(candidate)"
        >
          <span class="min-w-0 [overflow-wrap:anywhere]">{{
            candidate.title
          }}</span>
        </Button>
        <template v-else>
          <div class="grid min-w-0 gap-0.5">
            <span
              :id="rowId(candidate, 'title')"
              class="font-medium break-words"
              data-candidate-title
            >{{ candidate.title }}</span>
            <span
              v-if="candidate.repository.fullName !== candidate.title"
              class="text-sm break-words text-muted-foreground"
            >{{ candidate.repository.fullName }}</span>
            <time
              class="text-sm text-muted-foreground"
              :datetime="candidate.date"
            >{{ formatEvidenceDate(candidate.date, locale) }}</time>
          </div>
          <span
            v-if="picker.isAttached(candidate)"
            :id="rowId(candidate, 'attached')"
            class="text-sm"
            data-evidence-attached
            tabindex="-1"
          >{{ copy.attached }}</span>
          <Button
            v-else
            :id="rowId(candidate, 'attach')"
            :aria-describedby="picker.locked.value
              ? 'evidence-save-first'
              : undefined"
            :aria-labelledby="`${rowId(candidate, 'attach')} ${
              rowId(candidate, 'title')}`"
            data-action="evidence-attach"
            :disabled="disabled"
            size="sm"
            type="button"
            variant="outline"
            @click="attach(candidate)"
          >
            {{ copy.attach }}
          </Button>
        </template>
      </li>
    </ul>
    <div
      v-if="listing.status === 'loading'"
      class="grid gap-2 rounded-[var(--radius-card)] border border-border
        bg-card px-4 py-4"
      data-testid="evidence-listing-loading"
      role="status"
    >
      <span class="sr-only">{{ copy.loading }}</span>
      <Skeleton class="h-4 w-2/5" />
      <Skeleton class="h-3 w-1/5" />
      <Skeleton class="h-4 w-3/5" />
    </div>
    <p
      v-else-if="listing.status === 'ready' && listing.items.length === 0"
      class="text-sm text-muted-foreground"
      data-testid="evidence-empty"
    >
      {{ copy.empty }}
    </p>
    <div
      v-if="listing.status === 'failed'"
      class="grid gap-2"
    >
      <p
        class="text-sm"
        data-testid="evidence-listing-error"
        role="alert"
      >
        {{ failure }}
      </p>
      <div>
        <Button
          data-action="evidence-retry"
          size="sm"
          type="button"
          variant="outline"
          @click="picker.retry(kind)"
        >
          {{ copy.tryAgain }}
        </Button>
      </div>
    </div>
    <div v-else-if="listing.status === 'ready' && listing.nextPage !== null">
      <Button
        data-action="evidence-more"
        size="sm"
        type="button"
        variant="outline"
        @click="picker.more(kind)"
      >
        {{ copy.more }}
      </Button>
    </div>
  </div>
</template>
