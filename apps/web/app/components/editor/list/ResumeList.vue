<script setup lang="ts">
import { Ellipsis, Plus } from '@lucide/vue';
import { nextTick, ref, watch } from 'vue';

import IconButton from '@/components/app/IconButton.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import StateMark from '@/components/app/StateMark.vue';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import type { ResumeSummary } from '../../../editor/resumeApi';
import { resumeListCopy } from '@/i18n/resume-list';
import { formatRelativeTime } from '../../../utils/relativeTime';
import { useNow } from '../../../composables/useNow';

const props = withDefaults(
  defineProps<{
    items: readonly ResumeSummary[];
    busyIds: readonly string[];
    removalFocusId: string | null;
    removalFocusVersion: number;
    now?: Date;
  }>(),
  { now: undefined },
);
const emit = defineEmits<{
  create: [];
  rename: [item: ResumeSummary];
  remove: [item: ResumeSummary];
}>();
const root = ref<HTMLElement | null>(null);
const now = props.now ?? useNow();
const locale = useRouteLocale();
const copy = computed(() => resumeListCopy[locale.value]);

function focusMenuTrigger(id: string): void {
  const selector
    = `[data-testid="resume-row-${CSS.escape(id)}"]`
      + ' [data-resume-actions]';
  (
    root.value?.querySelector<HTMLElement>(selector)
    ?? document.querySelector<HTMLElement>(selector)
  )?.focus();
}

function onMenuOpen(id: string, open: boolean): void {
  if (!open) void nextTick(() => focusMenuTrigger(id));
}

watch(
  () => props.removalFocusVersion,
  () => {
    void nextTick(() => {
      const selector
        = props.removalFocusId === null
          ? '[data-testid="create-resume"]'
          : `[data-testid="resume-row-${CSS.escape(props.removalFocusId)}"]`
            + ' [data-resume-actions]';
      (
        root.value?.querySelector<HTMLElement>(selector)
        ?? document.querySelector<HTMLElement>(selector)
      )?.focus();
    });
  },
);
</script>

<template>
  <section
    ref="root"
    aria-labelledby="resume-list-title"
    class="mx-auto w-full max-w-7xl space-y-8 px-4 py-10 sm:px-8 sm:py-12"
    data-testid="resume-list"
  >
    <PageHeader
      :title="copy.title"
      title-id="resume-list-title"
    >
      <template #actions>
        <Button
          data-testid="create-resume"
          :disabled="items.length >= 3"
          size="lg"
          type="button"
          @click="emit('create')"
        >
          <Plus aria-hidden="true" />
          {{ copy.create }}
        </Button>
      </template>
    </PageHeader>
    <ul
      :aria-label="copy.listLabel"
      class="grid gap-6 md:grid-cols-3 md:gap-8"
    >
      <li
        v-for="item in items"
        :key="item.id"
        :data-testid="`resume-row-${item.id}`"
        class="sheet paper-surface relative flex min-h-40 flex-col
          rounded-[var(--radius-sheet)] shadow-[var(--shadow-paper)]
          transition-transform duration-200 ease-out hover:-translate-y-1
          motion-reduce:transition-none motion-reduce:hover:translate-y-0"
      >
        <NuxtLink
          :to="`/app/resumes/${encodeURIComponent(item.id)}`"
          class="sheet-face block p-6 pb-3 -outline-offset-4
            after:absolute after:inset-0"
          data-sheet-link
        >
          <span class="block pr-8 text-lg font-semibold">{{ item.title }}</span>
          <time
            class="block text-xs tabular-nums text-paper-muted"
            :datetime="item.updatedAt"
          >
            {{ copy.updated(formatRelativeTime(item.updatedAt, now, locale)) }}
          </time>
        </NuxtLink>
        <span
          class="relative z-10 mt-auto block px-6 pb-5"
          :class="{ 'pointer-events-none': !item.live || !item.slug }"
        >
          <StateMark
            v-if="item.live && item.slug"
            state="public"
            :link="`/${item.slug}`"
          />
          <StateMark
            v-else
            state="draft"
          />
        </span>
        <DropdownMenu @update:open="onMenuOpen(item.id, $event)">
          <DropdownMenuTrigger as-child>
            <IconButton
              class="absolute top-3 right-3 z-10"
              :disabled="busyIds.includes(item.id)"
              :label="copy.moreActions(item.title)"
              data-resume-actions
              size="icon-sm"
            >
              <Ellipsis aria-hidden="true" />
            </IconButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              :aria-label="copy.rename(item.title)"
              @select="emit('rename', item)"
            >
              {{ copy.renameAction }}
            </DropdownMenuItem>
            <DropdownMenuItem
              class="text-destructive"
              :aria-label="copy.delete(item.title)"
              variant="destructive"
              @select="emit('remove', item)"
            >
              {{ copy.deleteAction }}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </li>
      <li
        v-for="n in Math.max(0, 3 - items.length)"
        :key="`slot-${n}`"
        :data-testid="`resume-slot-${n}`"
        class="sheet sheet--empty flex min-h-40 flex-col justify-center
          gap-1 rounded-[var(--radius-sheet)] border-2 border-dashed
          border-border bg-card/50 p-6 text-sm text-muted-foreground"
      >
        <!-- Create resume in the header is the one create control. -->
        <template v-if="items.length === 0 && n === 1">
          <span
            class="text-foreground"
            role="status"
          >{{ copy.empty }}</span>
          <span>{{ copy.emptyHelp }}</span>
        </template>
        <span v-else>{{ copy.emptySlot }}</span>
      </li>
    </ul>
  </section>
</template>
