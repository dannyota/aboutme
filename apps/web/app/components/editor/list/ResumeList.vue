<script setup lang="ts">
import type { ResumeSummary } from '../../../editor/resumeApi';
import { Button } from '@/components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';

const props = defineProps<{
  items: readonly ResumeSummary[];
  busyIds: readonly string[];
  removalFocusId: string | null;
  removalFocusVersion: number;
}>();
const root = ref<HTMLElement | null>(null);

watch(() => props.removalFocusVersion, () => {
  void nextTick(() => {
    const selector = props.removalFocusId === null
      ? '[data-testid="create-resume"]'
      : `[data-testid="resume-row-${props.removalFocusId}"]`
        + ' [aria-label^="Rename "]';
    (root.value?.querySelector<HTMLElement>(selector)
      ?? document.querySelector<HTMLElement>(selector))?.focus();
  });
});

const updatedFormatter = new Intl.DateTimeFormat('en', {
  dateStyle: 'medium',
  timeZone: 'UTC',
});

function formatUpdated(value: string): string {
  return updatedFormatter.format(new Date(value));
}

defineEmits<{
  create: [];
  rename: [item: ResumeSummary];
  remove: [item: ResumeSummary];
}>();
</script>

<template>
  <section
    ref="root"
    aria-labelledby="page-title"
  >
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Title</TableHead>
          <TableHead class="w-40">
            Updated
          </TableHead>
          <TableHead class="w-40 text-right">
            <span class="sr-only">Actions</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody aria-label="Your resumes">
        <TableRow
          v-for="item in items"
          :key="item.id"
          :data-testid="`resume-row-${item.id}`"
        >
          <TableCell class="font-medium">
            <NuxtLink
              class="hover:underline"
              :to="`/app/resumes/${encodeURIComponent(item.id)}`"
            >
              {{ item.title }}
            </NuxtLink>
          </TableCell>
          <TableCell class="text-muted-foreground">
            <time :datetime="item.updatedAt">
              {{ formatUpdated(item.updatedAt) }}
            </time>
          </TableCell>
          <TableCell class="text-right">
            <Button
              :aria-label="`Rename ${item.title}`"
              :disabled="busyIds.includes(item.id)"
              size="sm"
              variant="ghost"
              @click="$emit('rename', item)"
            >
              Rename
            </Button>
            <Button
              :aria-label="`Delete ${item.title}`"
              class="text-destructive"
              :disabled="busyIds.includes(item.id)"
              size="sm"
              variant="ghost"
              @click="$emit('remove', item)"
            >
              Delete
            </Button>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </section>
</template>
