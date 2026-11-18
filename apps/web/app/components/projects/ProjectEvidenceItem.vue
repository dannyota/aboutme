<script setup lang="ts">
import { ChevronDown, ChevronUp } from '@lucide/vue';
import { computed, useId } from 'vue';

import IconButton from '@/components/app/IconButton.vue';
import { Button } from '@/components/ui/button';
import type { Locale } from '@/i18n/locale';
import { projectEvidenceCopy } from '@/i18n/project-evidence';
import { formatEvidenceDate } from './evidenceText';
import type { ProjectEvidence } from './projectDocument';

const props = defineProps<{
  readonly item: ProjectEvidence;
  readonly first: boolean;
  readonly last: boolean;
  readonly disabled: boolean;
  readonly describedBy?: string;
  readonly locale: Locale;
}>();
const emit = defineEmits<{
  move: [step: -1 | 1];
  recheck: [];
  remove: [];
}>();

const uid = useId();
const titleId = `${uid}-title`;
const recheckId = `${uid}-recheck`;
const removeId = `${uid}-remove`;
const copy = computed(() => projectEvidenceCopy[props.locale]);
const flag = computed(() => {
  if (props.item.confirmed) return null;
  if (props.item.flagReason === 'gone') return copy.value.gone;
  if (props.item.flagReason === 'rule_failed') return copy.value.ruleFailed;
  return null;
});

function date(value: string): string {
  return formatEvidenceDate(value, props.locale);
}
</script>

<template>
  <li
    class="grid gap-3 px-4 py-3"
    :data-evidence-item="item.id"
    :data-evidence-state="item.confirmed
      ? 'confirmed'
      : flag === null ? 'plain' : 'flagged'"
  >
    <div class="grid min-w-0 gap-0.5">
      <span
        v-if="item.confirmed"
        class="text-sm font-semibold"
        data-evidence-label
      >{{ copy.labels[item.kind] }}</span>
      <span
        v-else-if="flag !== null"
        class="text-sm text-destructive"
        data-evidence-flag
      >{{ flag }}</span>
      <a
        :id="titleId"
        class="w-fit max-w-full font-medium break-words text-link underline
          underline-offset-4"
        data-evidence-title
        :href="item.url"
        rel="noopener noreferrer"
        target="_blank"
      >{{ item.title }}</a>
      <span
        v-if="item.repositoryName !== item.title"
        class="text-sm break-words text-muted-foreground"
        data-evidence-repository
      >{{ item.repositoryName }}</span>
      <template v-if="item.confirmed">
        <time
          class="text-sm text-muted-foreground"
          data-evidence-date
          :datetime="item.itemDate"
        >{{ date(item.itemDate) }}</time>
        <span
          class="text-sm text-muted-foreground"
          data-evidence-checked
        >{{ copy.checked(date(item.checkedAt)) }}</span>
      </template>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <IconButton
        :aria-describedby="describedBy"
        data-action="evidence-move-up"
        :disabled="disabled || first"
        :label="copy.moveUp(item.title)"
        size="icon-sm"
        variant="outline"
        @click="emit('move', -1)"
      >
        <ChevronUp aria-hidden="true" />
      </IconButton>
      <IconButton
        :aria-describedby="describedBy"
        data-action="evidence-move-down"
        :disabled="disabled || last"
        :label="copy.moveDown(item.title)"
        size="icon-sm"
        variant="outline"
        @click="emit('move', 1)"
      >
        <ChevronDown aria-hidden="true" />
      </IconButton>
      <Button
        :id="recheckId"
        :aria-describedby="describedBy"
        :aria-labelledby="`${recheckId} ${titleId}`"
        data-action="evidence-recheck"
        :disabled="disabled"
        size="sm"
        type="button"
        variant="outline"
        @click="emit('recheck')"
      >
        {{ copy.checkAgain }}
      </Button>
      <Button
        :id="removeId"
        :aria-describedby="describedBy"
        :aria-labelledby="`${removeId} ${titleId}`"
        data-action="evidence-remove"
        :disabled="disabled"
        size="sm"
        type="button"
        variant="destructive"
        @click="emit('remove')"
      >
        {{ copy.remove }}
      </Button>
    </div>
  </li>
</template>
