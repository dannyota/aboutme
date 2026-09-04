<script setup lang="ts">
import { Ellipsis } from '@lucide/vue';
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

function focusMenuTrigger(id: string): void {
  const selector
    = `[data-testid="resume-row-${CSS.escape(id)}"]`
      + ' [aria-label^="More actions for "]';
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
            + ' [aria-label^="More actions for "]';
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
    class="mx-auto w-full max-w-7xl space-y-8 px-6 py-8"
    data-testid="resume-list"
  >
    <PageHeader
      title="Resumes"
      title-id="resume-list-title"
    >
      <template #actions>
        <Button
          data-testid="create-resume"
          :disabled="items.length >= 3"
          type="button"
          @click="emit('create')"
        >
          Create resume
        </Button>
      </template>
    </PageHeader>
    <ul
      aria-label="Your resumes"
      class="grid gap-6 md:grid-cols-3"
    >
      <li
        v-for="item in items"
        :key="item.id"
        :data-testid="`resume-row-${item.id}`"
        class="sheet relative rounded-[var(--radius-dialog)] bg-white
          text-[#171a18] shadow-[var(--shadow-paper)]"
      >
        <NuxtLink
          :to="`/app/resumes/${encodeURIComponent(item.id)}`"
          class="sheet-face block p-6 after:absolute after:inset-0"
          data-sheet-link
        >
          <span class="block text-lg font-semibold">{{ item.title }}</span>
          <time
            class="block text-xs tabular-nums text-muted-foreground"
            :datetime="item.updatedAt"
          >
            Updated {{ formatRelativeTime(item.updatedAt, now) }}
          </time>
        </NuxtLink>
        <span class="relative z-10 block px-6 pb-4">
          <StateMark
            v-if="item.live && item.slug"
            class="text-[#5f6763]"
            state="public"
            :link="`/${item.slug}`"
          />
          <StateMark
            v-else
            class="text-[#5f6763]"
            state="draft"
          />
        </span>
        <DropdownMenu @update:open="onMenuOpen(item.id, $event)">
          <DropdownMenuTrigger as-child>
            <IconButton
              class="absolute top-3 right-3 z-10"
              :disabled="busyIds.includes(item.id)"
              :label="`More actions for ${item.title}`"
              size="icon-sm"
            >
              <Ellipsis aria-hidden="true" />
            </IconButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              :aria-label="`Rename ${item.title}`"
              @select="emit('rename', item)"
            >
              Rename
            </DropdownMenuItem>
            <DropdownMenuItem
              class="text-destructive"
              :aria-label="`Delete ${item.title}`"
              variant="destructive"
              @select="emit('remove', item)"
            >
              Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </li>
      <li
        v-for="n in Math.max(0, 3 - items.length)"
        :key="`slot-${n}`"
        :data-testid="`resume-slot-${n}`"
        class="sheet sheet--empty min-h-40 rounded-[var(--radius-dialog)]
          border border-dashed"
      >
        <Button
          class="h-full min-h-40 w-full flex-col items-start justify-center
            whitespace-normal rounded-[var(--radius-dialog)] p-6 text-left"
          data-action="create-resume-slot"
          type="button"
          variant="ghost"
          @click="emit('create')"
        >
          <template v-if="items.length === 0 && n === 1">
            <span role="status">No resumes yet.</span>
            <span>Create your first resume. You can keep up to three.</span>
          </template>
          <span v-else>Create resume</span>
        </Button>
      </li>
    </ul>
  </section>
</template>
